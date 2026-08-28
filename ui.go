package godwarf

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	text "github.com/hajimehoshi/ebiten/v2/text/v2"
)

type MobileUI struct {
	chatWidth    int
	chatScroll   int
	chatBuf      string
	chatActive   bool
	touchHandled bool

	disconnectMsg string
	disconnectAge int

	playersOpen   bool
	inventoryOpen bool
	invScroll     int
	playerScroll  int

	invMenuOpen   bool
	invMenuIdx    int    // index of tapped item in inventory
	invMenuName   string // name of tapped item
	invMenuID     uint16 // item ID for commands
	invMenuEquipped bool

	onEditLayout func()

	dragTouchID     ebiten.TouchID
	dragStartY      int
	dragStartScroll int
	dragPanel       int // 0=chat, 1=inventory, 2=players

	settingsOpen         bool
	settingsDragSlider   int // -1 = none, 0-3 = slider index
	settingsDragTouchID  ebiten.TouchID

	playerMenuName string // name of selected player for context menu
	playerMenuOpen bool

	// Player tap tracking (to distinguish tap from drag)
	playerTapID     ebiten.TouchID
	playerTapX      int
	playerTapY      int
	playerTapRow    int
	playerDragReady bool // true once we've started dragging (threshold exceeded)

	// Inventory item tap tracking
	invTapID     ebiten.TouchID
	invTapX      int
	invTapY      int
	invTapRow    int
	invDragReady bool

	// Chat swipe gesture tracking
	chatVisible     bool
	swipeTouchID    ebiten.TouchID
	swipeStartX     int
	swipeStartY     int
	swipeTriggered  bool
}

func NewMobileUI() *MobileUI {
	return &MobileUI{
		chatWidth:    240,
		dragTouchID:  -1,
		swipeTouchID: -1,
	}
}

const panelWidth = 240

func (u *MobileUI) contentLeft() int {
	if u.chatVisible {
		return u.chatWidth
	}
	return 0
}

func (u *MobileUI) panelWidth() int {
	return panelWidth
}

