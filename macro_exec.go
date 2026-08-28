package godwarf

import (
	"fmt"
	"math/rand"
	"strings"
	"sync/atomic"
)

// macroContinue advances all executing macros by one step.
// Called once per frame from the game loop and when paused macros should
// be resumed (e.g. new text arrives). Re-entrant calls (from within a macro
// execution chain via AddTextMessage) return immediately.
func macroContinue() {
	macroState.mu.Lock()
	if macroState.Advancing {
		macroState.mu.Unlock()
		return
	}
	macroState.Advancing = true
	macroState.mu.Unlock()

	var prev *ExecutingMacro
	for ex := macroState.Executing; ex != nil; {
		next := ex.Next
		if macroContinueOne(ex) {
			// Macro is done
			macroFinish(ex)
			if prev == nil {
				macroState.Executing = next
			} else {
				prev.Next = next
			}
		} else {
			prev = ex
		}
		ex = next
	}

	macroState.mu.Lock()
	macroState.Advancing = false
	macroState.mu.Unlock()
}

// maxCmdsPerFrame is the maximum number of commands an unfriendly macro can
// execute before yielding to the game loop. This prevents infinite goto loops
// from freezing the client while still allowing function macros like @login
// to process their entire body per frame.
const maxCmdsPerFrame = 12000

// macroContinueOne advances a single executing macro by one step.
// Returns true if the macro is finished.
func macroContinueOne(ex *ExecutingMacro) bool {
	// Check for interruption
	if macroState.EnvKeyInterrupts {
		return true
	}

	// Check if paused
	if ex.WaitUntil > atomic.LoadInt32(&macroAckFrame) {
		return false
	}

	cmdCount := 0
	// Execute commands
	for {
		// Find the current command in the deepest active mark
		mark := ex.Mark
		for mark != nil && mark.Commands == nil {
			mark = mark.Next
			ex.Mark = mark
		}
		if mark == nil || mark.Commands == nil {
			return true // no more commands
		}

		cmd := mark.Commands
		// Advance past this command
		mark.Commands = cmd.Next

		done := macroExecuteCommand(ex, cmd, mark)
		if done {
			return true
		}

		// Check for buffer with \r (macros use literal \r or actual CR as command terminator)
		if strings.Contains(ex.Buffer, "\r") || strings.Contains(ex.Buffer, "\\r") {
			cmdStr := strings.ReplaceAll(ex.Buffer, "\r", "")
			cmdStr = strings.ReplaceAll(cmdStr, "\\r", "")
			if cmdStr != "" {
				// Handle client-side commands synchronously
				if !runClientCommand(cmdStr) {
					enqueueCommand(cmdStr)
				}
			}
			ex.Buffer = ""
			return false
		}

		// Yield to event loop after some processing (friendly mode)
		if !ex.Unfriendly {
			return false
		}

		// Unfriendly mode: yield after too many commands to prevent
		// infinite goto loops from freezing the client
		cmdCount++
		if cmdCount >= maxCmdsPerFrame {
			return false
		}
	}
}

// macroRun advances a trigger macro synchronously until it yields, pauses,
// or completes. With capture set, commands emitted by the macro are written
// to a capture buffer (and returned as a newline-joined string) instead of
// being sent to the server. Macros that pause remain in the executing list
// and are continued per-frame by macroContinue.
func macroRun(m *Macro, kind int, text string, capture bool) string {
	macroState.mu.Lock()
	if macroState.Advancing {
		macroState.mu.Unlock()
		return ""
	}
	macroState.Advancing = true
	var buf strings.Builder
	if capture {
		macroState.Output = &buf
	}
	prevUnfriendly := macroState.EnvUnfriendly
	macroState.EnvUnfriendly = true
	macroState.mu.Unlock()

	ex := macroStart(m, kind, text)
	for {
		if macroContinueOne(ex) {
			macroFinish(ex)
			if macroState.Executing == ex {
				macroState.Executing = ex.Next
			}
			break
		}
		if ex.Buffer != "" && (strings.Contains(ex.Buffer, "\r") || strings.Contains(ex.Buffer, "\\r")) {
			cmdStr := strings.ReplaceAll(ex.Buffer, "\r", "")
			cmdStr = strings.ReplaceAll(cmdStr, "\\r", "")
			if cmdStr != "" {
				enqueueCommand(cmdStr)
			}
			ex.Buffer = ""
			continue
		}
		break
	}

	macroState.mu.Lock()
	macroState.Output = nil
	macroState.EnvUnfriendly = prevUnfriendly
	macroState.Advancing = false
	macroState.mu.Unlock()

	if capture {
		return buf.String()
	}
	return ""
}

