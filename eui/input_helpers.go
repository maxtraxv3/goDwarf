package eui

import (
    "fmt"
    "runtime/debug"
    "strings"
    "unicode"
    "github.com/maxtraxv3/goDwarf/clanmacro" // <-- REPLACE with your module path, e.g. "github.com/Distortions81/goThoom/clanmacro"
)

// DispatchReplacementForWord looks up a replacement for `word` and returns it.
// It uses the clanmacro registry.
func DispatchReplacementForWord(word string) string {
    if word == "" {
        return word
    }
    repl, ok := clanmacro.ReplacementFor(word)
    if !ok {
        return word
    }
    return repl
}

// DispatchExpressionFromText dispatches a typed expression (e.g., when Enter is pressed).
// If a macro matched and requested submission, it will call clanmacro.SubmitExpanded.
func DispatchExpressionFromText(text string) {
    if strings.TrimSpace(text) == "" {
        return
    }
    expanded, handled, submit := clanmacro.DispatchExpression(text)
    if !handled {
        return
    }
    // If macro requested submit, call the registered send function inside clanmacro.
    if submit {
        clanmacro.SubmitExpanded(expanded)
        return
    }
    // Otherwise, replace the input text with the expanded text (if needed).
    // This behavior is optional; here we just print debug info.
    fmt.Println("clanmacro: expanded (no submit):", expanded)
}

// HandleFocusedInputReplacement attempts to replace the last word in focusedItem.Text
// using the clanmacro registry. This version is rune-safe and defensive.
func HandleFocusedInputReplacement(focusedItem *itemData) {
    if focusedItem == nil {
        return
    }

    // Defensive recover so a macro cannot crash the UI.
    defer func() {
        if r := recover(); r != nil {
            fmt.Printf("panic in HandleFocusedInputReplacement: %v\n", r)
            debug.PrintStack()
        }
    }()

    text := focusedItem.Text
    if strings.TrimSpace(text) == "" {
        return
    }

    // Work with runes to avoid byte/rune index mismatches.
    runes := []rune(text)

    // Find end index of trimmed runes (exclude trailing whitespace)
    end := len(runes)
    for end > 0 {
        if unicode.IsSpace(runes[end-1]) {
            end--
            continue
        }
        break
    }
    if end == 0 {
        return
    }

    // Find start index of last word
    start := end - 1
    for start >= 0 {
        if unicode.IsSpace(runes[start]) {
            start++
            break
        }
        start--
    }
    if start < 0 {
        start = 0
    }

    lastWord := string(runes[start:end])
    if strings.TrimSpace(lastWord) == "" {
        return
    }

    // Lookup replacement safely
    repl := DispatchReplacementForWord(lastWord)
    if repl == "" || repl == lastWord {
        return
    }

    // Build new rune slice: prefix + replacement + trailing whitespace
    prefix := string(runes[:start])
    trailing := string(runes[end:]) // trailing whitespace (if any)
    newText := prefix + repl + trailing

    // Update focused item safely
    focusedItem.Text = newText
    if focusedItem.TextPtr != nil {
        *focusedItem.TextPtr = focusedItem.Text
    }
    // Cursor at end of replacement + trailing whitespace (rune count)
    focusedItem.CursorPos = len([]rune(prefix + repl + trailing))
    focusedItem.markDirty()
}