func (u *MobileUI) UpdateWithTouches(justPressed []ebiten.TouchID, touches []ebiten.TouchID, sw, sh int) {
	u.touchHandled = false

	// Chat swipe gesture: track any touch starting in left edge or chat area
	if u.swipeTouchID >= 0 {
		stillActive := false
		for _, id := range touches {
			if id == u.swipeTouchID {
				curX, _ := ebiten.TouchPosition(id)
				dx := curX - u.swipeStartX
				if dx > 50 {
					u.chatVisible = true
					u.swipeTouchID = -1
				} else if dx < -50 {
					u.chatVisible = false
					u.swipeTouchID = -1
				}
				stillActive = true
				break
			}
		}
		if !stillActive {
			u.swipeTouchID = -1
			u.swipeTriggered = false
		}
	}
	if u.swipeTouchID < 0 {
		for _, id := range justPressed {
			x, y := ebiten.TouchPosition(id)
			if u.chatVisible && x < u.chatWidth {
				// Don't capture the input bar area — let taps open the keyboard
				contentY := 30
				msgH := sh - 70
				inputY := contentY + msgH
				if y >= inputY {
					continue
				}
				u.swipeTouchID = id
				u.swipeStartX = x
				_, u.swipeStartY = ebiten.TouchPosition(id)
				u.swipeTriggered = false
			} else if !u.chatVisible && x < 50 {
				// Tap the left indicator bar to open chat
				if y >= sh/2-15 && y <= sh/2+15 {
					u.chatVisible = true
					continue
				}
				u.swipeTouchID = id
				u.swipeStartX = x
				_, u.swipeStartY = ebiten.TouchPosition(id)
				u.swipeTriggered = false
			}
		}
	}

	if u.disconnectAge > 0 {
		u.disconnectAge++
		if u.disconnectAge > 180 {
			u.disconnectMsg = ""
			u.disconnectAge = 0
		}
	}

	if gameInstance != nil && gameInstance.kbd != nil && (gameInstance.kbd.IsVisible() || gameInstance.kbd.consumedTouch) {
		return
	}

	// Settings overlay: intercept all touches
	if u.settingsOpen {
		// Drag slider with continuous touches
		if u.settingsDragSlider >= 0 {
			found := false
			for _, id := range touches {
				if id == u.settingsDragTouchID {
					found = true
					x, _ := ebiten.TouchPosition(id)
					u.updateSettingsSlider(x, sw)
					break
				}
			}
			if !found {
				u.settingsDragSlider = -1
				saveSettingsMobile()
			}
		}
		for _, id := range justPressed {
			x, y := ebiten.TouchPosition(id)
			u.handleSettingsTouch(x, y, sw, sh, id)
		}
		return
	}

	if gameInstance != nil && gameInstance.net != nil {
		if msg := gameInstance.net.GetDisconnectMsg(); msg != "" {
			u.disconnectMsg = msg
			u.disconnectAge = 1
		}
	}

	// Drag tracking: if a drag is active and the touch is still present, update scroll
	if u.dragTouchID >= 0 {
		stillDragging := false
		for _, id := range touches {
			if id == u.dragTouchID {
				stillDragging = true
				break
			}
		}
		if stillDragging {
			_, curY := ebiten.TouchPosition(u.dragTouchID)
			dy := curY - u.dragStartY
			switch u.dragPanel {
			case 0: // chat
				u.chatScroll = u.dragStartScroll + dy/16
				if u.chatScroll < 0 {
					u.chatScroll = 0
				}
			case 1: // inventory
				invIs := settings.InvScale
				if invIs <= 0 {
					invIs = 1
				}
				invLineH := int(40 * invIs)
				if invLineH < 20 {
					invLineH = 20
				}
				invContentY := 60
				invMaxVisible := (sh - invContentY) / invLineH
				if invMaxVisible < 1 {
					invMaxVisible = 1
				}
				items := gameInstance.net.GetInventory()
				u.invScroll = u.dragStartScroll - dy/invLineH
				if u.invScroll < 0 {
					u.invScroll = 0
				}
				if len(items) > 0 && u.invScroll > len(items)-invMaxVisible {
					u.invScroll = len(items) - invMaxVisible
				}
			case 2: // players
				u.playerScroll = u.dragStartScroll - dy/16
				if u.playerScroll < 0 {
					u.playerScroll = 0
				}
			}
		} else {
			u.dragTouchID = -1
		}
	}

	// Player tap tracking: detect tap vs drag for player list
	if u.playerTapID >= 0 {
		still := false
		for _, id := range touches {
			if id == u.playerTapID {
				still = true
				break
			}
		}
		if still {
			curX, curY := ebiten.TouchPosition(u.playerTapID)
			dx := curX - u.playerTapX
			dy := curY - u.playerTapY
			if dx < 0 {
				dx = -dx
			}
			if dy < 0 {
				dy = -dy
			}
			if !u.playerDragReady && (dx > 10 || dy > 10) {
				// Exceeded threshold: switch to drag scrolling
				u.playerDragReady = true
				u.dragTouchID = u.playerTapID
				u.dragStartY = u.playerTapY
				u.dragStartScroll = u.playerScroll
				u.dragPanel = 2
				u.playerTapID = -1
			}
		} else {
			// Touch ended without drag — it's a tap, open menu
			if !u.playerDragReady && u.playerTapRow >= 0 {
				players := gameInstance.net.GetSortedPlayers()
				if u.playerTapRow < len(players) {
					u.playerMenuName = players[u.playerTapRow].Name
					u.playerMenuOpen = true
				}
			}
			u.playerTapID = -1
			u.playerDragReady = false
		}
	}

	// Inventory item tap tracking: detect tap vs drag for inventory list
	if u.invTapID >= 0 {
		still := false
		for _, id := range touches {
			if id == u.invTapID {
				still = true
				break
			}
		}
		if still {
			curX, curY := ebiten.TouchPosition(u.invTapID)
			dx := curX - u.invTapX
			dy := curY - u.invTapY
			if dx < 0 {
				dx = -dx
			}
			if dy < 0 {
				dy = -dy
			}
			if !u.invDragReady && (dx > 10 || dy > 10) {
				// Exceeded threshold: switch to drag scrolling
				u.invDragReady = true
				u.dragTouchID = u.invTapID
				u.dragStartY = u.invTapY
				u.dragStartScroll = u.invScroll
				u.dragPanel = 1
				u.invTapID = -1
			}
		} else {
			// Touch ended without drag — it's a tap, open item menu
			if !u.invDragReady && u.invTapRow >= 0 {
				items := gameInstance.net.GetInventory()
				if u.invTapRow < len(items) {
					item := items[u.invTapRow]
					u.invMenuIdx = u.invTapRow
					u.invMenuName = item.name
					u.invMenuID = item.id
					u.invMenuEquipped = item.equipped
					u.invMenuOpen = true
				}
			}
			u.invTapID = -1
			u.invDragReady = false
		}
	}

	for _, id := range justPressed {
		x, y := ebiten.TouchPosition(id)

		// Disconnect button (top-left, above chat) - skip when keyboard is open
		if x < 80 && y < 30 && gameInstance != nil && gameInstance.net != nil && gameInstance.net.Connected() && (gameInstance.kbd == nil || !gameInstance.kbd.IsVisible()) {
			gameInstance.net.Disconnect()
			gameInstance.loginState.step = 0
			gameInstance.loginState.err = ""
			u.touchHandled = true
			return
		}

		// Players/Inventory tabs (top-right, shifted left)
		tabW := 60
		tabH := 20
		tabPad := 10
		tabRight := sw - tabPad
		if x >= tabRight-tabW && x < tabRight && y >= 0 && y < tabH {
			u.playersOpen = !u.playersOpen
			u.inventoryOpen = false
			u.invMenuOpen = false
			u.touchHandled = true
			return
		}
		if x >= tabRight-tabW*2 && x < tabRight-tabW && y >= 0 && y < tabH {
			u.inventoryOpen = !u.inventoryOpen
			u.playersOpen = false
			u.touchHandled = true
			return
		}
		// If a panel is open and tap outside, close it
		if u.playersOpen || u.inventoryOpen {
			// Check inventory menu taps first (menu may draw outside the panel)
			if u.inventoryOpen && u.invMenuOpen {
				if u.handleInvMenuTap(x, y) {
					u.touchHandled = true
					return
				}
				u.invMenuOpen = false
				u.touchHandled = true
				return
			}
			panelX := sw - u.panelWidth()
			if x < panelX {
				u.playersOpen = false
				u.inventoryOpen = false
				u.playerMenuOpen = false
				u.invMenuOpen = false
				u.touchHandled = true
				return
			}
			// Player panel: record tap (will distinguish tap vs drag on movement)
			if u.playersOpen && y >= 44 && x >= panelX {
				// If menu is open, check menu taps first
				if u.playerMenuOpen {
					if u.handlePlayerMenuTap(x, y, sw) {
						u.touchHandled = true
						return
					}
					// Tap outside menu closes it
					u.playerMenuOpen = false
					u.touchHandled = true
					return
				}
				// Record tap start — don't start drag yet
				u.playerTapID = id
				u.playerTapX = x
				u.playerTapY = y
				u.playerDragReady = false
				// Compute which row was tapped
				players := gameInstance.net.GetSortedPlayers()
				ps := settings.PlayerScale
				if ps <= 0 {
					ps = 1
				}
				lineH := int(32 * ps)
				if lineH < 16 {
					lineH = 16
				}
				spriteSz := int(32 * ps)
				if spriteSz < 16 {
					spriteSz = 16
				}
				maxVisible := (sh - 44) / lineH
				if maxVisible < 1 {
					maxVisible = 1
				}
				scrollTop := 44
				row := (y - scrollTop) / lineH
				idx := u.playerScroll + row
				if idx >= 0 && idx < len(players) {
					u.playerTapRow = idx
				} else {
					u.playerTapRow = -1
				}
				u.touchHandled = true
				return
			}
			// Inventory panel
			if u.inventoryOpen {
				// If item menu is open, check menu taps first
				if u.invMenuOpen {
					if u.handleInvMenuTap(x, y) {
						u.touchHandled = true
						return
					}
					// Tap outside menu closes it
					u.invMenuOpen = false
					u.touchHandled = true
					return
				}
				invIs2 := settings.InvScale
				if invIs2 <= 0 {
					invIs2 = 1
				}
				invLineH := int(40 * invIs2)
				if invLineH < 20 {
					invLineH = 20
				}
				invContentY := 60
				invMaxVisible := (sh - invContentY) / invLineH
				if invMaxVisible < 1 {
					invMaxVisible = 1
				}

				items := gameInstance.net.GetInventory()
				total := len(items)
				if total > 0 {
					if u.invScroll > total-invMaxVisible {
						u.invScroll = total - invMaxVisible
					}
					if u.invScroll < 0 {
						u.invScroll = 0
					}
				}

				// Tap on inventory content area — track tap vs drag
				if y >= invContentY && x >= panelX {
					u.invTapID = id
					u.invTapX = x
					u.invTapY = y
					u.invDragReady = false
					// Compute which row was tapped
					start := u.invScroll
					row := (y - invContentY) / invLineH
					idx := start + row
					if idx >= 0 && idx < len(items) {
						u.invTapRow = idx
					} else {
						u.invTapRow = -1
					}
					u.touchHandled = true
					return
				}
			}
		}

		// Gear icon (center bottom) — opens settings
		gearX := sw/2 - 30
		gearY := sh - 30
		if x >= gearX && x < gearX+60 && y >= gearY && y < gearY+24 {
			u.settingsOpen = true
			u.touchHandled = true
			return
		}

		// Chat panel
		if u.chatVisible && x < u.chatWidth {
			contentY := 30
			msgH := sh - 70

			// Skip drag for swipe gesture touch
			if id == u.swipeTouchID {
				u.touchHandled = true
				return
			}

			// Start drag on chat message area
			if y >= contentY && y < contentY+msgH {
				u.dragTouchID = id
				u.dragStartY = y
				u.dragStartScroll = u.chatScroll
				u.dragPanel = 0
				u.touchHandled = true
				return
			}

			// Chat input field tap (opens keyboard)
			inputY := contentY + msgH
			kbd := gameInstance.kbd
			if !kbd.IsVisible() && y >= inputY && y <= inputY+40 {
				u.chatBuf = ""
				u.chatActive = true
				kbd.Show(
					func(ch rune) { u.chatBuf += string(ch) },
					func() {
						if len(u.chatBuf) > 0 {
							u.chatBuf = u.chatBuf[:len(u.chatBuf)-1]
						}
					},
				func() {
					if u.chatBuf != "" {
						flog(fmt.Sprintf("chat submit: %q", u.chatBuf))
						macroResult, ok := HandleMacroInput(u.chatBuf)
						flog(fmt.Sprintf("HandleMacroInput returned: ok=%v result=%q", ok, macroResult))
						if ok {
							gameInstance.net.AddTextMessage("<you> "+u.chatBuf, colMySpeech)
							logTextMessage("<you> " + u.chatBuf)
							lines := strings.Split(strings.TrimSpace(macroResult), "\n")
							for _, line := range lines {
								line = strings.TrimSpace(line)
								if line != "" {
									gameInstance.net.EnqueueCommand(line)
									gameInstance.net.AddTextMessage(line, colDefault)
									logTextMessage(line)
								}
							}
						} else {
							result := handleClientCommand(u.chatBuf)
							gameInstance.net.AddTextMessage("<you> "+u.chatBuf, colDefault)
							logTextMessage("<you> " + u.chatBuf)
							if result.Response != "" {
								gameInstance.net.AddTextMessage(result.Response, colDefault)
								logTextMessage(result.Response)
							}
							if result.Sent && result.Text != "" {
								gameInstance.net.EnqueueCommand(result.Text)
							}
						}
					}
					u.chatBuf = ""
					u.chatActive = false
					kbd.Hide()
				},
				)
				u.touchHandled = true
				return
			}
		}
	}
}

