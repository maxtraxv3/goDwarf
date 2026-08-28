package godwarf

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// macroTextLogStamp returns a timestamp prefix in ClanLord format: "M/DD/YY H:MM:SSa ".
func macroTextLogStamp() string {
	now := time.Now()
	hour := now.Hour()
	ampm := 'a'
	if hour >= 12 {
		ampm = 'p'
	}
	hour12 := (hour+11)%12 + 1
	return fmt.Sprintf("%d/%d/%02d %d:%02d:%02d%c ",
		now.Month(), now.Day(), now.Year()%100,
		hour12, now.Minute(), now.Second(), ampm)
}

// Macro types (what kind of macro definition this is)
const (
	macroEmpty = iota
	macroExpression
	macroReplacement
	macroFunction
	macroKey
	macroClick
	macroIncludeFile
	macroVariable
	macroTap
)

// Command types (commands inside a macro body)
const (
	cmdNone = iota
	cmdPause
	cmdMoveCommand
	cmdSetVariable
	cmdSetGlobalVariable
	cmdCallFunction
	cmdEnd
	cmdIf
	cmdElse
	cmdElseIf
	cmdEndIf
	cmdRandom
	cmdOr
	cmdEndRandom
	cmdLabelCommand
	cmdGoto
	cmdText
	cmdMessage
	cmdNotCaseSensitive
	cmdStart
	cmdFinish
)

// Attribute flags
const (
	attrIgnoreCase = 0x0001
	attrAnyClick   = 0x0002
	attrNoOverride = 0x0008
)

// Variable namespaces
const (
	varNone = iota
	varEnv
	varMy
	varSelPlayer
	varClick
	varRandom
)

// Environment variable sub-IDs
const (
	envKeyInterrupts = iota + 1
	envClickInterrupts
	envDebug
	envEcho
	envUnfriendly
	envTextLog
)

// Player variable sub-IDs (for @my.* and @selplayer.*)
const (
	pvarName = iota + 1
	pvarSimpleName
	playerLeftItem
	playerRightItem
	playerRace
	playerGender
	playerHealth
	playerBalance
	playerMagic
	playerSharesIn
	playerSharesOut
	playerForehead
	playerNeck
	playerShoulders
	playerArms
	playerGloves
	playerFinger
	playerCoat
	playerCloak
	playerTorso
	playerWaist
	playerLegs
	playerFeet
	playerBothHands
	playerHead
	playerSelectedItem
)

// Text manipulation variable sub-IDs
const (
	textWord = iota + 1
	textLetter
	textNumWords
	textNumLetters
)

// Macro is a single macro definition or command node.
// It forms a linked list via next. For trigger macros (expression, key,
// function, replacement), commands holds the body.
type Macro struct {
	Kind       int
	Next       *Macro
	Attributes uint

	// Expression macro: the trigger text (e.g. "??")
	Expression string

	// Replacement macro: the word to replace
	Replace string

	// Function macro: the function name (e.g. "@login")
	Name string

	// Key macro: key code and modifier mask
	Key       int
	Modifiers uint

	// Include file macro: filename
	FileName string

	// Variable macro: name and value
	VarName  string
	VarValue string

	// Command node: command kind + parameters
	CommandKind int
	Params      []*Macro

	// Command body for function/key/click macros (linked list of command nodes)
	Commands *Macro

	// Label command: label name
	LabelName string

	// Random command: tracks last chosen branch for no-repeat
	LastChosen int
	NoRepeat   bool

	// Source location for error reporting
	FileName2 string
	LineNum   int
}

// ExecutingMacro represents a macro currently being executed.
// Multiple macros can run concurrently.
type ExecutingMacro struct {
	Macro      *Macro
	Mark       *Mark
	Vars       *Macro // linked list of local CVariableMacro
	Buffer     string
	WaitUntil  int32
	Kind       int
	Debug      bool
	Unfriendly bool
	Next       *ExecutingMacro

	// IfMatched tracks whether a branch was taken in the current if-chain.
	// Each entry corresponds to a nesting level; true means a branch matched.
	IfMatched []bool

	// TextLogIfs tracks the last textlog sequence each if-node already fired
	// on, so a polled condition doesn't re-trigger for the same line.
	TextLogIfs map[*Macro]uint64
}

// Mark is a call-stack frame for nested function calls.
type Mark struct {
	Commands     *Macro
	CommandsHead *Macro // original head for goto searches
	Next         *Mark
}

