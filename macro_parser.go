package godwarf

import (
	"os"
	"path/filepath"
	"strings"
)

// macroParseFile parses a macro file and all its includes.
func macroParseFile(fname string) {
	data, err := os.ReadFile(fname)
	if err != nil {
		return
	}
	p := &macroParser{
		fileName: fname,
		lines:    strings.ReplaceAll(string(data), "\r\n", "\n"),
	}
	p.parse()
}

// macroParser holds state for parsing a single macro file.
type macroParser struct {
	fileName  string
	lines     string
	pos       int
	lineNum   int
	cmdLevel  int
	lastMacro *Macro
}

func (p *macroParser) parse() {
	// Strip /* */ block comments before line-by-line parsing
	p.lines = stripBlockComments(p.lines)
	for {
		line := p.readLine()
		if line == "" && p.pos >= len(p.lines) {
			break
		}
		p.parseLine(line)
	}
}

// stripBlockComments removes /* ... */ block comments from the input.
func stripBlockComments(s string) string {
	for {
		start := strings.Index(s, "/*")
		if start < 0 {
			return s
		}
		end := strings.Index(s[start+2:], "*/")
		if end < 0 {
			// Unterminated comment — remove to end
			return s[:start]
		}
		s = s[:start] + s[start+2+end+2:]
	}
}

func (p *macroParser) readLine() string {
	start := p.pos
	for p.pos < len(p.lines) {
		ch := p.lines[p.pos]
		p.pos++
		if ch == '\n' {
			p.lineNum++
			return p.lines[start : p.pos-1]
		}
	}
	if p.pos > start {
		p.lineNum++
		return p.lines[start:]
	}
	return ""
}

func (p *macroParser) parseLine(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	// Skip // comments
	if strings.HasPrefix(line, "//") {
		return
	}

	// Handle braces
	if line == "{" {
		p.cmdLevel++
		return
	}
	if line == "}" {
		if p.cmdLevel > 0 {
			p.cmdLevel--
		}
		p.lastMacro = nil
		return
	}
	// Handle lines starting with }
	if strings.HasPrefix(line, "}") {
		if p.cmdLevel > 0 {
			p.cmdLevel--
		}
		p.lastMacro = nil
		rest := strings.TrimSpace(line[1:])
		if rest != "" {
			p.parseLine(rest)
		}
		return
	}

	if p.cmdLevel == 0 && p.lastMacro == nil {
		p.newMacro(line)
	} else {
		p.newCommand(line)
	}
}