func (u *MobileUI) Draw(screen *ebiten.Image) {
	sw, sh := screen.Bounds().Dx(), screen.Bounds().Dy()

	// Left chat panel
	if u.chatVisible {
		drawRect(screen, 0, 0, u.chatWidth, sh, color.RGBA{R: 0, G: 0, B: 0, A: 100})

		// Disconnect button (top-left)
		if gameInstance != nil && gameInstance.net != nil && gameInstance.net.Connected() {
			drawRect(screen, 2, 2, 76, 26, color.RGBA{R: 140, G: 40, B: 40, A: 160})
			ebitenutil.DebugPrintAt(screen, "Disconnect", 8, 8)

			// Online / sharing stats (right of disconnect button)
			online, _, shareMe, shareThem := gameInstance.net.SharingStats()
			statsText := fmt.Sprintf("%d online | sharing: %d | shared: %d", online, shareMe, shareThem)
			ebitenutil.DebugPrintAt(screen, statsText, 82, 8)
		}

		u.drawChat(screen, 0, 30, u.chatWidth, sh-30)
	} else {
		// Small swipe indicator when chat is hidden
		drawRect(screen, 0, sh/2-15, 8, 30, color.RGBA{R: 80, G: 80, B: 80, A: 160})
	}

	// When keyboard is visible, draw the typed text in a bar above the keyboard
	if gameInstance != nil && gameInstance.kbd != nil && gameInstance.kbd.IsVisible() && u.chatActive {
		kbdTop := sh - 5*44 - 2
		barH := 30
		barY := kbdTop - barH
		drawRect(screen, 0, barY, sw, barH, color.RGBA{R: 30, G: 30, B: 40, A: 220})
		display := u.chatBuf + "_"
		if len(display) > 60 {
			display = display[len(display)-60:]
		}
		textW := len(display) * 6
		ebitenutil.DebugPrintAt(screen, display, (sw-textW)/2, barY+10)
	}

	// Players/Inventory tabs (top-right, shifted left a bit)
	tabW := 60
	tabH := 20
	tabPad := 10
	tabRight := sw - tabPad
	drawRect(screen, tabRight-tabW, 0, tabW, tabH, color.RGBA{R: 50, G: 60, B: 80, A: 180})
	ebitenutil.DebugPrintAt(screen, "Players", tabRight-tabW+4, 6)
	drawRect(screen, tabRight-tabW*2, 0, tabW, tabH, color.RGBA{R: 50, G: 60, B: 80, A: 180})
	ebitenutil.DebugPrintAt(screen, "Items", tabRight-tabW*2+8, 6)

	// Panels
	if u.playersOpen || u.inventoryOpen {
		pw := u.panelWidth()
		panelX := sw - pw
		drawRect(screen, panelX, 22, pw, sh-22, color.RGBA{R: 0, G: 0, B: 0, A: 150})
		if u.playersOpen && gameInstance != nil && gameInstance.net != nil {
			players := gameInstance.net.GetSortedPlayers()
			shareWith := gameInstance.net.GetSharingWith()
			sharingYou := gameInstance.net.GetSharingYou()
			initChatFont()
			ps := settings.PlayerScale
			if ps <= 0 {
				ps = 1
			}
			ebitenutil.DebugPrintAt(screen, fmt.Sprintf("Players (%d)", len(players)), panelX+4, 28)

			// Clamp player scroll
			lineH := int(32 * ps)
			if lineH < 16 {
				lineH = 16
			}
			spriteSz := int(32 * ps)
			if spriteSz < 16 {
				spriteSz = 16
			}
			maxVisible := (sh - 44) / lineH
			if maxVisible < 1 {
				maxVisible = 1
			}
			if u.playerScroll > len(players)-maxVisible {
				u.playerScroll = len(players) - maxVisible
			}
			if u.playerScroll < 0 {
				u.playerScroll = 0
			}

			start := u.playerScroll
			end := start + maxVisible
			if end > len(players) {
				end = len(players)
			}

			for i := start; i < end; i++ {
				pi := players[i]
				ly := 44 + (i-start)*lineH
				if ly+lineH > sh {
					break
				}
				if gameInstance.clImages != nil {
					pID := pi.PictID
					if pID == 0 {
						pID = 22 // kNewbieRacelessPlayerPict fallback
					}
					spr := loadMobileSprite(gameInstance.clImages, pID, pi.State, pi.Colors)
					if spr != nil {
						op := &ebiten.DrawImageOptions{}
						sx0 := float64(spriteSz) / float64(spr.Bounds().Dx())
						sy0 := float64(spriteSz) / float64(spr.Bounds().Dy())
						op.GeoM.Scale(sx0, sy0)
						op.GeoM.Translate(float64(panelX+4), float64(ly+4))
						screen.DrawImage(spr, op)
					}
				}
				textX := panelX + spriteSz + 8
				textY := ly + (spriteSz-int(metricsHeight(ps)))/2
				if textY < ly {
					textY = ly
				}
				drawPlayerName(screen, pi.Name, textX, textY, color.RGBA{R: 255, G: 255, B: 255, A: 255}, ps, shareWith, sharingYou)
			}
			u.drawPlayerMenu(screen)
		}
		if u.inventoryOpen && gameInstance != nil && gameInstance.net != nil {
			gameInstance.net.EnrichInventory(gameInstance.clImages)
			items := gameInstance.net.GetInventory()
			initChatFont()
			is := settings.InvScale
			if is <= 0 {
				is = 1
			}

			// Count slots: each stacked group = quantity slots
			totalSlots := 0
			for _, item := range items {
				totalSlots += item.quantity
			}
			freeSlots := inventoryMaxSlots - totalSlots
			title := fmt.Sprintf("Inv %d/%d", totalSlots, inventoryMaxSlots)
			if freeSlots <= 5 {
				title += fmt.Sprintf(" (%d free)", freeSlots)
			}
			ebitenutil.DebugPrintAt(screen, title, panelX+4, 28)

			invLineH := int(40 * is)
			if invLineH < 20 {
				invLineH = 20
			}
			spriteSz := int(32 * is)
			if spriteSz < 16 {
				spriteSz = 16
			}
			invContentY := 44
			invMaxVisible := (sh - invContentY) / invLineH
			if invMaxVisible < 1 {
				invMaxVisible = 1
			}

			total := len(items)
			if u.invScroll > total-invMaxVisible {
				u.invScroll = total - invMaxVisible
			}
			if u.invScroll < 0 {
				u.invScroll = 0
			}

			start := u.invScroll
			end := start + invMaxVisible
			if end > total {
				end = total
			}

			for i := start; i < end; i++ {
				item := items[i]
				ny := invContentY + (i-start)*invLineH
				if ny+invLineH > sh {
					break
				}

				// Icon
				if gameInstance.clImages != nil && item.pictID != 0 {
					spr := gameInstance.clImages.Get(uint32(item.pictID), nil, false)
					if spr != nil {
						op := &ebiten.DrawImageOptions{}
						sx0 := float64(spriteSz) / float64(spr.Bounds().Dx())
						sy0 := float64(spriteSz) / float64(spr.Bounds().Dy())
						op.GeoM.Scale(sx0, sy0)
						op.GeoM.Translate(float64(panelX+2), float64(ny+4))
						screen.DrawImage(spr, op)
					}
				}

				// Name with equipped tag and quantity
				displayName := item.name
				if item.equipped {
					if item.slot >= kItemSlotFirstReal && item.slot <= kItemSlotLastReal {
						tag := strings.Title(slotNames[item.slot])
						displayName += " [" + tag + "]"
					} else if item.slot == kItemSlotNotWearable {
						displayName += " [held]"
					}
				}
				if item.quantity > 1 {
					displayName += fmt.Sprintf(" (%d)", item.quantity)
				}

				// Truncate to fit panel (scaled char width ~ 6*is)
				maxChars := int(float64(u.panelWidth()-spriteSz-8) / (6 * is))
				if maxChars < 1 {
					maxChars = 1
				}
				if len(displayName) > maxChars {
					displayName = displayName[:maxChars-1] + "~"
				}

				textX := panelX + spriteSz + 6
				textY := ny + (spriteSz-int(metricsHeight(is)))/2
				if textY < ny {
					textY = ny
				}
				if item.equipped {
					drawScaledText(screen, "*"+displayName, textX, textY, color.RGBA{R: 255, G: 255, B: 255, A: 255}, is)
				} else {
					drawScaledText(screen, " "+displayName, textX, textY, color.RGBA{R: 255, G: 255, B: 255, A: 255}, is)
				}
			}
		}
		u.drawInvMenu(screen)
	}

	// Disconnect notification
	if u.disconnectMsg != "" {
		bg := color.RGBA{R: 120, G: 30, B: 30, A: 220}
		drawRect(screen, sw/2-100, sh/2-14, 200, 28, bg)
		ebitenutil.DebugPrintAt(screen, u.disconnectMsg, sw/2-len(u.disconnectMsg)*3, sh/2-4)
	}

	// Gear icon (center bottom)
	gearX := sw/2 - 30
	gearY := sh - 30
	drawRect(screen, gearX, gearY, 60, 24, color.RGBA{R: 50, G: 50, B: 60, A: 160})
	ebitenutil.DebugPrintAt(screen, "Settings", gearX+6, gearY+6)

	// Settings overlay (drawn last, on top of everything)
	u.drawSettings(screen)
}

