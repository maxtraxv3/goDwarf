package godwarf

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type CommandResult struct {
	Sent      bool   // true = send to server, false = handled locally
	Text      string // text to send to server if Sent=true
	Response  string // local response to show in chat
}

func handleClientCommand(input string) CommandResult {
	if !strings.HasPrefix(input, "/") && !strings.HasPrefix(input, "\\") {
		return CommandResult{Sent: true, Text: input}
	}

	cmd := strings.TrimLeft(input, "/\\")
	parts := strings.SplitN(cmd, " ", 2)
	name := strings.ToLower(parts[0])
	arg := ""
	if len(parts) > 1 {
		arg = parts[1]
	}

	switch name {
	case "select":
		return cmdSelect(arg)
	case "selectitem":
		return cmdSelectItem(arg)
	case "move":
		return cmdMove(arg)
	case "follow":
		return cmdFollow(arg)
	case "label":
		return cmdLabel(arg)
	case "wholabel":
		return cmdWhoLabel(arg)
	case "block":
		return cmdBlock(arg)
	case "ignore":
		return cmdIgnore(arg)
	case "forget":
		return cmdForget(arg)
	case "pref":
		return cmdPref(arg)
	case "roll":
		return cmdRoll(arg)
	case "play":
		return cmdPlay(arg)
	case "macro":
		return cmdMacro(arg)
	case "help":
		return cmdHelp(arg)
	case "time":
		return CommandResult{Response: time.Now().Format("15:04:05")}
	default:
		return CommandResult{Sent: true, Text: input}
	}
}

func cmdSelect(arg string) CommandResult {
	if arg == "" {
		return CommandResult{Response: "Usage: /select <player> or /select @next|@prev|@first|@last"}
	}
	if gameInstance == nil || gameInstance.net == nil {
		return CommandResult{Response: "Not connected"}
	}
	players := gameInstance.net.GetPlayerNames()
	if len(players) == 0 {
		return CommandResult{Response: "No players visible"}
	}

	lower := strings.ToLower(arg)
	switch lower {
	case "@next", "next":
		idx := gameInstance.net.GetSelectedIdx()
		idx = (idx + 1) % len(players)
		gameInstance.net.SetSelectedIdx(idx)
		return CommandResult{Response: fmt.Sprintf("Selected: %s", players[idx])}
	case "@prev", "prev":
		idx := gameInstance.net.GetSelectedIdx()
		idx = (idx - 1 + len(players)) % len(players)
		gameInstance.net.SetSelectedIdx(idx)
		return CommandResult{Response: fmt.Sprintf("Selected: %s", players[idx])}
	case "@first", "first":
		gameInstance.net.SetSelectedIdx(0)
		return CommandResult{Response: fmt.Sprintf("Selected: %s", players[0])}
	case "@last", "last":
		gameInstance.net.SetSelectedIdx(len(players) - 1)
		return CommandResult{Response: fmt.Sprintf("Selected: %s", players[len(players)-1])}
	case "@none", "none", "clear", "":
		gameInstance.net.SetSelectedIdx(-1)
		return CommandResult{Response: "Selection cleared"}
	}

	for i, p := range players {
		if strings.ToLower(p) == lower {
			gameInstance.net.SetSelectedIdx(i)
			return CommandResult{Response: fmt.Sprintf("Selected: %s", p)}
		}
	}
	gameInstance.net.SetSelectedIdx(-1)
	return CommandResult{Response: fmt.Sprintf("Player '%s' not found", arg)}
}

func cmdSelectItem(arg string) CommandResult {
	if gameInstance == nil || gameInstance.net == nil {
		return CommandResult{Response: "Not connected"}
	}
	lower := strings.ToLower(strings.TrimSpace(arg))
	switch lower {
	case "@next", "next", "@prev", "prev", "@first", "first", "@last", "last":
		return CommandResult{Response: "Relative item selection not yet implemented"}
	case "@none", "none", "clear", "":
		gameInstance.net.SetSelectedInvIdx(-1)
		return CommandResult{Response: "Item selection cleared"}
	}
	items := gameInstance.net.GetInventoryItems()
	if len(items) == 0 {
		return CommandResult{Response: "No items in inventory"}
	}
	for i, item := range items {
		if strings.ToLower(item.name) == lower || strings.ToLower(item.base) == lower {
			gameInstance.net.SetSelectedInvIdx(i)
			return CommandResult{Response: fmt.Sprintf("Selected: %s", item.name)}
		}
	}
	gameInstance.net.SetSelectedInvIdx(-1)
	return CommandResult{Response: fmt.Sprintf("Item '%s' not found", arg)}
}