// MacroState holds all global macro system state.
type MacroState struct {
	mu sync.Mutex

	// Macro definition lists (linked lists)
	Expressions  *Macro
	Replacements *Macro
	Keys         *Macro
	Clicks       *Macro
	Functions    *Macro
	IncludeFiles *Macro
	GlobalVars   *Macro
	TapMacros    *Macro

	// Currently executing macros
	Executing *ExecutingMacro

	// Environment variables
	EnvDebug           bool
	EnvEcho            bool
	EnvUnfriendly      bool
	EnvKeyInterrupts   bool
	EnvClickInterrupts bool
	TextLogBuffer      string

	// Last text sent to the text window (for @env.textlog)
	TextWinLine string

	// Monotonic sequence number incremented each time @env.textlog changes.
	// Used to deduplicate polled conditions against the same textlog line.
	TextLogSeq uint64

	// @login executed flag
	LoginExecuted bool

	// Per-character macro file name
	CurrentCharFile string

	// Whether the macro system is initialized
	Initialized bool

	// Capture buffer for commands emitted while a trigger macro runs
	// synchronously (used by HandleMacroInput / tap macros). When non-nil,
	// enqueueCommand writes here instead of sending to the server. Commands
	// are joined with newlines so callers can enqueue each one in turn.
	Output *strings.Builder

	// Advancing is set while a macro execution loop is running; nested
	// entry points (e.g. AddTextMessage -> UpdateLastTextLog -> macroContinue)
	// return immediately instead of re-entering.
	Advancing bool
}

var macroState MacroState

// keyNameToCode maps key name strings to their integer codes.
var keyNameToCode = map[string]int{
	"escape":    0x1B,
	"f1":       0x0105, "f2": 0x0106, "f3": 0x0107, "f4": 0x0108,
	"f5":       0x0109, "f6": 0x010A, "f7": 0x010B, "f8": 0x010C,
	"f9":       0x010D, "f10": 0x010E, "f11": 0x010F, "f12": 0x0110,
	"f13":      0x0111, "f14": 0x0112, "f15": 0x0113, "f16": 0x0114,
	"minus":    '-',
	"delete":   0x08,
	"tab":      '\t',
	"return":   0x0D,
	"space":    ' ',
	"help":     0x05,
	"home":     0x01,
	"undo":     0x1A,
	"pageup":   0x0B,
	"del":      0x7F,
	"end":      0x04,
	"pagedown": 0x0C,
	"up":       0x1E,
	"down":     0x1F,
	"left":     0x1C,
	"right":    0x1D,
	"clear":    0x1B,
	"enter":    0x03,
	"0":        '0', "1": '1', "2": '2', "3": '3', "4": '4',
	"5":        '5', "6": '6', "7": '7', "8": '8', "9": '9',
	"a":        'a', "b": 'b', "c": 'c', "d": 'd', "e": 'e',
	"f":        'f', "g": 'g', "h": 'h', "i": 'i', "j": 'j',
	"k":        'k', "l": 'l', "m": 'm', "n": 'n', "o": 'o',
	"p":        'p', "q": 'q', "r": 'r', "s": 's', "t": 't',
	"u":        'u', "v": 'v', "w": 'w', "x": 'x', "y": 'y',
	"z":        'z',
	"click":    1024,
	"click2":   1025, "right-click": 1025,
	"click3":   1026, "click4": 1027, "click5": 1028,
	"click6":   1029, "click7": 1030, "click8": 1031,
	"wheelup":   2048,
	"wheeldown": 2049,
	"wheelleft": 2050,
	"wheelright": 2051,
}

// modNameToBit maps modifier name strings to their bit values.
var modNameToBit = map[string]uint{
	"command": 0x0004,
	"control": 0x0002,
	"numpad":  0x0020,
	"option":  0x0008,
	"shift":   0x0001,
}

// cmdNameToID maps command name strings to their command kind IDs.
var cmdNameToID = map[string]int{
	"pause":       cmdPause,
	"move":        cmdMoveCommand,
	"set":         cmdSetVariable,
	"setglobal":   cmdSetGlobalVariable,
	"call":        cmdCallFunction,
	"if":          cmdIf,
	"random":      cmdRandom,
	"or":          cmdOr,
	"label":       cmdLabelCommand,
	"goto":        cmdGoto,
	"message":     cmdMessage,
	"ignore_case": cmdNotCaseSensitive,
}

// varNamespaceToID maps variable namespace prefixes to their IDs.
var varNamespaceToID = map[string]int{
	"@env":       varEnv,
	"@my":        varMy,
	"@selplayer": varSelPlayer,
	"@click":     varClick,
	"@random":    varRandom,
}