func (u *MobileUI) drawChat(screen *ebiten.Image, x, y, w, h int) {
	initChatFont()
	cs := settings.ChatScale
	if cs <= 0 {
		cs = 1
	}
	msgH := h - 40
	drawRect(screen, x, y, w, msgH, color.RGBA{R: 0, G: 0, B: 0, A: 60})

	var msgs []ChatMessage
	if gameInstance != nil && gameInstance.net != nil {
		msgs = gameInstance.net.GetTextMessages()
	}

	metrics := chatFace.Metrics()
	baseLineH := int(metrics.HAscent + metrics.HDescent + 2)
	if baseLineH < 14 {
		baseLineH = 14
	}
	lineH := int(float64(baseLineH) * cs)
	if lineH < 1 {
		lineH = 1
	}
	maxLines := msgH / lineH
	if maxLines < 1 {
		maxLines = 1
	}

	maxWidth := float64(w-8) / cs

	// Word-wrap messages into display lines with color
	type styledLine struct {
		text  string
		color color.RGBA
	}
	var wrappedLines []styledLine
	for _, msg := range msgs {
		words := strings.Fields(msg.Text)
		if len(words) == 0 {
			wrappedLines = append(wrappedLines, styledLine{text: "", color: msg.Color})
			continue
		}
		curLine := words[0]
		for _, word := range words[1:] {
			test := curLine + " " + word
			tw, _ := text.Measure(test, chatFace, 0)
			if tw > maxWidth {
				wrappedLines = append(wrappedLines, styledLine{text: curLine, color: msg.Color})
				curLine = word
			} else {
				curLine = test
			}
		}
		wrappedLines = append(wrappedLines, styledLine{text: curLine, color: msg.Color})
	}

	totalLines := len(wrappedLines)
	start := totalLines - maxLines - u.chatScroll
	if start < 0 {
		start = 0
	}
	end := start + maxLines
	if end > totalLines {
		end = totalLines
	}

	for i := start; i < end; i++ {
		ly := y + (i-start)*lineH + lineH
		if ly > y+msgH {
			break
		}
		sl := wrappedLines[i]
		if sl.text == "" {
			continue
		}
		op := &text.DrawOptions{}
		op.GeoM.Scale(cs, cs)
		op.GeoM.Translate(float64(x+4), float64(ly)-metrics.HDescent*cs)
		op.ColorScale.ScaleWithColor(sl.color)
		text.Draw(screen, sl.text, chatFace, op)
	}

	// Scroll position indicator
	if totalLines > maxLines {
		scrollPos := totalLines - maxLines - u.chatScroll
		scrollInfo := fmt.Sprintf("%d/%d", scrollPos, totalLines-maxLines)
		ebitenutil.DebugPrintAt(screen, scrollInfo, x+w-40, y+4)
	}

	inputY := y + msgH
	drawRect(screen, x, inputY, w, 40, color.RGBA{R: 30, G: 30, B: 40, A: 180})
	if u.chatActive {
		display := u.chatBuf + "_"
		maxChars := (w - 8) / 6
		if maxChars > 0 && len(display) > maxChars {
			display = display[len(display)-maxChars:]
		}
		ebitenutil.DebugPrintAt(screen, display, x+4, inputY+14)
	} else {
		ebitenutil.DebugPrintAt(screen, "[Tap to type]", x+4, inputY+28)
	}
}

