package godwarf

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync/atomic"
)

// macroPlayerName returns the current character's name.
func macroPlayerName() string {
	if gameInstance == nil || gameInstance.net == nil {
		return ""
	}
	return gameInstance.net.playerName
}

// macroResolveVariable resolves a built-in namespace variable reference like
// "@my.name" or "@env.debug", or a user variable. Returns the value and
// whether it was found.
func macroResolveVariable(name string) (string, bool) {
	// Check obsolete variable names first
	if newName, ok := obsoleteVarRemap[name]; ok {
		name = newName
	}

	// Handle .num_words suffix on any variable
	if strings.HasSuffix(name, ".num_words") {
		base := name[:len(name)-len(".num_words")]
		val, ok := macroResolveVariable(base)
		if !ok {
			return "0", false
		}
		return fmt.Sprintf("%d", len(strings.Fields(val))), true
	}

	// Handle .word[N] suffix on any variable
	if wordIdx, base, ok := parseWordIndex(name); ok {
		val, _ := macroResolveVariable(base)
		words := strings.Fields(val)
		if wordIdx < len(words) {
			return words[wordIdx], true
		}
		return "", false
	}

	// Extract namespace
	ns, subfield := splitVarRef(name)
	nsID := varNamespaceToID[ns]
	if nsID == 0 {
		// Not a built-in namespace - check user variables
		return macroGetUserVariable(name)
	}

	switch nsID {
	case varEnv:
		return macroGetEnvVar(subfield)
	case varMy:
		return macroGetMyVar(subfield)
	case varSelPlayer:
		return macroGetSelPlayerVar(subfield)
	case varClick:
		return macroGetClickVar(subfield)
	case varRandom:
		return fmt.Sprintf("%d", rand.Intn(10000)), true
	}

	return "", false
}

// parseWordIndex checks if name ends with ".word[N]" and returns (index, base, true).
func parseWordIndex(name string) (int, string, bool) {
	idx := strings.Index(name, ".word[")
	if idx < 0 || !strings.HasSuffix(name, "]") {
		return 0, "", false
	}
	nStr := name[idx+6 : len(name)-1]
	n, err := strconv.Atoi(nStr)
	if err != nil {
		return 0, "", false
	}
	return n, name[:idx], true
}

// splitVarRef splits "@namespace.subfield" into ("@namespace", "subfield").
func splitVarRef(name string) (string, string) {
	if !strings.HasPrefix(name, "@") {
		return name, ""
	}
	dotIdx := strings.Index(name[1:], ".")
	if dotIdx < 0 {
		return name, ""
	}
	return name[:dotIdx+1], name[dotIdx+2:]
}

// macroGetEnvVar returns an environment variable value.
func macroGetEnvVar(subfield string) (string, bool) {
	id := envVarNameToID[strings.ToLower(subfield)]
	switch id {
	case envKeyInterrupts:
		return fmt.Sprintf("%t", macroState.EnvKeyInterrupts), true
	case envClickInterrupts:
		return fmt.Sprintf("%t", macroState.EnvClickInterrupts), true
	case envDebug:
		return fmt.Sprintf("%t", macroState.EnvDebug), true
	case envEcho:
		return fmt.Sprintf("%t", macroState.EnvEcho), true
	case envUnfriendly:
		return fmt.Sprintf("%t", macroState.EnvUnfriendly), true
	case envTextLog:
		return macroState.TextLogBuffer, true
	}
	return "", false
}

// macroGetMyVar returns a player ("@my.*") variable value.
func macroGetMyVar(subfield string) (string, bool) {
	id := playerVarNameToID[subfield]
	switch id {
	case pvarName:
		return macroPlayerName(), true
	case pvarSimpleName:
		return simplePlayerName(macroPlayerName()), true
	case playerLeftItem:
		return macroGetEquippedItemName(kItemSlotLeftHand), true
	case playerRightItem:
		return macroGetEquippedItemName(kItemSlotRightHand), true
	case playerRace:
		return "", true
	case playerGender:
		return "", true
	case playerHealth:
		ds := netGetDrawState()
		if ds != nil {
			return fmt.Sprintf("%d", ds.hp), true
		}
		return "0", true
	case playerBalance:
		ds := netGetDrawState()
		if ds != nil {
			return fmt.Sprintf("%d", ds.bal), true
		}
		return "0", true
	case playerMagic:
		ds := netGetDrawState()
		if ds != nil {
			return fmt.Sprintf("%d", ds.sp), true
		}
		return "0", true
	case playerSharesIn:
		return macroListShares(false), true
	case playerSharesOut:
		return macroListShares(true), true
	case playerSelectedItem:
		return macroGetSelectedItemName(), true
	default:
		// Equipment slots: playerForehead through playerHead
		if id >= playerForehead && id <= playerHead {
			return macroGetEquipmentSlotVar(id), true
		}
	}
	return "", false
}