func (p *macroParser) newMacro(line string) {
	word, rest := getWord(line)
	if word == "" {
		return
	}
	lword := strings.ToLower(word)

	// Expression macro: starts with "
	if strings.HasPrefix(word, "\"") {
		expr := strings.TrimSuffix(word, "\"")
		if strings.HasPrefix(expr, "\"") {
			expr = strings.TrimPrefix(expr, "\"")
		}
		if expr == "" && len(rest) > 0 {
			// The quote was just the opening, get the rest
			expr, rest = getQuotedString(rest)
		}
		attrs := p.parseAttributes(rest)
		cmds, _ := p.parseCommandBody(rest)
		m := &Macro{
			Kind:       macroExpression,
			Expression: expr,
			Commands:   cmds,
			Attributes: attrs,
			FileName2:  p.fileName,
			LineNum:    p.lineNum,
		}
		// Check for duplicate
		if !macroFindExpression(macroState.Expressions, expr) {
			m.Next = macroState.Expressions
			macroState.Expressions = m
		}
		if cmds != nil {
			p.lastMacro = nil // inline commands — no block body expected
		} else {
			p.lastMacro = m // no commands yet — block body may follow
		}
		return
	}

	// Replacement macro: starts with '
	if strings.HasPrefix(word, "'") {
		repl := strings.TrimSuffix(word, "'")
		if strings.HasPrefix(repl, "'") {
			repl = strings.TrimPrefix(repl, "'")
		}
		if repl == "" && len(rest) > 0 {
			repl, rest = getQuotedString(rest)
		}
		attrs := p.parseAttributes(rest)
		cmds, _ := p.parseCommandBody(rest)
		m := &Macro{
			Kind:       macroReplacement,
			Replace:    repl,
			Commands:   cmds,
			Attributes: attrs,
			FileName2:  p.fileName,
			LineNum:    p.lineNum,
		}
		if !macroFindReplacement(macroState.Replacements, repl) {
			m.Next = macroState.Replacements
			macroState.Replacements = m
		}
		return
	}

	// set (top-level variable)
	if lword == "set" || lword == "setglobal" {
		isGlobal := lword == "setglobal"
		vname, rest2 := getWord(rest)
		vval, _ := getWord(rest2)
		macroSetVariable(vname, macroResolveExpr(vval), isGlobal)
		return
	}

	// include
	if lword == "include" {
		incName := strings.TrimSpace(rest)
		incName = strings.Trim(incName, "\"'")
		if incName == "" {
			return
		}
		if macroFindIncludeFile(macroState.IncludeFiles, incName) {
			return // already included
		}
		m := &Macro{
			Kind:     macroIncludeFile,
			FileName: incName,
		}
		m.Next = macroState.IncludeFiles
		macroState.IncludeFiles = m
		// Resolve include path relative to the current file's directory
		dir := filepath.Dir(p.fileName)
		incPath := macroFindIncludeOnDisk(dir, incName)
		if incPath == "" {
			incPath = filepath.Join(dir, incName)
		}
		macroLog("[macro]   include %q -> %s", incName, incPath)
		macroParseFile(incPath)
		return
	}

	// Tap macro: fires when a player is tapped in the game world.
	if lword == "tap" {
		attrs := p.parseAttributes(rest)
		m := &Macro{
			Kind:       macroTap,
			Name:       "tap",
			Attributes: attrs,
			FileName2:  p.fileName,
			LineNum:    p.lineNum,
		}
		m.Next = macroState.TapMacros
		macroState.TapMacros = m
		cmds, _ := p.parseCommandBody(rest)
		m.Commands = cmds
		p.lastMacro = m
		return
	}

	// Try key name
	key, mods := macroGetKeyByName(word)
	if key != 0 {
		m := &Macro{
			Kind:       macroKey,
			Key:        key,
			Modifiers:  mods,
			FileName2:  p.fileName,
			LineNum:    p.lineNum,
		}
		// Parse attributes from rest
		m.Attributes = p.parseAttributes(rest)
		// Parse command body from rest
		cmds, lastCmd := p.parseCommandBody(rest)
		m.Commands = cmds
		if key >= 1024 && key < 1032 {
			// Click macro
			m.Kind = macroClick
			m.Next = macroState.Clicks
			macroState.Clicks = m
		} else {
			m.Next = macroState.Keys
			macroState.Keys = m
		}
		p.lastMacro = m
		_ = lastCmd
		return
	}

	// Function macro (default)
	attrs := p.parseAttributes(rest)
	m := &Macro{
		Kind:       macroFunction,
		Name:       word,
		Attributes: attrs,
		FileName2:  p.fileName,
		LineNum:    p.lineNum,
	}
	cmds, _ := p.parseCommandBody(rest)
	m.Commands = cmds
	m.Next = macroState.Functions
	macroState.Functions = m
	p.lastMacro = m
}

func (p *macroParser) parseCommandBody(line string) (first, last *Macro) {
	// Parse commands from the rest of the line
	words := splitLine(line)
	for _, w := range words {
		cmd := p.makeCommandNode(w)
		if cmd != nil {
			if first == nil {
				first = cmd
				last = cmd
			} else {
				last.Next = cmd
				last = cmd
			}
		}
	}
	return
}