// enqueueCommand sends a command to the server. While a capture buffer is
// active (a trigger macro running synchronously), the command is appended
// to the buffer instead so the caller can enqueue it after the macro returns.
func enqueueCommand(cmd string) {
	macroState.mu.Lock()
	out := macroState.Output
	macroState.mu.Unlock()
	if out != nil {
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString(cmd)
		return
	}
	if gameInstance == nil || gameInstance.net == nil {
		return
	}
	gameInstance.net.EnqueueCommand(cmd)
}

// runClientCommand processes a macro command locally where possible.
// It returns true if the command was handled locally (and so should not be
// sent to the server). Any local response is shown in the chat window.
func runClientCommand(cmd string) bool {
	if gameInstance == nil || gameInstance.net == nil {
		return false
	}
	res := handleClientCommand(cmd)
	if res.Response != "" {
		gameInstance.net.AddTextMessage(res.Response, colDefault)
		logTextMessage(res.Response)
	}
	return !res.Sent
}

// macroExecuteCommand executes a single command node.
// Returns true if the macro is finished.
func macroExecuteCommand(ex *ExecutingMacro, cmd *Macro, mark *Mark) bool {
	switch cmd.CommandKind {
	case cmdPause:
		delay := macroGetParamInt(ex, cmd, 0)
		if delay < 0 {
			delay = 0
		}
		ex.WaitUntil = atomic.LoadInt32(&macroAckFrame) + int32(delay)
		return false

	case cmdMoveCommand:
		return macroExecuteMove(ex, cmd)

	case cmdSetVariable:
		return macroExecuteSetVar(ex, cmd, false)

	case cmdSetGlobalVariable:
		return macroExecuteSetVar(ex, cmd, true)

	case cmdCallFunction:
		return macroExecuteCall(ex, cmd)

	case cmdIf:
		return macroExecuteIf(ex, cmd, mark)

	case cmdElseIf:
		return macroExecuteElseIf(ex, cmd, mark)

	case cmdElse:
		return macroExecuteElse(ex, mark)

	case cmdEndIf:
		// Pop the if-match tracking stack
		if len(ex.IfMatched) > 0 {
			ex.IfMatched = ex.IfMatched[:len(ex.IfMatched)-1]
		}
		return false

	case cmdRandom:
		return macroExecuteRandom(ex, cmd, mark)

	case cmdOr:
		// Skip to end random
		macroSkipToCmd(ex, mark, cmdEndRandom)
		return false

	case cmdEndRandom:
		return false

	case cmdLabelCommand:
		return false

	case cmdGoto:
		return macroExecuteGoto(ex, cmd, mark)

	case cmdText:
		ex.Buffer += macroResolveExpression(ex, cmd.VarName)
		for _, p := range cmd.Params {
			ex.Buffer += " " + macroResolveExpression(ex, p.VarName)
		}
		return false

	case cmdMessage:
		msg := ""
		for i, p := range cmd.Params {
			if i > 0 {
				msg += " "
			}
			msg += macroResolveExpression(ex, p.VarName)
		}
		macroShowInfo(msg, true)
		return false

	case cmdNotCaseSensitive:
		return false
	}

	return false
}

// macroExecuteMove handles the "move" command.
func macroExecuteMove(ex *ExecutingMacro, cmd *Macro) bool {
	if len(cmd.Params) == 0 {
		return false
	}
	dir := strings.ToLower(macroResolveExpression(ex, cmd.Params[0].VarName))
	if dir == "stop" {
		sendWalkCommand(0, 0, false)
		return false
	}
	if len(cmd.Params) < 2 {
		return false
	}
	speed := strings.ToLower(macroResolveExpression(ex, cmd.Params[1].VarName))
	fast := speed == "run"

	var dx, dy int
	switch dir {
	case "e", "east":
		dx, dy = 1, 0
	case "ne", "northeast":
		dx, dy = 1, -1
	case "n", "north":
		dx, dy = 0, -1
	case "nw", "northwest":
		dx, dy = -1, -1
	case "w", "west":
		dx, dy = -1, 0
	case "sw", "southwest":
		dx, dy = -1, 1
	case "s", "south":
		dx, dy = 0, 1
	case "se", "southeast":
		dx, dy = 1, 1
	case "stop":
		dx, dy = 0, 0
	}
	sendWalkCommand(dx, dy, fast)
	return false
}