// netGetDrawState returns the current draw state, or nil when unavailable.
func netGetDrawState() *drawState {
	if gameInstance == nil || gameInstance.net == nil {
		return nil
	}
	return gameInstance.net.GetDrawState()
}

// macroGetSelPlayerVar returns a selected player variable value.
func macroGetSelPlayerVar(subfield string) (string, bool) {
	id := playerVarNameToID[subfield]
	switch id {
	case pvarName:
		return macroSelPlayerName, true
	case pvarSimpleName:
		return macroSelPlayerSimpleName, true
	}
	return "", false
}

// macroGetClickVar returns a click variable value.
func macroGetClickVar(subfield string) (string, bool) {
	id := playerVarNameToID[subfield]
	switch id {
	case pvarName:
		return macroClickName, true
	case pvarSimpleName:
		return macroClickSimpleName, true
	}
	switch subfield {
	case "button":
		return fmt.Sprintf("%d", macroClickButton), true
	case "chord":
		return macroClickChord, true
	}
	return "", false
}

// macroResolveBrackets resolves [expr] patterns in a variable name.
// Each expr is looked up as a global variable and replaced with its value.
// If the expr is not found as a variable, the literal text is kept as-is.
func macroResolveBrackets(name string) string {
	for {
		start := strings.IndexByte(name, '[')
		if start < 0 {
			return name
		}
		end := strings.IndexByte(name[start:], ']')
		if end < 0 {
			return name
		}
		end += start
		inner := name[start+1 : end]
		val, ok := macroFindGlobalVariable(inner)
		if !ok {
			return name
		}
		name = name[:start+1] + val + name[end:]
	}
}

// macroGetUserVariable resolves a user-defined variable (global only here;
// local resolution happens through the executing macro).
func macroGetUserVariable(name string) (string, bool) {
	return macroFindGlobalVariable(name)
}

// macroFindGlobalVariable finds a variable in the global variable list.
func macroFindGlobalVariable(name string) (string, bool) {
	name = macroResolveBrackets(name)
	for m := macroState.GlobalVars; m != nil; m = m.Next {
		if m.VarName == name {
			return m.VarValue, true
		}
	}
	return "", false
}

// macroSetVariable sets a variable from a top-level set/setglobal command.
func macroSetVariable(name, value string, global bool) {
	name = macroResolveBrackets(name)
	if global {
		macroSetGlobalVariable(name, value)
	} else {
		macroSetGlobalVariable(name, value)
	}
}

// macroSetGlobalVariable sets a variable in the global variable list.
func macroSetGlobalVariable(name, value string) {
	if newName, ok := obsoleteVarRemap[name]; ok {
		name = newName
	}
	name = macroResolveBrackets(name)
	for m := macroState.GlobalVars; m != nil; m = m.Next {
		if m.VarName == name {
			m.VarValue = value
			return
		}
	}
	macroState.GlobalVars = &Macro{
		Kind:     macroVariable,
		VarName:  name,
		VarValue: value,
		Next:     macroState.GlobalVars,
	}
}

// macroSetLocalVariable sets a variable in an executing macro's local scope.
func macroSetLocalVariable(ex *ExecutingMacro, name, value string) {
	name = macroResolveBrackets(name)
	for m := ex.Vars; m != nil; m = m.Next {
		if m.VarName == name {
			m.VarValue = value
			return
		}
	}
	ex.Vars = &Macro{
		Kind:     macroVariable,
		VarName:  name,
		VarValue: value,
		Next:     ex.Vars,
	}
}

// macroGetLocalVariable finds a variable in the executing macro's local scope,
// falling back to global scope.
func macroGetLocalVariable(ex *ExecutingMacro, name string) (string, bool) {
	if newName, ok := obsoleteVarRemap[name]; ok {
		name = newName
	}
	name = macroResolveBrackets(name)
	for m := ex.Vars; m != nil; m = m.Next {
		if m.VarName == name {
			return m.VarValue, true
		}
	}
	return macroFindGlobalVariable(name)
}

