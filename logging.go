package godwarf

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

var (
	textLogFile     *os.File
	textLogMu       sync.Mutex
	textLogCharName string
	textLogHasEntry bool
)

func textLogsDir() string {
	return filepath.Join(goDwarfDir(), "Text Log")
}

func macrosDir() string {
	return filepath.Join(goDwarfDir(), "macros")
}

func debugLogsDir() string {
	return filepath.Join(goDwarfDir(), "debuglogs")
}

func goDwarfDir() string {
	for _, dir := range []string{
		"/storage/emulated/0/Documents/goDwarf",
		"/sdcard/Documents/goDwarf",
	} {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir
		}
	}
	return "/storage/emulated/0/Documents/goDwarf"
}

func storageOK() bool {
	if runtime.GOOS != "android" {
		return true
	}
	dir := goDwarfDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false
	}
	return os.MkdirAll(filepath.Join(dir, "macros"), 0755) == nil
}

func StartTextLog(charName string) {
	textLogMu.Lock()
	defer textLogMu.Unlock()

	if textLogFile != nil {
		textLogFile.Close()
		textLogFile = nil
	}

	if charName == "" {
		charName = "unknown"
	}
	textLogCharName = charName
	textLogHasEntry = false

	dir := filepath.Join(textLogsDir(), charName)
	os.MkdirAll(dir, 0755)

	t := time.Now()
	fname := fmt.Sprintf("CL Log %04d/%02d/%02d %02d.%02d.%02d.txt",
		t.Year(), t.Month(), t.Day(),
		t.Hour(), t.Minute(), t.Second())

	fpath := filepath.Join(dir, fname)
	os.MkdirAll(filepath.Dir(fpath), 0755)

	f, err := os.OpenFile(fpath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		flog(fmt.Sprintf("text log open error: %v", err))
		return
	}
	textLogFile = f
	flog("text log started: " + fpath)
}

func CloseTextLog() {
	textLogMu.Lock()
	defer textLogMu.Unlock()
	if textLogFile != nil {
		textLogFile.Close()
		textLogFile = nil
	}
}

func logTextMessage(msg string) {
	textLogMu.Lock()
	defer textLogMu.Unlock()
	if textLogFile == nil {
		return
	}

	if textLogHasEntry {
		textLogFile.WriteString("\n")
	}
	textLogHasEntry = true

	t := time.Now()
	hour := t.Hour()
	ampm := "a"
	if hour >= 12 {
		ampm = "p"
	}
	hour12 := (hour+11)%12 + 1
	ts := fmt.Sprintf("%d/%d/%02d %d:%02d:%02d%s ",
		t.Month(), t.Day(), t.Year()%100,
		hour12, t.Minute(), t.Second(), ampm)

	textLogFile.WriteString(ts + msg)
	textLogFile.Sync()
}

func macroFilePath(charName string) string {
	if charName == "" {
		charName = "Default"
	}
	return filepath.Join(macrosDir(), charName)
}

func defaultMacroPath() string {
	return filepath.Join(macrosDir(), "Default")
}

func ensureMacroFolder(charName string) {
	dir := macrosDir()
	os.MkdirAll(dir, 0755)

	if charName != "" {
		charPath := macroFilePath(charName)
		if _, err := os.Stat(charPath); os.IsNotExist(err) {
			os.WriteFile(charPath, []byte(fmt.Sprintf("include \"Default\"\n")), 0644)
		}
	}

	instPath := filepath.Join(dir, "Macro Instructions")
	if _, err := os.Stat(instPath); os.IsNotExist(err) {
		os.WriteFile(instPath, []byte(`goDwarf Macro Instructions
========================

Macros are text commands that expand into longer commands.

Format:
  "trigger" { /command to send }

Examples:
  "hh" { /action cast heal }
  "cc" { /action cast cure }
  "ll" { /look }

To use: Type the trigger text and press Enter.
The trigger is replaced with the full command.

Per-character macros are in files named after your character.
Default macros apply to all characters.
`), 0644)
	}
}
