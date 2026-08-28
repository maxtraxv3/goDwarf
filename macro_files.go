package godwarf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// macroInit initializes the macro system. Called once on startup.
func macroInit() {
	macroState.mu.Lock()
	defer macroState.mu.Unlock()

	if macroState.Initialized {
		return
	}

	// Environment defaults
	macroState.EnvEcho = true
	macroState.EnvClickInterrupts = true

	// Create Macros directory
	macroDir := macrosDir()
	os.MkdirAll(macroDir, 0755)

	// Write default macro file if missing
	defaultPath := filepath.Join(macroDir, "Default")
	if _, err := os.Stat(defaultPath); os.IsNotExist(err) {
		os.WriteFile(defaultPath, []byte(macroDefaultContent()), 0644)
	}

	macroState.Initialized = true
}

// macroLoadCharacter loads macros for the current character.
func macroLoadCharacter(charName string) {
	macroState.mu.Lock()
	defer macroState.mu.Unlock()

	// Kill any existing macros and re-run @login on every load
	macroKillLocked()
	macroState.LoginExecuted = false

	if charName == "" {
		return
	}

	macroDir := macrosDir()
	os.MkdirAll(macroDir, 0755)

	// Find the character's macro file, case-insensitive
	charFile := macroFindCharFile(macroDir, charName)
	if charFile == "" {
		macroLog("[macro] no macro file for character %q", charName)
		return
	}
	macroState.CurrentCharFile = charFile

	// Parse the character's macro file
	macroLog("[macro] loading macros from %s", filepath.Base(charFile))
	macroParseFile(charFile)

	// Log summary
	countFuncs := 0
	for m := macroState.Functions; m != nil; m = m.Next {
		countFuncs++
	}
	countExprs := 0
	for m := macroState.Expressions; m != nil; m = m.Next {
		countExprs++
	}
	countKeys := 0
	for m := macroState.Keys; m != nil; m = m.Next {
		countKeys++
	}
	countClicks := 0
	for m := macroState.Clicks; m != nil; m = m.Next {
		countClicks++
	}
	countRepl := 0
	for m := macroState.Replacements; m != nil; m = m.Next {
		countRepl++
	}
	countGlob := 0
	for m := macroState.GlobalVars; m != nil; m = m.Next {
		countGlob++
	}
	macroLog("[macro] %d functions, %d expressions, %d keys, %d clicks, %d replacements, %d globals",
		countFuncs, countExprs, countKeys, countClicks, countRepl, countGlob)

	// Execute @login function if it exists
	if !macroState.LoginExecuted {
		macroExecLogin()
		macroState.LoginExecuted = true
	}
}

// macroFindCharFile searches macroDir for a file matching name (case-insensitive).
func macroFindCharFile(macroDir, name string) string {
	target := strings.ToLower(name)
	entries, err := os.ReadDir(macroDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.ToLower(e.Name()) == target {
			return filepath.Join(macroDir, e.Name())
		}
	}
	return ""
}