// Settings overlay layout constants
const (
	settPad      = 16
	settLineH    = 32
	settSliderH  = 10
	settSliderW  = 180
	settValueGap = 8
	settValueW   = 40
)

func settBoxW() int {
	return settPad + settSliderW + settValueGap + settValueW + settPad
}

type settSlider struct {
	label  string
	min    float64
	max    float64
	valF   *float64
	valI   *intSlider
}

func (s *settSlider) get() float64 {
	if s.valF != nil {
		return *s.valF
	}
	return s.valI.get()
}

func (s *settSlider) set(f float64) {
	if s.valF != nil {
		*s.valF = f
	} else {
		s.valI.set(f)
	}
}

// sliderVal wrappers so we can take pointers to int fields as float64
type intSlider struct{ v *int }

func (s *intSlider) get() float64 { return float64(*s.v) }
func (s *intSlider) set(f float64) { *s.v = int(f) }

func (u *MobileUI) settingsSliders() []settSlider {
	return []settSlider{
		{"Joystick Speed", 0.5, 3.0, &settings.JoySpeed, nil},
		{"Dead Zone", 0.0, 0.5, &settings.JoyDeadZone, nil},
		{"Game Sounds", 0.0, 100.0, nil, &intSlider{&settings.SoundVol}},
		{"Music", 0.0, 100.0, nil, &intSlider{&settings.MusicVol}},
		{"Chat Text", 0.5, 3.0, &settings.ChatScale, nil},
		{"Player List", 0.5, 3.0, &settings.PlayerScale, nil},
		{"Inventory", 0.5, 3.0, &settings.InvScale, nil},
		{"Status Bars", 0.5, 3.0, &settings.StatBarScale, nil},
	}
}