func (p *macroParser) makeCommandNode(word string) *Macro {
	lword := strings.ToLower(word)

	// Check for attributes
	if strings.HasPrefix(word, "$") {
		return nil // attributes handled elsewhere
	}

	// Check for control characters
	if word == "{" {
		p.cmdLevel++
		return nil
	}
	if word == "}" {
		if p.cmdLevel > 0 {
			p.cmdLevel--
		}
		p.lastMacro = nil
		return nil
	}

	// Look up command kind
	cmdID := cmdNameToID[lword]
	if cmdID == 0 {
		// Not a known command - treat as text
		return &Macro{
			Kind:        macroVariable, // reused as "command node"
			CommandKind: cmdText,
			VarName:     word,
			FileName2:   p.fileName,
			LineNum:     p.lineNum,
		}
	}

	switch cmdID {
	case cmdEnd:
		// "end" followed by "random" or "if"
		return nil // handled by newCommand
	case cmdElse:
		return nil // handled by newCommand
	case cmdLabelCommand:
		return nil // handled by newCommand
	case cmdRandom:
		return &Macro{
			Kind:        macroVariable,
			CommandKind: cmdRandom,
			FileName2:   p.fileName,
			LineNum:     p.lineNum,
		}
	case cmdPause:
		return &Macro{
			Kind:        macroVariable,
			CommandKind: cmdPause,
			FileName2:   p.fileName,
			LineNum:     p.lineNum,
		}
	case cmdMoveCommand:
		return &Macro{
			Kind:        macroVariable,
			CommandKind: cmdMoveCommand,
			FileName2:   p.fileName,
			LineNum:     p.lineNum,
		}
	case cmdSetVariable:
		return &Macro{
			Kind:        macroVariable,
			CommandKind: cmdSetVariable,
			FileName2:   p.fileName,
			LineNum:     p.lineNum,
		}
	case cmdSetGlobalVariable:
		return &Macro{
			Kind:        macroVariable,
			CommandKind: cmdSetGlobalVariable,
			FileName2:   p.fileName,
			LineNum:     p.lineNum,
		}
	case cmdCallFunction:
		return &Macro{
			Kind:        macroVariable,
			CommandKind: cmdCallFunction,
			FileName2:   p.fileName,
			LineNum:     p.lineNum,
		}
	case cmdIf:
		return &Macro{
			Kind:        macroVariable,
			CommandKind: cmdIf,
			FileName2:   p.fileName,
			LineNum:     p.lineNum,
		}
	case cmdGoto:
		return &Macro{
			Kind:        macroVariable,
			CommandKind: cmdGoto,
			FileName2:   p.fileName,
			LineNum:     p.lineNum,
		}
	case cmdMessage:
		return &Macro{
			Kind:        macroVariable,
			CommandKind: cmdMessage,
			FileName2:   p.fileName,
			LineNum:     p.lineNum,
		}
	case cmdNotCaseSensitive:
		return &Macro{
			Kind:        macroVariable,
			CommandKind: cmdNotCaseSensitive,
			FileName2:   p.fileName,
			LineNum:     p.lineNum,
		}
	}
	return nil
}