// macroFindIncludeOnDisk searches dir for a file matching name (case-insensitive).
func macroFindIncludeOnDisk(dir, name string) string {
	// If exact path exists, use it
	exact := filepath.Join(dir, name)
	if _, err := os.Stat(exact); err == nil {
		return exact
	}
	target := strings.ToLower(name)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.ToLower(e.Name()) == target {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// macroReload reloads all macros for the current character.
func macroReload() {
	charName := ""
	if macroState.CurrentCharFile != "" {
		charName = strings.TrimSuffix(
			filepath.Base(macroState.CurrentCharFile),
			filepath.Ext(macroState.CurrentCharFile),
		)
	}
	macroState.mu.Lock()
	macroState.LoginExecuted = false
	macroState.mu.Unlock()

	macroStopAll()
	macroLoadCharacter(charName)
	macroShowInfo("Macros reloaded", true)
}

// macroKill destroys all macro definitions and stops execution.
func macroKill() {
	macroState.mu.Lock()
	defer macroState.mu.Unlock()
	macroKillLocked()
}

// macroKillLocked destroys all macro definitions and stops execution.
// Must be called with the lock held.
func macroKillLocked() {
	macroState.Expressions = nil
	macroState.Replacements = nil
	macroState.Keys = nil
	macroState.Clicks = nil
	macroState.Functions = nil
	macroState.IncludeFiles = nil
	macroState.GlobalVars = nil
	macroState.TapMacros = nil
	macroState.Initialized = true

	// Stop all executing macros
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

// macroExecLogin executes the @login function macro if it exists.
// Must be called with the lock held.
func macroExecLogin() {
	for m := macroState.Functions; m != nil; m = m.Next {
		if m.Name == "@login" {
			macroLog("[macro] executing @login")
			macroStart(m, macroFunction, "")
			return
		}
	}
	macroLog("[macro] no @login function found")
}

// macroFindFunction finds a function macro by name.
func macroFindFunction(name string) *Macro {
	for m := macroState.Functions; m != nil; m = m.Next {
		if m.Name == name {
			return m
		}
	}
	return nil
}

// macroFuncExists reports whether a function macro with the given name exists.
func macroFuncExists(name string) bool {
	if !strings.HasPrefix(name, "@") {
		name = "@" + name
	}
	macroState.mu.Lock()
	defer macroState.mu.Unlock()
	return macroFindFunction(name) != nil
}

// macroExecFunc executes a function macro by name.
func macroExecFunc(name string) {
	if !strings.HasPrefix(name, "@") {
		name = "@" + name
	}
	macroState.mu.Lock()
	m := macroFindFunction(name)
	macroState.mu.Unlock()
	if m == nil {
		macroShowInfo(fmt.Sprintf("macro function %q not found", name), false)
		return
	}
	macroRun(m, macroFunction, "", false)
}

// macroFindExpressionMacro finds an expression macro matching the given text.
func macroFindExpressionMacro(text string) *Macro {
	for m := macroState.Expressions; m != nil; m = m.Next {
		if m.Attributes&attrIgnoreCase != 0 {
			if strings.EqualFold(m.Expression, text) {
				return m
			}
		} else {
			if m.Expression == text {
				return m
			}
		}
	}
	return nil
}

// macroFindKeyMacro finds a key macro matching the given key and modifiers.
func macroFindKeyMacro(key int, mods uint) *Macro {
	for m := macroState.Keys; m != nil; m = m.Next {
		mk := m.Key
		mm := m.Modifiers &^ 0x0040 // mask out capslock
		if mk == key && mm == (mods&^0x0040) {
			return m
		}
	}
	return nil
}

// macroFindClickMacro finds a click macro matching the given button and modifiers.
func macroFindClickMacro(button int, mods uint) *Macro {
	for m := macroState.Clicks; m != nil; m = m.Next {
		if m.Key == button && m.Modifiers == mods {
			return m
		}
	}
	return nil
}

// macroDoKey handles a key event, returning true if the key was consumed.
func macroDoKey(key int, mods uint) bool {
	// Ctrl+Escape always stops all macros
	if key == 0x1B && mods&0x0002 != 0 {
		macroStopAll()
		return true
	}

	m := macroFindKeyMacro(key, mods)
	if m == nil {
		return false
	}

	macroRun(m, macroKey, "", false)

	return m.Attributes&attrNoOverride == 0
}

// macroDoClick handles a click event, returning true if the click was consumed.
func macroDoClick(button int, mods uint, clickedName string) bool {
	// Stop macros if click_interrupts is set
	if macroState.EnvClickInterrupts {
		macroStopAll()
	}

	m := macroFindClickMacro(button, mods)
	if m == nil {
		return false
	}

	macroClickName = clickedName
	macroClickSimpleName = simplePlayerName(clickedName)
	macroClickButton = button
	macroClickChord = ""

	macroRun(m, macroKey, "", false)

	return m.Attributes&attrNoOverride == 0
}

// macroDoText handles text input, checking for expression macros.
// Returns true if the text was handled by a macro.
func macroDoText(text string) bool {
	words := strings.Fields(text)
	if len(words) == 0 {
		return false
	}
	firstWord := words[0]

	m := macroFindExpressionMacro(firstWord)
	if m == nil {
		return false
	}

	// The rest of the text (after the expression) becomes @text
	rest := strings.TrimPrefix(text, firstWord)
	rest = strings.TrimSpace(rest)

	macroRun(m, macroExpression, rest, false)

	return true
}

// macroDoReplacement checks if the current word being typed is a replacement
// macro. Returns the replacement text if found, or empty string.
func macroDoReplacement(word string) string {
	if macroState.Replacements == nil {
		return ""
	}

	for m := macroState.Replacements; m != nil; m = m.Next {
		if m.Attributes&attrIgnoreCase != 0 {
			if strings.EqualFold(m.Replace, word) {
				return macroGetReplacementBody(m)
			}
		} else {
			if m.Replace == word {
				return macroGetReplacementBody(m)
			}
		}
	}
	return ""
}

// macroGetReplacementBody builds the replacement text from a replacement macro.
func macroGetReplacementBody(m *Macro) string {
	// Replacement macros can't contain \r (pauses forbidden)
	var buf strings.Builder
	for cmd := m.Commands; cmd != nil; cmd = cmd.Next {
		if cmd.CommandKind == cmdText {
			buf.WriteString(cmd.VarName)
			for _, p := range cmd.Params {
				buf.WriteString(" ")
				buf.WriteString(p.VarName)
			}
		}
	}
	return buf.String()
}

// macroDefaultContent returns the default macro file content.
func macroDefaultContent() string {
	return `// Default macros - included by all character macro files
// These are shorthand expression macros for common commands.

"??"    "/help "    @text "\r"
"aa"    "/action "  @text "\r"
"gg"    "/give "    @text "\r"
"ii"    "/info "    @text "\r"
"mm"    "/money\r"
"pp"    "/ponder "  @text "\r"
"rr"    "/report "  @text "\r"
"ss"    "/speak "   @text "\r"
"tt"    "/think "   @text "\r"
"ww"    "/whisper " @text "\r"
"yy"    "/yell "    @text "\r"
`
}

// ebitenKeyToMacroKey converts an ebiten key to a macro key code.
// Returns 0 if the key has no macro equivalent.
func ebitenKeyToMacroKey(k ebiten.Key) int {
	switch k {
	case ebiten.KeyEscape:
		return 0x1B
	case ebiten.KeyF1:
		return 0x0105
	case ebiten.KeyF2:
		return 0x0106
	case ebiten.KeyF3:
		return 0x0107
	case ebiten.KeyF4:
		return 0x0108
	case ebiten.KeyF5:
		return 0x0109
	case ebiten.KeyF6:
		return 0x010A
	case ebiten.KeyF7:
		return 0x010B
	case ebiten.KeyF8:
		return 0x010C
	case ebiten.KeyF9:
		return 0x010D
	case ebiten.KeyF10:
		return 0x010E
	case ebiten.KeyF11:
		return 0x010F
	case ebiten.KeyF12:
		return 0x0110
	case ebiten.KeyMinus:
		return '-'
	case ebiten.KeyBackspace:
		return 0x08
	case ebiten.KeyTab:
		return '\t'
	case ebiten.KeyEnter:
		return 0x0D
	case ebiten.KeySpace:
		return ' '
	case ebiten.KeyHome:
		return 0x01
	case ebiten.KeyPageUp:
		return 0x0B
	case ebiten.KeyDelete:
		return 0x7F
	case ebiten.KeyEnd:
		return 0x04
	case ebiten.KeyPageDown:
		return 0x0C
	case ebiten.KeyArrowUp:
		return 0x1E
	case ebiten.KeyArrowDown:
		return 0x1F
	case ebiten.KeyArrowLeft:
		return 0x1C
	case ebiten.KeyArrowRight:
		return 0x1D
	case ebiten.KeyA:
		return 'a'
	case ebiten.KeyB:
		return 'b'
	case ebiten.KeyC:
		return 'c'
	case ebiten.KeyD:
		return 'd'
	case ebiten.KeyE:
		return 'e'
	case ebiten.KeyF:
		return 'f'
	case ebiten.KeyG:
		return 'g'
	case ebiten.KeyH:
		return 'h'
	case ebiten.KeyI:
		return 'i'
	case ebiten.KeyJ:
		return 'j'
	case ebiten.KeyK:
		return 'k'
	case ebiten.KeyL:
		return 'l'
	case ebiten.KeyM:
		return 'm'
	case ebiten.KeyN:
		return 'n'
	case ebiten.KeyO:
		return 'o'
	case ebiten.KeyP:
		return 'p'
	case ebiten.KeyQ:
		return 'q'
	case ebiten.KeyR:
		return 'r'
	case ebiten.KeyS:
		return 's'
	case ebiten.KeyT:
		return 't'
	case ebiten.KeyU:
		return 'u'
	case ebiten.KeyV:
		return 'v'
	case ebiten.KeyW:
		return 'w'
	case ebiten.KeyX:
		return 'x'
	case ebiten.KeyY:
		return 'y'
	case ebiten.KeyZ:
		return 'z'
	case ebiten.KeyDigit0:
		return '0'
	case ebiten.KeyDigit1:
		return '1'
	case ebiten.KeyDigit2:
		return '2'
	case ebiten.KeyDigit3:
		return '3'
	case ebiten.KeyDigit4:
		return '4'
	case ebiten.KeyDigit5:
		return '5'
	case ebiten.KeyDigit6:
		return '6'
	case ebiten.KeyDigit7:
		return '7'
	case ebiten.KeyDigit8:
		return '8'
	case ebiten.KeyDigit9:
		return '9'
	case ebiten.KeyNumpad0:
		return '0'
	case ebiten.KeyNumpad1:
		return '1'
	case ebiten.KeyNumpad2:
		return '2'
	case ebiten.KeyNumpad3:
		return '3'
	case ebiten.KeyNumpad4:
		return '4'
	case ebiten.KeyNumpad5:
		return '5'
	case ebiten.KeyNumpad6:
		return '6'
	case ebiten.KeyNumpad7:
		return '7'
	case ebiten.KeyNumpad8:
		return '8'
	case ebiten.KeyNumpad9:
		return '9'
	case ebiten.KeyNumpadEnter:
		return 0x0D
	case ebiten.KeyNumpadAdd:
		return '+'
	case ebiten.KeyNumpadSubtract:
		return '-'
	case ebiten.KeyNumpadMultiply:
		return '*'
	case ebiten.KeyNumpadDivide:
		return '/'
	case ebiten.KeyNumpadEqual:
		return '='
	}
	return 0
}

// ebitenButtonToMacroClick converts an ebiten mouse button to a macro click code.
func ebitenButtonToMacroClick(b ebiten.MouseButton) int {
	switch b {
	case ebiten.MouseButtonLeft:
		return 1024 // click
	case ebiten.MouseButtonRight:
		return 1025 // click2
	case ebiten.MouseButtonMiddle:
		return 1026 // click3
	}
	// Higher mouse buttons: button 3 = click4, button 4 = click5, etc.
	if b >= 3 && b <= 7 {
		return 1024 + int(b)
	}
	return 0
}

// macroCurrentMods returns the current modifier bitmask for the macro system.
func macroCurrentMods() uint {
	var mods uint
	if ebiten.IsKeyPressed(ebiten.KeyShift) || ebiten.IsKeyPressed(ebiten.KeyShiftLeft) || ebiten.IsKeyPressed(ebiten.KeyShiftRight) {
		mods |= 0x0001 // shift
	}
	if ebiten.IsKeyPressed(ebiten.KeyControl) || ebiten.IsKeyPressed(ebiten.KeyControlLeft) || ebiten.IsKeyPressed(ebiten.KeyControlRight) {
		mods |= 0x0002 // control
	}
	if ebiten.IsKeyPressed(ebiten.KeyMeta) || ebiten.IsKeyPressed(ebiten.KeyMetaLeft) || ebiten.IsKeyPressed(ebiten.KeyMetaRight) {
		mods |= 0x0004 // command
	}
	if ebiten.IsKeyPressed(ebiten.KeyAlt) || ebiten.IsKeyPressed(ebiten.KeyAltLeft) || ebiten.IsKeyPressed(ebiten.KeyAltRight) {
		mods |= 0x0008 // option
	}
	return mods
}

// isNumpadKey returns true if the ebiten key is a numpad key.
func isNumpadKey(k ebiten.Key) bool {
	return k >= ebiten.KeyNumpad0 && k <= ebiten.KeyNumpadEqual
}

// macroProcessKeyEvents checks for key-based macro triggers. Called each frame.
// Returns true if a macro consumed the key.
func macroProcessKeyEvents() bool {
	if macroState.Keys == nil {
		return false
	}
	for _, k := range inpututil.AppendJustPressedKeys(nil) {
		macroKey := ebitenKeyToMacroKey(k)
		if macroKey == 0 {
			continue
		}
		mods := macroCurrentMods()
		if isNumpadKey(k) {
			mods |= 0x0020 // numpad modifier
		}
		if macroDoKey(macroKey, mods) {
			return true
		}
	}
	return false
}

// macroProcessClickEvents checks for click-based macro triggers. Called each
// frame. clickedName is the player name if a player was clicked, empty otherwise.
func macroProcessClickEvents(clickedName string) bool {
	if macroState.Clicks == nil {
		return false
	}
	mods := macroCurrentMods()
	for b := ebiten.MouseButton(0); b <= 7; b++ {
		if inpututil.IsMouseButtonJustPressed(b) {
			if macroDoClick(ebitenButtonToMacroClick(b), mods, clickedName) {
				return true
			}
		}
	}
	return false
}

// LoadMacros loads the macro system and parses macros for the given character.
func LoadMacros(charName string) {
	macroInit()
	macroLoadCharacter(charName)
}

// ContinuePausedMacros resumes paused macros. Called when new text arrives
// or once per game frame.
func ContinuePausedMacros() {
	macroContinue()
}

// UpdateLastTextLog records the last text window line and resumes paused macros.
func UpdateLastTextLog(msg string) {
	macroState.mu.Lock()
	macroState.TextLogBuffer = msg
	atomic.AddUint64(&macroState.TextLogSeq, 1)
	macroState.mu.Unlock()
	ContinuePausedMacros()
}

// HandleMacroInput runs an expression macro for the given chat input.
// Returns the commands the macro emitted (newline separated) and true if the
// input was consumed by a macro.
func HandleMacroInput(input string) (string, bool) {
	words := strings.Fields(input)
	if len(words) == 0 {
		return "", false
	}
	m := macroFindExpressionMacro(words[0])
	if m == nil {
		return "", false
	}
	rest := strings.TrimPrefix(input, words[0])
	rest = strings.TrimSpace(rest)
	return macroRun(m, macroExpression, rest, true), true
}

// HandleMacroFunction runs a function macro by name and returns captured output.
func HandleMacroFunction(name string) string {
	if !strings.HasPrefix(name, "@") {
		name = "@" + name
	}
	macroState.mu.Lock()
	m := macroFindFunction(name)
	macroState.mu.Unlock()
	if m == nil {
		return ""
	}
	return macroRun(m, macroFunction, "", true)
}

// HandleTapMacro runs all tap macros with the given player name.
// Returns the captured commands joined with newlines.
func HandleTapMacro(playerName string) string {
	macroState.mu.Lock()
	taps := macroState.TapMacros
	macroState.mu.Unlock()
	if taps == nil {
		return ""
	}

	macroClickName = playerName
	macroClickSimpleName = simplePlayerName(playerName)
	macroClickButton = 1024
	macroClickChord = ""

	var out strings.Builder
	for m := taps; m != nil; m = m.Next {
		if m.Commands == nil {
			continue
		}
		r := macroRun(m, macroTap, playerName, true)
		if r != "" {
			if out.Len() > 0 {
				out.WriteString("\n")
			}
			out.WriteString(r)
		}
	}
	return out.String()
}