func (u *MobileUI) handleSettingsTouch(x, y, sw, sh int, touchID ebiten.TouchID) {
	sliders := u.settingsSliders()
	boxW := settBoxW()
	boxH := 90 + len(sliders)*settLineH + 60
	boxX := (sw - boxW) / 2
	boxY := (sh - boxH) / 2

	// Slider tracks
	for i := range sliders {
		sy := boxY + 70 + i*settLineH
		sx := boxX + settPad
		// Handle drag start
		if x >= sx && x <= sx+settSliderW && y >= sy-8 && y <= sy+settSliderH+8 {
			u.settingsDragSlider = i
			u.settingsDragTouchID = touchID
			u.updateSettingsSlider(x, sw)
			return
		}
	}

	// Mute toggle
	muteY := boxY + 70 + len(sliders)*settLineH + 4
	if x >= boxX+settPad && x <= boxX+settPad+80 && y >= muteY && y <= muteY+20 {
		settings.Mute = !settings.Mute
		saveSettingsMobile()
		return
	}

	// Edit Layout button
	editY := muteY + 28
	if x >= boxX+settPad && x <= boxX+settPad+100 && y >= editY && y <= editY+24 {
		u.settingsOpen = false
		if u.onEditLayout != nil {
			u.onEditLayout()
		}
		return
	}

	// Close button
	closeY := editY + 30
	if x >= boxX+boxW-settPad-60 && x <= boxX+boxW-settPad && y >= closeY && y <= closeY+24 {
		u.settingsOpen = false
		saveSettingsMobile()
		return
	}
}

func (u *MobileUI) updateSettingsSlider(x, sw int) {
	if u.settingsDragSlider < 0 {
		return
	}
	sliders := u.settingsSliders()
	if u.settingsDragSlider >= len(sliders) {
		u.settingsDragSlider = -1
		return
	}
	boxW := settBoxW()
	boxX := (sw - boxW) / 2
	sx := boxX + settPad

	t := float64(x-sx) / float64(settSliderW)
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	s := sliders[u.settingsDragSlider]
	s.set(s.min + t*(s.max-s.min))
}

