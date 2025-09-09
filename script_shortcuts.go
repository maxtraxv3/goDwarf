package main

import (
    "encoding/json"
    "fmt"
    "log"
    "os"
    "path/filepath"
    "strings"
    "sync"
)

// This file implements helpers for chat shortcuts registered by scripts.
// Shortcuts replace a short bit of text with a longer command, making it easy
// to type common actions. The helpers below manage shortcuts on behalf of a
// script.

var (
	// shortcutMu guards access to shortcutMaps so that multiple scripts can add
	// shortcuts safely.
	shortcutMu sync.RWMutex
	// shortcutMaps keeps shortcuts separate for each script by name.
    shortcutMaps = map[string]map[string]string{}
)

const shortcutsFile = "shortcuts.json"

// pluginAddShortcut registers a single shortcut for the script identified by owner.
// Typing short text in the chat box will expand into the full string before
// being sent.  For example, adding ("pp", "/ponder ") means that typing
// "pp" or "pp hello" becomes "/ponder " or "/ponder hello" respectively.
func addShortcut(owner, short, full string) {
	short = strings.ToLower(short)
	shortcutMu.Lock()
	m := shortcutMaps[owner]
	if m == nil {
		m = map[string]string{}
		shortcutMaps[owner] = m
		pluginRegisterInputHandler(owner, func(txt string) string {
			shortcutMu.RLock()
			local := shortcutMaps[owner]
			shortcutMu.RUnlock()
			lower := strings.ToLower(txt)
			for k, v := range local {
				if strings.HasPrefix(lower, k) {
					if len(lower) == len(k) {
						return v
					}
					if len(lower) > len(k) && lower[len(k)] == ' ' {
						return v + txt[len(k)+1:]
					}
				}
			}
			return txt
		})
	}
    m[short] = full
    shortcutMu.Unlock()
    refreshShortcutsList()
}

func pluginAddShortcut(owner, short, full string) {
	if pluginIsDisabled(owner) {
		return
	}
	addShortcut(owner, short, full)
	name := pluginDisplayNames[owner]
	if name == "" {
		name = owner
	}
	msg := fmt.Sprintf("[plugin:%s] shortcut added: %s -> %s", name, short, full)
	if gs.pluginOutputDebug {
		consoleMessage(msg)
	}
	log.Print(msg)
}

// pluginAddShortcuts registers many shortcuts at once for the given script.
func pluginAddShortcuts(owner string, shortcuts map[string]string) {
	if pluginIsDisabled(owner) {
		return
	}
	for k, v := range shortcuts {
		pluginAddShortcut(owner, k, v)
	}
}

// pluginRemoveShortcuts deletes all shortcuts registered by the specified script.
// It is typically called when a script is disabled or unloaded so that any
// previously registered shortcut prefixes no longer expand.
func pluginRemoveShortcuts(owner string) {
	shortcutMu.Lock()
	delete(shortcutMaps, owner)
	shortcutMu.Unlock()
	refreshShortcutsList()
	name := pluginDisplayNames[owner]
	if name == "" {
		name = owner
	}
	msg := fmt.Sprintf("[plugin:%s] shortcuts removed", name)
	if gs.pluginOutputDebug {
		consoleMessage(msg)
	}
	log.Print(msg)
}

func removeShortcut(owner, short string) {
    short = strings.ToLower(short)
    shortcutMu.Lock()
    if m := shortcutMaps[owner]; m != nil {
        delete(m, short)
        if len(m) == 0 {
            delete(shortcutMaps, owner)
        }
    }
    shortcutMu.Unlock()
    refreshShortcutsList()
    if owner == "user" || owner == "global" {
        saveShortcuts()
    }
}

func addUserShortcut(short, full string) {
    addShortcut("user", short, full)
    saveShortcuts()
}

func addGlobalShortcut(short, full string) {
    addShortcut("global", short, full)
    saveShortcuts()
}

func removeUserShortcut(short string) {
	removeShortcut("user", short)
}

func removeGlobalShortcut(short string) {
    removeShortcut("global", short)
}

// loadShortcuts loads persisted user and global shortcuts from disk.
func loadShortcuts() {
    type fileFmt struct {
        User   map[string]string `json:"user,omitempty"`
        Global map[string]string `json:"global,omitempty"`
    }
    path := filepath.Join(dataDirPath, shortcutsFile)
    data, err := os.ReadFile(path)
    if err != nil {
        return
    }
    var f fileFmt
    if err := json.Unmarshal(data, &f); err != nil {
        return
    }
    // Populate using addShortcut to ensure input handlers are registered.
    for k, v := range f.User {
        if k != "" && v != "" {
            addShortcut("user", k, v)
        }
    }
    for k, v := range f.Global {
        if k != "" && v != "" {
            addShortcut("global", k, v)
        }
    }
}

// saveShortcuts persists user and global shortcuts to disk.
func saveShortcuts() {
    type fileFmt struct {
        User   map[string]string `json:"user,omitempty"`
        Global map[string]string `json:"global,omitempty"`
    }
    shortcutMu.RLock()
    // Copy only user/global maps to avoid persisting plugin-defined shortcuts.
    out := fileFmt{}
    if m := shortcutMaps["user"]; len(m) > 0 {
        out.User = make(map[string]string, len(m))
        for k, v := range m {
            out.User[k] = v
        }
    }
    if m := shortcutMaps["global"]; len(m) > 0 {
        out.Global = make(map[string]string, len(m))
        for k, v := range m {
            out.Global[k] = v
        }
    }
    shortcutMu.RUnlock()

    data, err := json.MarshalIndent(out, "", "  ")
    if err != nil {
        return
    }
    _ = os.MkdirAll(dataDirPath, 0o755)
    path := filepath.Join(dataDirPath, shortcutsFile)
    _ = os.WriteFile(path, data, 0o644)
}