// macroProcessEscapes converts \r, \n, \t, \", \', \\ in macro text.
func macroProcessEscapes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'r':
				b.WriteByte('\r')
				i++
			case 'n':
				b.WriteByte('\n')
				i++
			case 't':
				b.WriteByte('\t')
				i++
			case '"', '\'', '\\':
				b.WriteByte(s[i+1])
				i++
			default:
				b.WriteByte('\\')
				b.WriteByte(s[i+1])
				i++
			}
		} else {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// macroResolveExpression resolves a macro command parameter, handling
// variable references, quotes, and text operations.
func macroResolveExpression(ex *ExecutingMacro, expr string) string {
	s := strings.TrimSpace(expr)
	if len(s) == 0 {
		return s
	}

	// Strip matching quotes and process escape sequences
	if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
		return macroProcessEscapes(s[1 : len(s)-1])
	}

	// Variable reference with @ prefix
	if strings.HasPrefix(s, "@") {
		if strings.HasPrefix(s, "@text.") {
			return macroResolveTextOp(ex, s)
		}
		val, ok := macroGetLocalVariable(ex, s)
		if ok {
			return val
		}
		if val, ok := macroResolveVariable(s); ok {
			return val
		}
		if val, ok := macroResolveVarSuffix(ex, s); ok {
			return val
		}
		return s
	}

	// Handle .word[N], .letter[N], .num_words, .num_letters suffixes on bare vars
	if val, ok := macroResolveVarSuffix(ex, s); ok {
		return val
	}

	// Handle bracket-indexed variable references on the RHS (e.g., dataset[cknum])
	if idx := strings.IndexByte(s, '['); idx > 0 && strings.HasSuffix(s, "]") {
		base := s[:idx]
		inner := s[idx+1 : len(s)-1]
		resolved := macroResolveExpression(ex, inner)
		fullName := base + "[" + resolved + "]"
		if val, ok := macroGetLocalVariable(ex, fullName); ok {
			return val
		}
		if val, ok := macroFindGlobalVariable(fullName); ok {
			return val
		}
	}

	if val, ok := macroGetLocalVariable(ex, s); ok {
		return val
	}
	if val, ok := macroFindGlobalVariable(s); ok {
		return val
	}

	return s
}

// macroResolveBaseVar resolves a base variable name for suffix operations,
// trying local, global, and built-in namespace variables.
func macroResolveBaseVar(ex *ExecutingMacro, name string) (string, bool) {
	val, ok := macroGetLocalVariable(ex, name)
	if ok {
		return val, true
	}
	val, ok = macroFindGlobalVariable(name)
	if ok {
		return val, true
	}
	if strings.HasPrefix(name, "@") {
		val, ok = macroResolveVariable(name)
		if ok {
			return val, true
		}
	}
	return "", false
}

// macroResolveVarSuffix handles .word[N], .letter[N], .num_words, .num_letters
// suffixes on variable names. Supports literal and variable-based indices.
func macroResolveVarSuffix(ex *ExecutingMacro, name string) (string, bool) {
	s := name

	if strings.HasSuffix(s, ".num_words") {
		base := s[:len(s)-len(".num_words")]
		val, ok := macroResolveBaseVar(ex, base)
		if !ok {
			return "", false
		}
		return fmt.Sprintf("%d", len(strings.Fields(val))), true
	}

	if strings.HasSuffix(s, ".num_letters") {
		base := s[:len(s)-len(".num_letters")]
		val, ok := macroResolveBaseVar(ex, base)
		if !ok {
			return "", false
		}
		return fmt.Sprintf("%d", len([]rune(val))), true
	}

	// .word[N] or .letter[N] suffix - find the rightmost one
	wordIdx := strings.LastIndex(s, ".word[")
	letterIdx := strings.LastIndex(s, ".letter[")
	hasClose := strings.HasSuffix(s, "]")

	if wordIdx >= 0 && hasClose && (letterIdx < 0 || wordIdx > letterIdx) {
		nStr := s[wordIdx+6 : len(s)-1]
		n, err := strconv.Atoi(nStr)
		if err != nil {
			resolved := macroResolveExpression(ex, nStr)
			n, err = strconv.Atoi(resolved)
		}
		if err != nil || n < 0 {
			return "", false
		}
		base := s[:wordIdx]
		val, ok := macroResolveBaseVar(ex, base)
		if !ok {
			return "", false
		}
		words := strings.Fields(val)
		if n < len(words) {
			return words[n], true
		}
		return "", false
	}

	if letterIdx >= 0 && hasClose && (wordIdx < 0 || letterIdx > wordIdx) {
		nStr := s[letterIdx+8 : len(s)-1]
		n, err := strconv.Atoi(nStr)
		if err != nil {
			resolved := macroResolveExpression(ex, nStr)
			n, err = strconv.Atoi(resolved)
		}
		if err != nil || n < 0 {
			return "", false
		}
		base := s[:letterIdx]
		val, ok := macroResolveBaseVar(ex, base)
		if !ok {
			return "", false
		}
		runes := []rune(val)
		if n < len(runes) {
			return string(runes[n]), true
		}
		return "", false
	}

	return "", false
}

