package godwarf

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

type LoginState struct {
	net              *Network
	step             int
	name             string
	pass             string
	selected         int // 0=name, 1=pass
	err              string
	onConnectAttempt func()
}

func NewLoginState(net *Network) *LoginState {
	return &LoginState{
		net: net,
	}
}

const loginServer = "server.deltatao.com:5010"

func (l *LoginState) Draw(screen *ebiten.Image, sw, sh int) {
	cx := sw / 2
	scale := float64(sw) / 886.0
	if scale < 1 {
		scale = 1
	}

	title := "goDwarf - GoThoom Mobile"
	titleY := int(float64(20) * scale)
	ebitenutil.DebugPrintAt(screen, title, cx-len(title)*3, titleY)

	fields := []struct {
		label string
		value string
		y     int
	}{
		{"Name: ", l.name, int(float64(80) * scale)},
		{"Pass: ", maskPass(l.pass), int(float64(130) * scale)},
	}

	fieldH := int(float64(28) * scale)
	for i, f := range fields {
		selected := i == l.selected
		bg := color.RGBA{R: 0, G: 0, B: 0, A: 0}
		if selected {
			bg = color.RGBA{R: 40, G: 60, B: 120, A: 180}
		}
		if bg.A > 0 {
			drawRect(screen, cx-120, f.y-4, 240, fieldH, bg)
		}
		text := f.label + f.value
		ebitenutil.DebugPrintAt(screen, text, cx-110, f.y+4)
		if selected {
			drawRect(screen, cx-110+len(text)*6, f.y+16, 20, 3, color.RGBA{R: 80, G: 160, B: 255, A: 255})
		}
	}

	tapY := int(float64(190) * scale)
	ebitenutil.DebugPrintAt(screen, "Tap name/pass to edit", cx-80, tapY)

	btnW := 120
	btnH := int(float64(36) * scale)
	btnX := cx - btnW/2
	btnY := int(float64(230) * scale)
	drawRect(screen, btnX, btnY, btnW, btnH, color.RGBA{R: 40, G: 80, B: 40, A: 200})
	ebitenutil.DebugPrintAt(screen, "Connect", cx-24, btnY+12)

	if l.step == 3 {
		ebitenutil.DebugPrintAt(screen, "Connecting...", cx-40, btnY+btnH+int(12*scale))
	}
	if l.err != "" {
		errY := btnY + btnH + int(30*scale)
		ebitenutil.DebugPrintAt(screen, "Error: "+l.err, cx-100, errY)
	}

	// Saved characters (right side)
	chars := getCharacters()
	if len(chars) > 0 {
		listX := sw/2 + int(float64(200)*scale)
		listY := int(float64(80) * scale)
		title := "-- tap to select --"
		ebitenutil.DebugPrintAt(screen, title, listX, listY)
		for i, c := range chars {
			cy := listY + int(20*scale) + i*int(70*scale)
			if cy+int(50*scale) > sh {
				break
			}
			if gameInstance != nil && gameInstance.clImages != nil && c.PictID != 0 {
				img := loadMobileSprite(gameInstance.clImages, c.PictID, 0, c.Colors)
				if img != nil {
					op := &ebiten.DrawImageOptions{}
					h := img.Bounds().Dy()
					sprScale := 3.0 * scale
					op.GeoM.Scale(sprScale, sprScale)
					op.GeoM.Translate(float64(listX), float64(cy))
					screen.DrawImage(img, op)
					ebitenutil.DebugPrintAt(screen, c.Name, listX, cy+int(float64(h)*sprScale)+4)
					continue
				}
			}
			ebitenutil.DebugPrintAt(screen, c.Name, listX, cy+int(10*scale))
		}
	}
}

func (l *LoginState) HandleTouch(x, y int) {
}

func (l *LoginState) tryConnect() {
	if l.name == "" || l.pass == "" {
		return
	}
	l.step = 3
	l.err = ""
	l.onConnectAttempt()
	flog(fmt.Sprintf("tryConnect: name=%s pass=%s", l.name, maskPass(l.pass)))
	go func() {
		clVer := 0
		if gameInstance != nil {
			clVer = gameInstance.clVersion
		}
		err := l.net.Connect(loginServer, l.name, l.pass, clVer)
		if err != nil {
			l.err = fmt.Sprintf("%v", err)
			l.step = 0
		} else {
			// Preserve existing appearance data — don't overwrite with 0/nil
			charactersMu.Lock()
			for i := range characters {
				if strings.EqualFold(characters[i].Name, l.name) {
					characters[i].Key = scrambleStr(l.name, l.pass)
					saveCharactersFile()
					charactersMu.Unlock()
					return
				}
			}
			charactersMu.Unlock()
			saveCharacter(l.name, l.pass, 0, nil)
		}
	}()
}

// HandleEnter processes the Enter key based on which field is selected.
func (l *LoginState) HandleEnter() {
	if l.selected == 0 {
		// In username field → move to password
		l.selected = 1
	} else {
		// In password field → try to connect
		l.tryConnect()
	}
}

func (l *LoginState) FeedChar(ch rune) {
	switch l.selected {
	case 0:
		l.name += string(ch)
	case 1:
		l.pass += string(ch)
	}
}

func (l *LoginState) Backspace() {
	switch l.selected {
	case 0:
		if len(l.name) > 0 {
			l.name = l.name[:len(l.name)-1]
		}
	case 1:
		if len(l.pass) > 0 {
			l.pass = l.pass[:len(l.pass)-1]
		}
	}
}

func (l *LoginState) SelectField(idx int) {
	l.selected = idx
}

func (l *LoginState) HandleCharacterTap(x, y, sw int) {
	chars := getCharacters()
	if len(chars) == 0 {
		return
	}
	scale := float64(sw) / 886.0
	if scale < 1 {
		scale = 1
	}
	listX := sw/2 + int(float64(200)*scale)
	listY := int(float64(80) * scale) + int(20*scale)
	itemW := int(float64(200) * scale)
	for _, c := range chars {
		cy := listY
		itemH := int(70 * scale)
		if x >= listX && x <= listX+itemW && y >= cy && y <= cy+itemH {
			l.name = c.Name
			l.pass = getCharacterPass(c.Name)
			l.selected = 1
			return
		}
		listY += itemH
	}
}