func cmdMove(arg string) CommandResult {
	if arg == "" || strings.ToLower(arg) == "stop" {
		return CommandResult{Response: "Movement stopped"}
	}

	parts := strings.Fields(arg)
	dir := strings.ToLower(parts[0])

	dx, dy := 0.0, 0.0
	switch dir {
	case "n", "north":
		dy = -1
	case "s", "south":
		dy = 1
	case "e", "east":
		dx = 1
	case "w", "west":
		dx = -1
	case "ne":
		dx, dy = 0.707, -0.707
	case "nw":
		dx, dy = -0.707, -0.707
	case "se":
		dx, dy = 0.707, 0.707
	case "sw":
		dx, dy = -0.707, 0.707
	default:
		return CommandResult{Response: fmt.Sprintf("Unknown direction: %s (use n/s/e/w/ne/nw/se/sw)", dir)}
	}

	run := false
	if len(parts) > 1 && strings.ToLower(parts[1]) == "run" {
		run = true
	}

	if gameInstance != nil && gameInstance.net != nil && gameInstance.net.Connected() {
		go func() {
			dur := 500 * time.Millisecond
			if run {
				dur = 200 * time.Millisecond
			}
			gameInstance.net.SendInput(float32(dx), float32(dy), true)
			time.Sleep(dur)
			gameInstance.net.SendInput(0, 0, false)
		}()
	}
	return CommandResult{Response: fmt.Sprintf("Moving %s", dir)}
}

func cmdFollow(arg string) CommandResult {
	if arg == "" || strings.ToLower(arg) == "stop" || strings.ToLower(arg) == "off" {
		return CommandResult{Response: "/follow not yet implemented (use joystick)"}
	}
	return CommandResult{Response: fmt.Sprintf("/follow %s not yet implemented (use joystick)", arg)}
}

func cmdLabel(arg string) CommandResult {
	if arg == "" || arg == "?" {
		return CommandResult{Response: "Usage: /label <player> [red|orange|green|blue|purple|none|0-5]"}
	}
	if gameInstance == nil || gameInstance.net == nil {
		return CommandResult{Response: "Not connected"}
	}
	parts := strings.SplitN(arg, " ", 2)
	playerName := strings.TrimSpace(parts[0])
	if playerName == "" || playerName == "?" {
		return CommandResult{Response: "Usage: /label <player> [red|orange|green|blue|purple|none|0-5]"}
	}
	labelNum := kFriendLabel1 // default
	if len(parts) > 1 {
		labelStr := strings.TrimSpace(parts[1])
		if ln, ok := friendLabelNames[strings.ToLower(labelStr)]; ok {
			labelNum = ln
		} else {
			return CommandResult{Response: fmt.Sprintf("Unknown label: %s (use red/orange/green/blue/purple/none/0-5)", labelStr)}
		}
	}
	gameInstance.net.SetLabel(playerName, labelNum)
	if labelNum == kFriendNone {
		return CommandResult{Response: fmt.Sprintf("Cleared label on %s", playerName)}
	}
	return CommandResult{Response: fmt.Sprintf("Labeled %s as %s", playerName, friendLabelName(labelNum))}
}

func cmdWhoLabel(arg string) CommandResult {
	if arg == "?" {
		return CommandResult{Response: "Usage: /wholabel [label] — list labeled players"}
	}
	if gameInstance == nil || gameInstance.net == nil {
		return CommandResult{Response: "Not connected"}
	}
	labelFilter := -1 // all
	if arg != "" {
		if ln, ok := friendLabelNames[strings.ToLower(strings.TrimSpace(arg))]; ok {
			labelFilter = ln
		} else {
			return CommandResult{Response: fmt.Sprintf("Unknown label: %s", arg)}
		}
	}
	names := gameInstance.net.GetLabeledPlayers(labelFilter)
	if len(names) == 0 {
		if labelFilter >= 0 {
			return CommandResult{Response: fmt.Sprintf("No players with label %s", friendLabelName(labelFilter))}
		}
		return CommandResult{Response: "No labeled players"}
	}
	var sb strings.Builder
	for i, name := range names {
		if i > 0 {
			sb.WriteString(", ")
		}
		label := gameInstance.net.GetLabel(name)
		if labelFilter >= 0 {
			sb.WriteString(name)
		} else {
			sb.WriteString(fmt.Sprintf("%s [%s]", name, friendLabelName(label)))
		}
	}
	return CommandResult{Response: sb.String()}
}

func cmdBlock(arg string) CommandResult {
	if arg == "" {
		return CommandResult{Response: "Usage: /block <player>"}
	}
	if gameInstance != nil && gameInstance.net != nil {
		gameInstance.net.ToggleBlock(arg)
		return CommandResult{Response: fmt.Sprintf("Toggled block on %s", arg)}
	}
	return CommandResult{Response: "Not connected"}
}

func cmdIgnore(arg string) CommandResult {
	if arg == "" {
		return CommandResult{Response: "Usage: /ignore <player>"}
	}
	if gameInstance != nil && gameInstance.net != nil {
		gameInstance.net.ToggleIgnore(arg)
		return CommandResult{Response: fmt.Sprintf("Toggled ignore on %s", arg)}
	}
	return CommandResult{Response: "Not connected"}
}

func cmdForget(arg string) CommandResult {
	if arg == "" {
		return CommandResult{Response: "Usage: /forget <player>"}
	}
	if gameInstance != nil && gameInstance.net != nil {
		gameInstance.net.RemoveBlockIgnore(arg)
		return CommandResult{Response: fmt.Sprintf("Cleared labels on %s", arg)}
	}
	return CommandResult{Response: "Not connected"}
}