// macroExecuteSetVar handles "set" and "setglobal" commands.
func macroExecuteSetVar(ex *ExecutingMacro, cmd *Macro, global bool) bool {
	if len(cmd.Params) < 2 {
		return false
	}
	name := macroResolveBrackets(cmd.Params[0].VarName)

	if len(cmd.Params) == 2 {
		// set name value
		value := macroResolveExpression(ex, cmd.Params[1].VarName)
		if global {
			macroSetGlobalVariable(name, value)
		} else {
			macroSetLocalVariable(ex, name, value)
		}
		return false
	}

	if len(cmd.Params) >= 3 {
		// set name op value (arithmetic/string operations)
		op := cmd.Params[1].VarName
		valStr := macroResolveExpression(ex, cmd.Params[2].VarName)

		var curVal string
		if global {
			curVal, _ = macroFindGlobalVariable(name)
		} else {
			curVal, _ = macroGetLocalVariable(ex, name)
		}
		if curVal == "" {
			curVal = "0"
		}

		var result string
		switch op {
		case "+":
			// Try numeric addition first
			n1, e1 := parseIntVal(curVal)
			n2, e2 := parseIntVal(valStr)
			if e1 == nil && e2 == nil {
				result = fmt.Sprintf("%d", n1+n2)
			} else {
				// String concatenation
				result = curVal + valStr
			}
		case "-":
			n1, _ := parseIntVal(curVal)
			n2, _ := parseIntVal(valStr)
			result = fmt.Sprintf("%d", n1-n2)
		case "*":
			n1, _ := parseIntVal(curVal)
			n2, _ := parseIntVal(valStr)
			result = fmt.Sprintf("%d", n1*n2)
		case "/":
			n1, _ := parseIntVal(curVal)
			n2, _ := parseIntVal(valStr)
			if n2 != 0 {
				result = fmt.Sprintf("%d", n1/n2)
			} else {
				result = "0"
			}
		case "%":
			n1, _ := parseIntVal(curVal)
			n2, _ := parseIntVal(valStr)
			if n2 != 0 {
				result = fmt.Sprintf("%d", n1%n2)
			} else {
				result = "0"
			}
		default:
			result = valStr
		}

		if global {
			macroSetGlobalVariable(name, result)
		} else {
			macroSetLocalVariable(ex, name, result)
		}
	}
	return false
}

func parseIntVal(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty string")
	}
	n := 0
	negative := false
	if s[0] == '-' {
		negative = true
		s = s[1:]
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(ch-'0')
	}
	if negative {
		n = -n
	}
	return n, nil
}

// macroExecuteCall handles the "call" command.
func macroExecuteCall(ex *ExecutingMacro, cmd *Macro) bool {
	if len(cmd.Params) == 0 {
		return false
	}
	funcName := macroResolveExpression(ex, cmd.Params[0].VarName)

	// Find the function macro
	var funcMacro *Macro
	for m := macroState.Functions; m != nil; m = m.Next {
		if m.Name == funcName {
			funcMacro = m
			break
		}
	}
	if funcMacro == nil {
		macroShowInfo(fmt.Sprintf("Function not found: %s", funcName), false)
		return false
	}

	// Push a new mark (call stack frame)
	newMark := &Mark{
		Commands:     funcMacro.Commands,
		CommandsHead: funcMacro.Commands,
		Next:         ex.Mark,
	}
	ex.Mark = newMark
	return false
}

// macroExecuteIf handles the "if" command.
func macroExecuteIf(ex *ExecutingMacro, cmd *Macro, mark *Mark) bool {
	// Push a new nesting level: no branch matched yet
	ex.IfMatched = append(ex.IfMatched, false)
	result := macroEvalCondition(ex, cmd, cmd.Params)
	if result {
		// Mark this level as matched
		ex.IfMatched[len(ex.IfMatched)-1] = true
	} else {
		// Skip to else/elseif/endif
		macroSkipToCmd(ex, mark, cmdElse, cmdElseIf, cmdEndIf)
	}
	return false
}