func (u *MobileUI) drawSettings(screen *ebiten.Image) {
	if !u.settingsOpen {
		return
	}
	sw, sh := screen.Bounds().Dx(), screen.Bounds().Dy()

	// Dim background
	drawRect(screen, 0, 0, sw, sh, color.RGBA{R: 0, G: 0, B: 0, A: 180})

	sliders := u.settingsSliders()

	boxW := settBoxW()
	boxH := 90 + len(sliders)*settLineH + 70
	boxX := (sw - boxW) / 2
	boxY := (sh - boxH) / 2

	// Panel background
	drawRect(screen, boxX, boxY, boxW, boxH, color.RGBA{R: 20, G: 20, B: 30, A: 240})
	drawRect(screen, boxX, boxY, boxW, 28, color.RGBA{R: 40, G: 50, B: 70, A: 240})
	ebitenutil.DebugPrintAt(screen, "Settings", boxX+settPad, boxY+8)

	for i, s := range sliders {
		sy := boxY + 70 + i*settLineH
		sx := boxX + settPad

		ebitenutil.DebugPrintAt(screen, s.label, sx, sy-14)

		// Value text
		var valStr string
		if s.max <= 1.0 {
			valStr = fmt.Sprintf("%.2f", s.get())
		} else {
			valStr = fmt.Sprintf("%d", int(s.get()))
		}
		ebitenutil.DebugPrintAt(screen, valStr, sx+settSliderW+8, sy)

		// Track
		drawRect(screen, sx, sy+2, settSliderW, settSliderH, color.RGBA{R: 60, G: 60, B: 80, A: 200})

		// Fill
		t := (s.get() - s.min) / (s.max - s.min)
		fillW := int(float64(settSliderW) * t)
		if fillW > 0 {
			drawRect(screen, sx, sy+2, fillW, settSliderH, color.RGBA{R: 80, G: 140, B: 200, A: 220})
		}

		// Handle
		hx := sx + fillW - 4
		drawRect(screen, hx, sy-2, 8, settSliderH+8, color.RGBA{R: 200, G: 200, B: 220, A: 240})
	}

	muteY := boxY + 70 + len(sliders)*settLineH + 4
	muteLabel := "[ ] Mute"
	if settings.Mute {
		muteLabel = "[x] Mute"
	}
	ebitenutil.DebugPrintAt(screen, muteLabel, boxX+settPad, muteY+4)

	editY := muteY + 28
	drawRect(screen, boxX+settPad, editY, 100, 24, color.RGBA{R: 50, G: 80, B: 50, A: 200})
	ebitenutil.DebugPrintAt(screen, "Edit Layout", boxX+settPad+4, editY+6)

	closeY := editY + 30
	drawRect(screen, boxX+boxW-settPad-60, closeY, 60, 24, color.RGBA{R: 100, G: 40, B: 40, A: 200})
	ebitenutil.DebugPrintAt(screen, "Close", boxX+boxW-settPad-52, closeY+6)

	// Storage warning
	if !storageOK() {
		warnY := closeY + 30
		drawRect(screen, boxX, warnY, boxW, 48, color.RGBA{R: 80, G: 40, B: 20, A: 220})
		ebitenutil.DebugPrintAt(screen, "Macros/logs need file access.", boxX+settPad, warnY+6)
		ebitenutil.DebugPrintAt(screen, "Grant 'All files access' in:", boxX+settPad, warnY+16)
		ebitenutil.DebugPrintAt(screen, "Settings > Apps > goDwarf", boxX+settPad, warnY+26)
		ebitenutil.DebugPrintAt(screen, "  > Special access > All files", boxX+settPad, warnY+36)
	}
}

func drawRect(screen *ebiten.Image, x, y, w, h int, c color.RGBA) {
	ebitenutil.DrawRect(screen, float64(x), float64(y), float64(w), float64(h), c)
}

// metricsHeight returns the chatFace line height at the given scale.
func metricsHeight(scale float64) float64 {
	initChatFont()
	m := chatFace.Metrics()
	h := m.HAscent + m.HDescent + 2
	if h < 14 {
		h = 14
	}
	return h * scale
}

// drawScaledText draws text using chatFace scaled by s. Falls back to
// DebugPrintAt when s ≈ 1 (no scaling needed, faster).
func drawScaledText(screen *ebiten.Image, s string, x, y int, clr color.RGBA, scale float64) {
	if scale <= 0 {
		scale = 1
	}
	op := &text.DrawOptions{}
	op.GeoM.Scale(scale, scale)
	op.GeoM.Translate(float64(x), float64(y))
	op.ColorScale.ScaleWithColor(clr)
	text.Draw(screen, s, chatFace, op)
}

// drawPlayerName draws a player name with bold/italic based on sharing status.
func drawPlayerName(screen *ebiten.Image, name string, x, y int, clr color.RGBA, scale float64, shareWith, sharingYou map[string]bool) {
	initChatFont()
	low := strings.ToLower(name)
	face := chatFace
	isShareWith := shareWith[low]
	isSharingYou := sharingYou[low]
	switch {
	case isShareWith && isSharingYou:
		face = chatFaceBoldItalic
	case isShareWith:
		face = chatFaceItalic
	case isSharingYou:
		face = chatFaceBold
	}
	op := &text.DrawOptions{}
	if scale <= 0 {
		scale = 1
	}
	op.GeoM.Scale(scale, scale)
	op.GeoM.Translate(float64(x), float64(y))
	op.ColorScale.ScaleWithColor(clr)
	text.Draw(screen, name, face, op)
}

// Player context menu
type playerMenuItem struct {
	label string
	cmd   string // command template, %s = player name
}

var playerMenuItems = []playerMenuItem{
	{"Share", "/share %s"},
	{"Unshare", "/unshare %s"},
	{"Info", "/info %s"},
	{"Thank", "/thank %s"},
	{"Curse", "/curse %s"},
}

// Inventory item context menu
type invMenuItem struct {
	label string
	cmd   string // command template: %d = item ID, %s = item name
}

var invMenuItems = []invMenuItem{
	{"Equip", "/equip %d"},
	{"Unequip", "/unequip %d"},
	{"Drop", "/drop %s"},
	{"Mine", "/mine %s"},
	{"Examine", "/examine %s"},
}