// macroResolveTextOp handles @text.word[N], @text.letter[N],
// @text.num_words, @text.num_letters.
func macroResolveTextOp(ex *ExecutingMacro, expr string) string {
	textVal, _ := macroGetLocalVariable(ex, "@text")

	suffix := strings.TrimPrefix(expr, "@text.")
	if strings.HasPrefix(suffix, "word[") {
		idxStr := strings.TrimSuffix(strings.TrimPrefix(suffix, "word["), "]")
		idx, err := strconv.Atoi(idxStr)
		if err != nil || idx < 0 {
			return ""
		}
		words := strings.Fields(textVal)
		if idx >= len(words) {
			return ""
		}
		return words[idx]
	}
	if strings.HasPrefix(suffix, "letter[") {
		idxStr := strings.TrimSuffix(strings.TrimPrefix(suffix, "letter["), "]")
		idx, err := strconv.Atoi(idxStr)
		if err != nil || idx < 0 {
			return ""
		}
		runes := []rune(textVal)
		if idx >= len(runes) {
			return ""
		}
		return string(runes[idx])
	}
	if suffix == "num_words" {
		return fmt.Sprintf("%d", len(strings.Fields(textVal)))
	}
	if suffix == "num_letters" {
		return fmt.Sprintf("%d", len([]rune(textVal)))
	}
	return ""
}

// macroGetEquippedItemName returns the name of the item in the given slot.
func macroGetEquippedItemName(slot int) string {
	if gameInstance == nil || gameInstance.net == nil || gameInstance.clImages == nil {
		return "Nothing"
	}
	items := gameInstance.net.GetInventory()
	for _, it := range items {
		if !it.equipped {
			continue
		}
		if gameInstance.clImages.ItemSlot(uint32(it.id)) == slot {
			return it.name
		}
	}
	return "Nothing"
}

// macroGetEquipmentSlotVar returns the equipped item name for a player
// variable ID (playerForehead through playerHead).
func macroGetEquipmentSlotVar(varID int) string {
	var slot int
	for s, vid := range playerSlotToVar {
		if vid == varID {
			slot = s
			break
		}
	}
	if slot == 0 {
		return ""
	}
	return macroGetEquippedItemName(slot)
}

// macroGetSelectedItemName returns the name of the currently selected item.
func macroGetSelectedItemName() string {
	if gameInstance == nil || gameInstance.net == nil {
		return ""
	}
	idx := gameInstance.net.GetSelectedInvIdx()
	if idx < 0 {
		return ""
	}
	items := gameInstance.net.GetInventory()
	if idx >= len(items) {
		return ""
	}
	return items[idx].name
}

// macroListShares returns a comma-separated list of shared players.
// Shares aren't exposed by the current client state; stub returns empty.
func macroListShares(isOut bool) string {
	return ""
}

// simplePlayerName strips non-alphanumeric characters from a name.
func simplePlayerName(name string) string {
	var b strings.Builder
	for _, ch := range name {
		if ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' {
			b.WriteRune(ch)
		}
	}
	return b.String()
}

// macroEvalCondition evaluates a condition like "value1 > value2".
// Returns the boolean result.
func macroEvalCondition(ex *ExecutingMacro, cmd *Macro, params []*Macro) bool {
	if len(params) < 3 {
		return false
	}
	val1 := macroResolveExpression(ex, params[0].VarName)
	op := params[1].VarName
	val2 := macroResolveExpression(ex, params[2].VarName)

	result := false

	// Try numeric comparison
	n1, err1 := strconv.Atoi(val1)
	n2, err2 := strconv.Atoi(val2)
	if err1 == nil && err2 == nil {
		switch op {
		case ">":
			result = n1 > n2
		case "<":
			result = n1 < n2
		case ">=":
			result = n1 >= n2
		case "<=":
			result = n1 <= n2
		case "==":
			result = n1 == n2
		case "!=":
			result = n1 != n2
		}
	} else {
		// String comparison
		switch op {
		case ">":
			result = strings.Contains(strings.ToLower(val2), strings.ToLower(val1))
		case "<":
			result = strings.Contains(strings.ToLower(val1), strings.ToLower(val2))
		case ">=":
			result = strings.Contains(strings.ToLower(val1), strings.ToLower(val2))
		case "<=":
			result = strings.Contains(strings.ToLower(val2), strings.ToLower(val1))
		case "==":
			result = strings.EqualFold(val1, val2)
		case "!=":
			result = !strings.EqualFold(val1, val2)
		}
	}

	// Dedup: once an if-node matches on a textlog line, don't re-fire for
	// the same line on later loop/poll evaluations.
	if result && ex != nil && cmd != nil {
		tl := macroState.TextLogBuffer
		if tl != "" && (val1 == tl || val2 == tl) {
			seq := atomic.LoadUint64(&macroState.TextLogSeq)
			if ex.TextLogIfs == nil {
				ex.TextLogIfs = make(map[*Macro]uint64)
			}
			if last, ok := ex.TextLogIfs[cmd]; ok && last == seq {
				return false
			}
			ex.TextLogIfs[cmd] = seq
		}
	}

	return result
}