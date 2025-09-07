//go:build plugin

package main

import (
    "gt"
)

// Minimal metadata required by the client
const PluginName = "APISmoke"
const PluginAuthor = "Test"
const PluginCategory = "Tests"
const PluginAPIVersion = 1

// Init exercises a subset of the script API in a predictable way.
// The Go unit test loads this script and verifies these side effects
// by inspecting storage, shortcuts, hotkeys, and triggers.
func Init() {
    // Basic console output (only shown when debug enabled)
    gt.Print("api: init")

    // Shortcut installs an input handler for this owner
    gt.AddShortcut("yy", "/yell ")

    // Register a command that records its args via storage
    gt.RegisterCommand("apit_cmd", func(args string) {
        gt.Save("last_args", args)
        gt.Print("cmd:" + args)
    })

    // Function hotkey that records a flag when triggered
    gt.Key("Ctrl-Alt-T", func() {
        gt.Save("hotkey", "triggered")
    })

    // Chat trigger and console trigger record markers
    gt.Chat("ping", func(msg string) {
        gt.Save("chat", "ping")
        gt.Print("chat:" + msg)
    })
    gt.Console("ready", func(msg string) {
        gt.Save("console", "ready")
    })

    // Set and expose input text for assertion
    gt.SetInputText("test-in")

    // Indicate init completed
    gt.Save("started", "yes")
}