// handlePlayerMenuTap returns true if the tap was handled by the menu.
func (u *MobileUI) handlePlayerMenuTap(x, y, sw int) bool {
	if !u.playerMenuOpen || u.playerMenuName == "" {
		return false
	}
	ps := settings.PlayerScale
	if ps <= 0 {
		ps = 1
	}
	// Menu is drawn centered within the right-side panel
	menuW := int(160 * ps)
	itemH := int(28 * ps)
	itemGap := int(30 * ps)
	titleH := int(20 * ps)
	panelX := sw - u.chatWidth
	menuX := panelX + (u.chatWidth-menuW)/2
	menuY := 60

	for i, item := range playerMenuItems {
		bx := menuX
		by := menuY + titleH + i*itemGap
		if x >= bx && x < bx+menuW && y >= by && y < by+itemH {
			cmd := fmt.Sprintf(item.cmd, u.playerMenuName)
			if gameInstance != nil && gameInstance.net != nil && gameInstance.net.Connected() {
				gameInstance.net.EnqueueCommand(cmd)
			}
			u.playerMenuOpen = false
			return true
		}
	}
	return false
}

// drawPlayerMenu draws the player context menu overlay.
func (u *MobileUI) drawPlayerMenu(screen *ebiten.Image) {
	if !u.playerMenuOpen || u.playerMenuName == "" {
		return
	}
	sw := screen.Bounds().Dx()
	ps := settings.PlayerScale
	if ps <= 0 {
		ps = 1
	}
	menuW := int(160 * ps)
	itemH := int(28 * ps)
	itemGap := int(30 * ps)
	titleH := int(20 * ps)
	menuH := len(playerMenuItems)*itemGap + titleH + 4
	panelX := sw - u.chatWidth
	menuX := panelX + (u.chatWidth-menuW)/2
	menuY := 60

	// Background
	drawRect(screen, menuX-2, menuY-2, menuW+4, menuH+4, color.RGBA{R: 40, G: 40, B: 50, A: 230})

	// Title
	drawRect(screen, menuX, menuY, menuW, titleH, color.RGBA{R: 60, G: 80, B: 120, A: 255})
	drawScaledText(screen, u.playerMenuName, menuX+int(4*ps), menuY+int(4*ps), color.RGBA{R: 255, G: 255, B: 255, A: 255}, ps)

	// Buttons
	for i, item := range playerMenuItems {
		bx := menuX
		by := menuY + titleH + i*itemGap
		kc := color.RGBA{R: 50, G: 55, B: 70, A: 255}
		drawRect(screen, bx, by, menuW, itemH, kc)
		drawScaledText(screen, item.label, bx+int(8*ps), by+int(8*ps), color.RGBA{R: 255, G: 255, B: 255, A: 255}, ps)
	}
}

// handleInvMenuTap returns true if the tap was handled by the item context menu.
func (u *MobileUI) handleInvMenuTap(x, y int) bool {
	if !u.invMenuOpen {
		return false
	}
	is := settings.InvScale
	if is <= 0 {
		is = 1
	}
	menuW := int(160 * is)
	itemH := int(28 * is)
	itemGap := int(30 * is)
	titleH := int(20 * is)
	menuX := u.contentLeft()
	menuY := 60

	for i, item := range invMenuItems {
		bx := menuX
		by := menuY + titleH + i*itemGap
		if x >= bx && x < bx+menuW && y >= by && y < by+itemH {
			var cmd string
			if strings.Contains(item.cmd, "%d") {
				cmd = fmt.Sprintf(item.cmd, u.invMenuID)
			} else {
				cmd = fmt.Sprintf(item.cmd, u.invMenuName)
			}
			if gameInstance != nil && gameInstance.net != nil && gameInstance.net.Connected() {
				gameInstance.net.EnqueueCommand(cmd)
			}
			u.invMenuOpen = false
			return true
		}
	}
	// Tap outside closes menu
	u.invMenuOpen = false
	return true
}

// drawInvMenu draws the inventory item context menu overlay.
func (u *MobileUI) drawInvMenu(screen *ebiten.Image) {
	if !u.invMenuOpen {
		return
	}
	is := settings.InvScale
	if is <= 0 {
		is = 1
	}
	menuW := int(160 * is)
	itemH := int(28 * is)
	itemGap := int(30 * is)
	titleH := int(20 * is)
	menuH := len(invMenuItems)*itemGap + titleH + 4
	menuX := u.contentLeft()
	menuY := 60

	// Background
	drawRect(screen, menuX-2, menuY-2, menuW+4, menuH+4, color.RGBA{R: 40, G: 40, B: 50, A: 230})

	// Title
	drawRect(screen, menuX, menuY, menuW, titleH, color.RGBA{R: 60, G: 80, B: 120, A: 255})
	title := u.invMenuName
	if len(title) > 18 {
		title = title[:17] + "~"
	}
	drawScaledText(screen, title, menuX+int(4*is), menuY+int(4*is), color.RGBA{R: 255, G: 255, B: 255, A: 255}, is)

	// Buttons
	for i, item := range invMenuItems {
		bx := menuX
		by := menuY + titleH + i*itemGap
		kc := color.RGBA{R: 50, G: 55, B: 70, A: 255}
		// Gray out equip/unequip depending on state
		if (i == 0 && u.invMenuEquipped) || (i == 1 && !u.invMenuEquipped) {
			kc = color.RGBA{R: 35, G: 35, B: 40, A: 200}
		}
		drawRect(screen, bx, by, menuW, itemH, kc)
		drawScaledText(screen, item.label, bx+int(8*is), by+int(8*is), color.RGBA{R: 255, G: 255, B: 255, A: 255}, is)
	}
}
