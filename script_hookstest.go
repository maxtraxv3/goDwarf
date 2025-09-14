package main

// testScriptHooks emits sample chat and console messages to exercise script trigger hooks.
func testScriptHooks() {
	// NPC chat with a blank name
	chatMessage(" says, testing NPC chat")
	// System console message
	consoleMessage("System: testing message")
	// Simulated share message using info text format
	msg := append([]byte("You are sharing experiences with "), pnTag("Tester")...)
	msg = append(msg, '.')
	handleInfoText(append(bepp("sh", msg), '\r'))
}