func (p *macroParser) newCommand(line string) {
	words := splitLine(line)
	if len(words) == 0 {
		return
	}

	word := words[0]
	rest := words[1:]
	lword := strings.ToLower(word)

	// Handle "end random" and "end if"
	if lword == "end" && len(rest) > 0 {
		lrest := strings.ToLower(rest[0])
		if lrest == "random" {
			p.appendCommandNode(&Macro{
				CommandKind: cmdEndRandom,
				FileName2:   p.fileName,
				LineNum:     p.lineNum,
			})
			return
		}
		if lrest == "if" {
			p.appendCommandNode(&Macro{
				CommandKind: cmdEndIf,
				FileName2:   p.fileName,
				LineNum:     p.lineNum,
			})
			return
		}
	}

	// Handle "else if"
	if lword == "else" && len(rest) > 0 && strings.ToLower(rest[0]) == "if" {
		node := &Macro{
			CommandKind: cmdElseIf,
			FileName2:   p.fileName,
			LineNum:     p.lineNum,
		}
		for _, w := range rest[1:] {
			node.Params = append(node.Params, &Macro{
				CommandKind: cmdText,
				VarName:     w,
			})
		}
		p.appendCommandNode(node)
		return
	}

	// Handle "else"
	if lword == "else" {
		p.appendCommandNode(&Macro{
			CommandKind: cmdElse,
			FileName2:   p.fileName,
			LineNum:     p.lineNum,
		})
		return
	}

	// Check for attributes
	if strings.HasPrefix(word, "$") {
		if p.lastMacro != nil {
			switch strings.ToLower(word) {
			case "$ignore_case":
				p.lastMacro.Attributes |= attrIgnoreCase
			case "$any_click":
				p.lastMacro.Attributes |= attrAnyClick
			case "$no_override":
				p.lastMacro.Attributes |= attrNoOverride
			}
		}
		return
	}

	// Look up command kind
	cmdID := cmdNameToID[lword]

	switch cmdID {
	case cmdLabelCommand:
		if len(rest) > 0 {
			p.appendCommandNode(&Macro{
				CommandKind: cmdLabelCommand,
				LabelName:   rest[0],
				FileName2:   p.fileName,
				LineNum:     p.lineNum,
			})
		}
		return

	case cmdRandom:
		node := &Macro{
			CommandKind: cmdRandom,
			FileName2:   p.fileName,
			LineNum:     p.lineNum,
		}
		if len(rest) > 0 && strings.ToLower(rest[0]) == "no-repeat" {
			node.NoRepeat = true
		}
		p.appendCommandNode(node)
		return
	}

	// General command or text
	node := &Macro{
		CommandKind: cmdID,
		FileName2:   p.fileName,
		LineNum:     p.lineNum,
	}
	if cmdID == 0 {
		node.CommandKind = cmdText
		node.VarName = word
	}
	// All words (including rest) become parameters
	for _, w := range rest {
		node.Params = append(node.Params, &Macro{
			CommandKind: cmdText,
			VarName:     w,
		})
	}
	p.appendCommandNode(node)
}

func (p *macroParser) appendCommandNode(node *Macro) {
	if p.lastMacro == nil {
		return
	}
	if p.lastMacro.Commands == nil {
		p.lastMacro.Commands = node
	} else {
		tail := p.lastMacro.Commands
		for tail.Next != nil {
			tail = tail.Next
		}
		tail.Next = node
	}
}

func (p *macroParser) parseAttributes(line string) uint {
	var attrs uint
	words := splitLine(line)
	for _, w := range words {
		if !strings.HasPrefix(w, "$") {
			break
		}
		switch strings.ToLower(w) {
		case "$ignore_case":
			attrs |= attrIgnoreCase
		case "$any_click":
			attrs |= attrAnyClick
		case "$no_override":
			attrs |= attrNoOverride
		}
	}
	return attrs
}

// getWord extracts the first whitespace-delimited word from line.
func getWord(line string) (word, rest string) {
	line = strings.TrimLeft(line, " \t\r\n")
	if line == "" {
		return "", ""
	}

	// Check for quoted string
	if line[0] == '"' || line[0] == '\'' {
		quote := line[0]
		end := strings.IndexByte(line[1:], quote)
		if end >= 0 {
			return line[:end+2], strings.TrimLeft(line[end+2:], " \t\r\n")
		}
		// No closing quote - take the rest
		return line, ""
	}

	// Check for // comment
	if idx := strings.Index(line, "//"); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
		if line == "" {
			return "", ""
		}
	}

	// Split on whitespace
	for i, ch := range line {
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
			return line[:i], strings.TrimLeft(line[i:], " \t\r\n")
		}
	}
	return line, ""
}