// envVarNameToID maps environment variable sub-field names to their IDs.
var envVarNameToID = map[string]int{
	"key_interrupts":  envKeyInterrupts,
	"click_interrupts": envClickInterrupts,
	"debug":           envDebug,
	"echo":            envEcho,
	"unfriendly":      envUnfriendly,
	"textlog":         envTextLog,
}

// playerVarNameToID maps player variable sub-field names to their IDs.
var playerVarNameToID = map[string]int{
	"name":           pvarName,
	"simple_name":    pvarSimpleName,
	"left_item":      playerLeftItem,
	"right_item":     playerRightItem,
	"race":           playerRace,
	"gender":         playerGender,
	"health":         playerHealth,
	"balance":        playerBalance,
	"magic":          playerMagic,
	"shares_in":      playerSharesIn,
	"shares_out":     playerSharesOut,
	"forehead_item":  playerForehead,
	"neck_item":      playerNeck,
	"shoulders_item": playerShoulders,
	"arms_item":      playerArms,
	"gloves_item":    playerGloves,
	"finger_item":    playerFinger,
	"coat_item":      playerCoat,
	"cloak_item":     playerCloak,
	"torso_item":     playerTorso,
	"waist_item":     playerWaist,
	"legs_item":      playerLegs,
	"feet_item":      playerFeet,
	"hands_item":     playerBothHands,
	"head_item":      playerHead,
	"selected_item":  playerSelectedItem,
}

// obsoleteVarRemap maps old variable names to new ones.
var obsoleteVarRemap = map[string]string{
	"@name":          "@my.name",
	"@splayer":       "@selplayer.simple_name",
	"@rplayer":       "@selplayer.name",
	"@rhanditem":     "@my.right_item",
	"@lhanditem":     "@my.left_item",
	"@echo":          "@env.echo",
	"@debug":         "@env.debug",
	"@interruptclick": "@env.click_interrupts",
	"@interruptkey":  "@env.key_interrupts",
	"@clicksplayer":  "@click.simple_name",
	"@clickrplayer":  "@click.name",
	"@wordcount":     "@text.num_words",
}

// playerSlotToVar maps equipment slot constants to player variable IDs.
var playerSlotToVar = map[int]int{
	kItemSlotForehead:  playerForehead,
	kItemSlotNeck:      playerNeck,
	kItemSlotShoulder:  playerShoulders,
	kItemSlotArms:      playerArms,
	kItemSlotGloves:    playerGloves,
	kItemSlotFinger:    playerFinger,
	kItemSlotCoat:      playerCoat,
	kItemSlotCloak:     playerCloak,
	kItemSlotTorso:     playerTorso,
	kItemSlotWaist:     playerWaist,
	kItemSlotLegs:      playerLegs,
	kItemSlotFeet:      playerFeet,
	kItemSlotBothHands: playerBothHands,
	kItemSlotHead:      playerHead,
}

// invSlotNames maps player variable IDs to slot name strings for getInventory lookups.
var invSlotNames = map[int]string{
	playerForehead:  "forehead",
	playerNeck:      "neck",
	playerShoulders: "shoulders",
	playerArms:      "arms",
	playerGloves:    "gloves",
	playerFinger:    "finger",
	playerCoat:      "coat",
	playerCloak:     "cloak",
	playerTorso:     "torso",
	playerWaist:     "waist",
	playerLegs:      "legs",
	playerFeet:      "feet",
	playerBothHands: "hands",
	playerHead:      "head",
}

// Click vars stored when a click macro fires
var (
	macroClickName       string
	macroClickSimpleName string
	macroClickButton     int
	macroClickChord      string
)

// Selected player vars
var (
	macroSelPlayerName      string
	macroSelPlayerSimpleName string
)

// Macro random state
var macroRandomState int