// macroExecuteElseIf handles the "else if" command.
func macroExecuteElseIf(ex *ExecutingMacro, cmd *Macro, mark *Mark) bool {
	if len(ex.IfMatched) == 0 {
		// No matching if — treat as skip
		macroSkipToCmd(ex, mark, cmdElse, cmdElseIf, cmdEndIf)
		return false
	}
	top := len(ex.IfMatched) - 1
	if ex.IfMatched[top] {
		// A previous branch in this chain was already taken — skip to endif
		macroSkipToCmd(ex, mark, cmdEndIf)
		return false
	}
	// No branch taken yet — evaluate this condition
	result := macroEvalCondition(ex, cmd, cmd.Params)
	if result {
		ex.IfMatched[top] = true
	} else {
		macroSkipToCmd(ex, mark, cmdElse, cmdElseIf, cmdEndIf)
	}
	return false
}

// macroExecuteElse handles the "else" command.
func macroExecuteElse(ex *ExecutingMacro, mark *Mark) bool {
	if len(ex.IfMatched) == 0 {
		macroSkipToCmd(ex, mark, cmdEndIf)
		return false
	}
	top := len(ex.IfMatched) - 1
	if ex.IfMatched[top] {
		// A previous branch was taken — skip to endif
		macroSkipToCmd(ex, mark, cmdEndIf)
		return false
	}
	// No branch taken yet — mark as matched and execute the else body
	ex.IfMatched[top] = true
	return false
}

// macroExecuteRandom handles the "random" command.
func macroExecuteRandom(ex *ExecutingMacro, cmd *Macro, mark *Mark) bool {
	// Count "or" branches between here and endrandom
	branchCount := 0
	cur := mark.Commands
	levels := 0
	for cur != nil {
		if cur.CommandKind == cmdRandom {
			levels++
		} else if cur.CommandKind == cmdEndRandom {
			if levels == 0 {
				break
			}
			levels--
		} else if cur.CommandKind == cmdOr && levels == 0 {
			branchCount++
		}
		cur = cur.Next
	}

	if branchCount == 0 {
		return false
	}

	// Choose a random branch
	chosen := rand.Intn(branchCount + 1)

	// If no-repeat, avoid the last chosen
	if cmd.NoRepeat && branchCount > 1 {
		for chosen == cmd.LastChosen {
			chosen = rand.Intn(branchCount + 1)
		}
	}
	cmd.LastChosen = chosen

	// Skip past 'chosen' number of 'or' boundaries
	cur = mark.Commands
	levels = 0
	for cur != nil && chosen > 0 {
		if cur.CommandKind == cmdRandom {
			levels++
		} else if cur.CommandKind == cmdEndRandom {
			if levels == 0 {
				break
			}
			levels--
		} else if cur.CommandKind == cmdOr && levels == 0 {
			chosen--
		}
		cur = cur.Next
	}
	mark.Commands = cur
	return false
}

// macroExecuteGoto handles the "goto" command.
// Backward jumps always yield the frame, matching the original ClanLaw client
// where goto that jumped backwards ended the frame even in unfriendly mode.
// Forward jumps (to labels after the current position) continue within the
// same frame.
func macroExecuteGoto(ex *ExecutingMacro, cmd *Macro, mark *Mark) bool {
	if len(cmd.Params) == 0 {
		return false
	}
	labelName := macroResolveExpression(ex, cmd.Params[0].VarName)

	// Search from the beginning of the function's command list
	head := mark.CommandsHead
	if head == nil {
		head = mark.Commands
	}
	cur := head
	for cur != nil {
		if cur.CommandKind == cmdLabelCommand && cur.LabelName == labelName {
			// Always yield on backward jumps to prevent infinite loops
			// from freezing the game. This matches the original ClanLaw
			// client where backward goto always ended the frame, even
			// in unfriendly (function macro) mode.
			isBackward := false
			check := mark.Commands
			for check != nil {
				if check == cur {
					isBackward = true
					break
				}
				check = check.Next
			}
			if isBackward {
				mark.Commands = cur.Next
				return false // yield
			}
			mark.Commands = cur.Next // skip past the label node itself
			return false
		}
		cur = cur.Next
	}
	macroShowInfo(fmt.Sprintf("Label not found: %s", labelName), false)
	return false
}

