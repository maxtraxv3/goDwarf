// clanmacro/registry.go
package clanmacro

import (
    "bufio"
    "fmt"
    "os"
    "path/filepath"
    "regexp"
    "strings"
    "sync"
)

// Macro represents a single Clan Lord style macro.
type Macro struct {
    Trigger  string // trigger token, e.g. "gg"
    Template string // template, e.g. "/give @text"
    Submit   bool   // whether template ends with a submit token (\r)
}

// registry holds loaded macros keyed by lowercased trigger.
var (
    mu       sync.RWMutex
    registry = map[string]Macro{}
    sendFunc func(string)
)

// SetSendFunc registers a callback that will be used to submit expanded macro text.
// Call this from your main/init code to wire the client's send-chat function.
func SetSendFunc(f func(string)) {
    mu.Lock()
    defer mu.Unlock()
    sendFunc = f
}

// SubmitExpanded calls the registered send function with expanded text.
// It is safe to call even if no send function is registered.
func SubmitExpanded(expanded string) {
    mu.RLock()
    f := sendFunc
    mu.RUnlock()
    if f != nil {
        f(expanded)
    }
}

// ReplacementFor returns a replacement for a single-word trigger (used on separator).
// If no replacement exists, ok == false.
func ReplacementFor(word string) (replacement string, ok bool) {
    if word == "" {
        return "", false
    }
    key := strings.ToLower(strings.TrimSpace(word))
    mu.RLock()
    m, found := registry[key]
    mu.RUnlock()
    if !found {
        return "", false
    }
    // If the template contains @text, but we only have a single word, replace with empty.
    repl := strings.ReplaceAll(m.Template, "@text", "")
    // If template had trailing space, keep it.
    return repl, true
}

// DispatchExpression attempts to expand a full-line expression. It returns:
//   expanded: the expanded text (with @text replaced)
//   handled: whether a macro matched and expansion occurred
//   submit: whether the macro requested submission (template ended with \r)
func DispatchExpression(line string) (expanded string, handled bool, submit bool) {
    line = strings.TrimRight(line, "\r\n")
    if line == "" {
        return "", false, false
    }

    // Split into first token and rest
    parts := strings.Fields(line)
    if len(parts) == 0 {
        return "", false, false
    }
    trigger := strings.ToLower(parts[0])
    rest := ""
    if len(parts) > 1 {
        rest = strings.Join(parts[1:], " ")
    }

    mu.RLock()
    m, ok := registry[trigger]
    mu.RUnlock()
    if !ok {
        return "", false, false
    }

    // Replace @text with the rest of the line
    exp := strings.ReplaceAll(m.Template, "@text", rest)

    // If template contains literal \r at the end, treat as submit and strip it
    if strings.HasSuffix(exp, `\r`) {
        exp = strings.TrimSuffix(exp, `\r`)
        submit = true
    }

    return exp, true, submit
}

// LoadDir loads all macro files from dirPath. It will parse each line that looks like:
//   "trigger" "template" [@text] ["\r"]
// The parser is permissive: it extracts the first two quoted strings and inspects template for @text and \r.
func LoadDir(dirPath string) error {
    if dirPath == "" {
        return fmt.Errorf("empty macro directory")
    }
    files, err := filepath.Glob(filepath.Join(dirPath, "*"))
    if err != nil {
        return err
    }
    for _, f := range files {
        if fi, err := os.Stat(f); err == nil && fi.Mode().IsRegular() {
            if err := loadFile(f); err != nil {
                // continue loading other files but report error
                fmt.Printf("clanmacro: failed to load %s: %v\n", f, err)
            }
        }
    }
    return nil
}

var quotedRE = regexp.MustCompile(`"([^"]*)"`) // captures quoted substrings

// loadFile parses a single macro file.
func loadFile(path string) error {
    f, err := os.Open(path)
    if err != nil {
        return err
    }
    defer f.Close()

    scanner := bufio.NewScanner(f)
    lineNo := 0
    for scanner.Scan() {
        lineNo++
        line := strings.TrimSpace(scanner.Text())
        if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
            continue
        }
        matches := quotedRE.FindAllStringSubmatch(line, -1)
        if len(matches) < 2 {
            // not a macro line we can parse
            continue
        }
        trigger := matches[0][1]
        template := matches[1][1]

        // If template contains literal \r at end, keep it in Template so DispatchExpression can detect it.
        // Normalize trigger to lower-case for case-insensitive matching.
        m := Macro{
            Trigger:  strings.ToLower(strings.TrimSpace(trigger)),
            Template: template,
            Submit:   strings.HasSuffix(template, `\r`),
        }

        mu.Lock()
        registry[m.Trigger] = m
        mu.Unlock()
    }
    if err := scanner.Err(); err != nil {
        return err
    }
    return nil
}

// RegisterMacro allows programmatic registration of a macro.
func RegisterMacro(trigger, template string) {
    if trigger == "" {
        return
    }
    m := Macro{
        Trigger:  strings.ToLower(strings.TrimSpace(trigger)),
        Template: template,
        Submit:   strings.HasSuffix(template, `\r`),
    }
    mu.Lock()
    registry[m.Trigger] = m
    mu.Unlock()
}