// macroCodeToName maps macro key/click codes back to human-readable names.
var macroCodeToName = map[int]string{
	0x1B:   "Escape",
	0x0105: "F1", 0x0106: "F2", 0x0107: "F3", 0x0108: "F4",
	0x0109: "F5", 0x010A: "F6", 0x010B: "F7", 0x010C: "F8",
	0x010D: "F9", 0x010E: "F10", 0x010F: "F11", 0x0110: "F12",
	0x0111: "F13", 0x0112: "F14", 0x0113: "F15", 0x0114: "F16",
	'-':    "Minus",
	0x08:   "Delete",
	'\t':   "Tab",
	0x0D:   "Return",
	' ':    "Space",
	0x05:   "Help",
	0x01:   "Home",
	0x1A:   "Undo",
	0x0B:   "PageUp",
	0x7F:   "Del",
	0x04:   "End",
	0x0C:   "PageDown",
	0x1E:   "Up",
	0x1F:   "Down",
	0x1C:   "Left",
	0x1D:   "Right",
	0x03:   "Enter",
	1024:   "Click",
	1025:   "RightClick",
	1026:   "MiddleClick",
	1027:   "Click4", 1028: "Click5",
	1029:   "Click6", 1030: "Click7", 1031: "Click8",
	2048:   "WheelUp",
	2049:   "WheelDown",
	2050:   "WheelLeft",
	2051:   "WheelRight",
}

// macroModBitToName maps modifier bit values to human-readable names.
var macroModBitToName = map[uint]string{
	0x0001: "Shift",
	0x0002: "Control",
	0x0004: "Command",
	0x0008: "Option",
	0x0020: "Numpad",
}

// MacroBinding describes a single key or click macro binding for display.
type MacroBinding struct {
	Combo    string // human-readable combo, e.g. "Ctrl+F1"
	Source   string // source file basename
	LineNum  int
	BodyHint string // first command in the macro body (e.g. "/action")
}

// macroGetComboName converts a macro key code + modifier bitmask to a
// human-readable combo string like "Ctrl+Shift+F1".
func macroGetComboName(keyCode int, mods uint) string {
	var parts []string
	// Add modifiers in a stable order
	for _, bit := range []uint{0x0002, 0x0001, 0x0008, 0x0004, 0x0020} {
		if mods&bit != 0 {
			parts = append(parts, macroModBitToName[bit])
		}
	}
	name := macroCodeToName[keyCode]
	if name == "" {
		name = fmt.Sprintf("Key(%d)", keyCode)
	}
	parts = append(parts, name)
	return strings.Join(parts, "+")
}

// macroGetBodyHint returns a short description of the first command in a macro body.
func macroGetBodyHint(cmds *Macro) string {
	if cmds == nil {
		return ""
	}
	if cmds.CommandKind == cmdText {
		s := cmds.VarName
		for _, p := range cmds.Params {
			s += " " + p.VarName
		}
		if len(s) > 40 {
			s = s[:40] + "..."
		}
		return s
	}
	return ""
}

// macroGetBindings returns all loaded key and click macro bindings.
// Must be called with macroState.mu held or during single-threaded init.
func macroGetBindings() []MacroBinding {
	var bindings []MacroBinding
	for m := macroState.Keys; m != nil; m = m.Next {
		combo := macroGetComboName(m.Key, m.Modifiers)
		src := filepath.Base(m.FileName2)
		if src == "." || src == "" {
			src = "?"
		}
		bindings = append(bindings, MacroBinding{
			Combo:    combo,
			Source:   src,
			LineNum:  m.LineNum,
			BodyHint: macroGetBodyHint(m.Commands),
		})
	}
	for m := macroState.Clicks; m != nil; m = m.Next {
		combo := macroGetComboName(m.Key, m.Modifiers)
		src := filepath.Base(m.FileName2)
		if src == "." || src == "" {
			src = "?"
		}
		bindings = append(bindings, MacroBinding{
			Combo:    combo,
			Source:   src,
			LineNum:  m.LineNum,
			BodyHint: macroGetBodyHint(m.Commands),
		})
	}
	return bindings
}

// macroLog writes a diagnostic line to the debug log.
func macroLog(format string, args ...interface{}) {
	flog(fmt.Sprintf(format, args...))
}

// macroConsoleMessage prints a message to the chat window and text log.
func macroConsoleMessage(msg string) {
	logTextMessage(msg)
	if gameInstance != nil && gameInstance.net != nil {
		gameInstance.net.AddTextMessage(msg, colDefault)
	}
}

// macroShowInfo displays a macro debug/error message.
func macroShowInfo(msg string, mustShow bool) {
	if mustShow || macroState.EnvDebug {
		macroConsoleMessage("[MACRO] " + msg)
	}
}

// macroAckFrame is incremented once per game frame. Macro "pause N" waits N
// frames by comparing against this counter.
var macroAckFrame int32

// Macro-directed movement overrides the joystick while it is active.
// sendWalkCommand sets these; Game.Update sends them each frame.
var (
	macroMoveActive bool
	macroMoveDX     float32
	macroMoveDY     float32
)