// macroSkipToCmd advances mark.Commands past commands until one of the target kinds is found.
func macroSkipToCmd(ex *ExecutingMacro, mark *Mark, targets ...int) {
	levels := 0
	cur := mark.Commands
	for cur != nil {
		// Level tracking for nested blocks
		if cur.CommandKind == cmdIf || cur.CommandKind == cmdRandom {
			levels++
		} else if (cur.CommandKind == cmdEndIf || cur.CommandKind == cmdEndRandom) && levels > 0 {
			levels--
		} else if levels == 0 {
			// At level 0, check if this is a target
			for _, t := range targets {
				if cur.CommandKind == t {
					if t == cmdElse {
						// Skip past else keyword into the body
						mark.Commands = cur.Next
					} else {
						// For endif/elseif, land on the node so the handler runs
						mark.Commands = cur
					}
					return
				}
			}
		}
		cur = cur.Next
	}
	mark.Commands = nil
}

// macroGetParamInt resolves the Nth parameter of a command as an integer.
func macroGetParamInt(ex *ExecutingMacro, cmd *Macro, idx int) int {
	if idx >= len(cmd.Params) {
		return 0
	}
	val := macroResolveExpression(ex, cmd.Params[idx].VarName)
	n, _ := parseIntVal(val)
	return n
}

// macroFinish cleans up an executing macro when it's done.
func macroFinish(ex *ExecutingMacro) {
	// Send any remaining buffer content
	if ex.Buffer != "" {
		cmd := strings.ReplaceAll(ex.Buffer, "\r", "")
		cmd = strings.ReplaceAll(cmd, "\\r", "")
		if cmd != "" {
			enqueueCommand(cmd)
		}
		ex.Buffer = ""
	}
}

// macroStart creates a new executing macro and adds it to the executing list.
func macroStart(m *Macro, kind int, text string) *ExecutingMacro {
	ex := &ExecutingMacro{
		Macro: m,
		Kind:  kind,
		Debug: macroState.EnvDebug,
		// Function and tap macros always run unfriendly (all commands per
		// frame), matching the original ClanLord client behavior. Expression/
		// key/click macros respect the env unfriendly setting.
		Unfriendly: kind == macroFunction || kind == macroTap || macroState.EnvUnfriendly,
	}

	// Create initial mark
	ex.Mark = &Mark{
		Commands:     m.Commands,
		CommandsHead: m.Commands,
	}

	// Set @text variable
	macroSetLocalVariable(ex, "@text", text)
	macroSetLocalVariable(ex, "@textsel", "")

	// Add to executing list
	ex.Next = macroState.Executing
	macroState.Executing = ex

	return ex
}

// macroStopAll stops all executing macros.
func macroStopAll() {
	macroState.mu.Lock()
	defer macroState.mu.Unlock()

	macroStopAllLocked()
}

// macroStopAllLocked stops all executing macros. Caller must hold macroState.mu.
func macroStopAllLocked() {
	for ex := macroState.Executing; ex != nil; ex = ex.Next {
		macroFinish(ex)
	}
	macroState.Executing = nil

	// Stop any macro-directed movement
	macroMoveActive = false
	macroMoveDX = 0
	macroMoveDY = 0
	if gameInstance != nil && gameInstance.net != nil {
		gameInstance.net.SendInput(0, 0, false)
	}
}

// macroIsRunning returns true if any macros are currently executing.
func macroIsRunning() bool {
	macroState.mu.Lock()
	defer macroState.mu.Unlock()
	return macroState.Executing != nil
}

// sendWalkCommand starts or stops macro-directed movement. While the macro
// move is active, Game.Update sends it to the server instead of the joystick.
func sendWalkCommand(dx, dy int, fast bool) {
	if gameInstance == nil || gameInstance.net == nil {
		return
	}
	if dx == 0 && dy == 0 {
		macroMoveActive = false
		macroMoveDX = 0
		macroMoveDY = 0
		gameInstance.net.SendInput(0, 0, false)
		return
	}
	speed := float32(0.5)
	if fast {
		speed = 1.0
	}
	fdx := float32(dx) * speed
	fdy := float32(dy) * speed
	if dx != 0 && dy != 0 {
		// Normalize diagonals to avoid an extra-long stride
		fdx *= 0.7071
		fdy *= 0.7071
	}
	macroMoveActive = true
	macroMoveDX = fdx
	macroMoveDY = fdy
	gameInstance.net.SendInput(fdx, fdy, true)
}