// getQuotedString extracts a quoted string from line.
func getQuotedString(line string) (string, string) {
	line = strings.TrimLeft(line, " \t\r\n")
	if len(line) == 0 {
		return "", ""
	}
	quote := line[0]
	if quote != '"' && quote != '\'' {
		return "", line
	}
	end := strings.IndexByte(line[1:], quote)
	if end >= 0 {
		return line[1 : end+1], strings.TrimLeft(line[end+2:], " \t\r\n")
	}
	return line[1:], ""
}

// splitLine splits a line into words, respecting quotes.
func splitLine(line string) []string {
	var words []string
	line = strings.TrimLeft(line, " \t\r\n")
	for len(line) > 0 {
		// Skip whitespace
		i := 0
		for i < len(line) && (line[i] == ' ' || line[i] == '\t' || line[i] == '\r' || line[i] == '\n') {
			i++
		}
		line = line[i:]
		if line == "" {
			break
		}
		// Check for comment
		if strings.HasPrefix(line, "//") {
			break
		}
		// Check for quoted string
		if line[0] == '"' || line[0] == '\'' {
			quote := line[0]
			end := strings.IndexByte(line[1:], quote)
			if end >= 0 {
				words = append(words, line[:end+2])
				line = line[end+2:]
				continue
			}
			words = append(words, line)
			break
		}
		// Check for special single-char tokens
		if line[0] == '{' || line[0] == '}' {
			words = append(words, line[:1])
			line = line[1:]
			continue
		}
		// Unquoted word
		end := 0
		for end < len(line) && line[end] != ' ' && line[end] != '\t' && line[end] != '\r' && line[end] != '\n' {
			end++
		}
		words = append(words, line[:end])
		line = line[end:]
	}
	return words
}

// macroGetKeyByName parses a key specification like "command-shift-f1" or
// "shift-right-click" and returns the key code and modifier bits.
func macroGetKeyByName(spec string) (int, uint) {
	parts := strings.Split(strings.ToLower(spec), "-")
	if len(parts) == 0 {
		return 0, 0
	}

	// Try progressively longer suffixes as the key name, checking that
	// all preceding parts are modifiers. This handles keys with hyphens
	// like "right-click" in "shift-right-click".
	for keyStart := len(parts) - 1; keyStart >= 0; keyStart-- {
		keyPart := strings.Join(parts[keyStart:], "-")
		code, ok := keyNameToCode[keyPart]
		if !ok {
			continue
		}
		// Verify all parts before keyStart are modifiers
		var mods uint
		valid := true
		for i := 0; i < keyStart; i++ {
			if bit, ok := modNameToBit[parts[i]]; ok {
				mods |= bit
			} else {
				valid = false
				break
			}
		}
		if valid {
			return code, mods
		}
	}

	return 0, 0
}

// macroFindExpression checks if an expression macro already exists.
func macroFindExpression(root *Macro, expr string) bool {
	for m := root; m != nil; m = m.Next {
		if m.Expression == expr {
			return true
		}
	}
	return false
}

// macroFindReplacement checks if a replacement macro already exists.
func macroFindReplacement(root *Macro, repl string) bool {
	for m := root; m != nil; m = m.Next {
		if m.Replace == repl {
			return true
		}
	}
	return false
}

// macroFindIncludeFile checks if an include file has already been processed.
func macroFindIncludeFile(root *Macro, name string) bool {
	for m := root; m != nil; m = m.Next {
		if strings.EqualFold(m.FileName, name) {
			return true
		}
	}
	return false
}

// macroResolveExpr resolves a macro expression string, handling quotes
// and variable references.
func macroResolveExpr(expr string) string {
	s := strings.TrimSpace(expr)
	if len(s) == 0 {
		return s
	}
	// Strip matching quotes
	if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
		s = s[1 : len(s)-1]
		return s
	}
	// Try variable resolution
	if strings.HasPrefix(s, "@") {
		val, ok := macroResolveVariable(s)
		if ok {
			return val
		}
	}
	return s
}