func cmdPref(arg string) CommandResult {
	if arg == "" || arg == "?" {
		return CommandResult{Response: "Usage: /pref <setting> [value]\nSettings: volume (0-100), mute (true/false)"}
	}
	parts := strings.SplitN(arg, " ", 2)
	sub := strings.ToLower(parts[0])
	val := ""
	if len(parts) > 1 {
		val = strings.TrimSpace(parts[1])
	}
	switch sub {
	case "volume", "soundvolume":
		if val == "" {
			return CommandResult{Response: fmt.Sprintf("Sound volume: %d", settings.SoundVol)}
		}
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 || n > 100 {
			return CommandResult{Response: "Volume must be 0-100"}
		}
		settings.SoundVol = n
		saveSettingsMobile()
		return CommandResult{Response: fmt.Sprintf("Sound volume set to %d", n)}
	case "musicvolume", "bardvolume":
		if val == "" {
			return CommandResult{Response: fmt.Sprintf("Music volume: %d", settings.MusicVol)}
		}
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 || n > 100 {
			return CommandResult{Response: "Volume must be 0-100"}
		}
		settings.MusicVol = n
		saveSettingsMobile()
		return CommandResult{Response: fmt.Sprintf("Music volume set to %d", n)}
	case "mute":
		if val == "" {
			return CommandResult{Response: fmt.Sprintf("Mute: %v", settings.Mute)}
		}
		switch strings.ToLower(val) {
		case "true", "on", "1", "yes":
			settings.Mute = true
		case "false", "off", "0", "no":
			settings.Mute = false
		default:
			return CommandResult{Response: "Use: /pref mute true|false"}
		}
		saveSettingsMobile()
		return CommandResult{Response: fmt.Sprintf("Mute set to %v", settings.Mute)}
	case "movement", "message", "brightcolors", "shownames", "autohide", "timestamps", "maxnightpercent", "newlog", "movielogs":
		return CommandResult{Response: fmt.Sprintf("/pref %s not available on mobile (use settings UI)", sub)}
	default:
		return CommandResult{Response: "Unknown setting. Use: volume, musicvolume, mute"}
	}
}

func cmdRoll(arg string) CommandResult {
	if arg == "" {
		arg = "1d6"
	}
	parts := strings.SplitN(strings.ToLower(arg), "d", 2)
	if len(parts) != 2 {
		return CommandResult{Response: "Usage: /roll NdM (e.g., /roll 2d6)"}
	}
	num, err1 := strconv.Atoi(parts[0])
	sides, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || num < 1 || sides < 1 || num > 100 {
		return CommandResult{Response: "Invalid dice format"}
	}
	total := 0
	rolls := make([]int, num)
	for i := 0; i < num; i++ {
		rolls[i] = int(time.Now().UnixNano()%int64(sides)) + 1
		total += rolls[i]
	}
	if num == 1 {
		return CommandResult{Response: fmt.Sprintf("Rolled 1d%d: %d", sides, total)}
	}
	return CommandResult{Response: fmt.Sprintf("Rolled %s: %d (%v)", arg, total, rolls)}
}

func cmdPlay(arg string) CommandResult {
	return CommandResult{Response: "/play not yet implemented"}
}

func cmdMacro(arg string) CommandResult {
	if arg == "" || strings.ToLower(arg) == "reload" {
		if gameInstance != nil && gameInstance.net != nil {
			name := gameInstance.net.playerName
			if name == "" {
				name = "Default"
			}
			LoadMacros(name)
			return CommandResult{Response: "Macros reloaded"}
		}
		return CommandResult{Response: "Not connected"}
	}
	return CommandResult{Response: "Usage: /macro reload"}
}

func cmdHelp(arg string) CommandResult {
	help := `Commands:
/select <name>     - Select a player
/selectitem <name> - Select an inventory item
/move <dir>        - Move (n/s/e/w/ne/nw/se/sw)
/label <name> [l]  - Label player (red/orange/green/blue/purple/none)
/wholabel [label]  - List labeled players
/block <name>      - Block a player
/ignore <name>     - Ignore a player
/forget <name>     - Clear block/ignore on player
/roll NdM          - Roll dice (e.g. /roll 2d6)
/pref <setting>    - Settings (volume/mute)
/macro reload      - Reload macro files
/time              - Show current time

Macros: "aa" action, "t" think, "w" who,
"yy" yell, "wh" whisper, "pp" ponder,
"mm" money, "nn" news, "sl" sleep,
"sh" share, "un" unshare, "uu" use,
"ui" useitem, "gg" give, "ii" info,
"kk" karma, "th" thank, "tt" thinkto

Server commands (sent directly):
/who /info /status /karma /money /news
/action /yell /whisper /speak /ponder /think
/equip /unequip /use /bag /drop /show /examine
/share /unshare /give /tip /pull /push
/pose /sleep /sky /pray
/bug /report /name /buy /sell`
	return CommandResult{Response: help}
}
