package godwarf

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	text "github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

func drawCircleOutline(screen *ebiten.Image, cx, cy, r float64, col color.RGBA, strokeW float32) {
	var p vector.Path
	p.MoveTo(float32(cx)+float32(r), float32(cy))
	p.Arc(float32(cx), float32(cy), float32(r), 0, 2*math.Pi, vector.Clockwise)
	p.Close()
	strokeOp := &vector.StrokeOptions{Width: strokeW}
	drawOp := &vector.DrawPathOptions{AntiAlias: true}
	drawOp.ColorScale.ScaleWithColor(col)
	vector.StrokePath(screen, &p, strokeOp, drawOp)
}

type ControlTouchResult struct {
	Element  *ControlElement
	Touched  bool
	Joystick *JoystickTouch
}

type JoystickTouch struct {
	DX, DY float32
}

type ControlView struct {
	state          *ControlsState
	sw, sh         int
	onDone         func()
	activeTouches  map[ebiten.TouchID]int
	dragElement    int
	dragStartX     int
	dragStartY     int
	dragOrigX      float64
	dragOrigY      float64
	editing        bool
	scrollY        int
	selectedIdx    int
	configOpen     bool
	configField    int
	editCursor     int
	editBuf        string
	deleteConfirm  bool
	addMenu        bool
	dragTarget     string // "joystick" or "statbar"
	dragStartOrigX int
	dragStartOrigY int

	playerMenuName string
	playerMenuOpen bool
}

func NewControlView(state *ControlsState) *ControlView {
	return &ControlView{
		state:         state,
		activeTouches: make(map[ebiten.TouchID]int),
	}
}

func (cv *ControlView) SetScreen(w, h int) {
	cv.sw = w
	cv.sh = h
}

func (cv *ControlView) Editing() bool {
	return cv.editing
}

func (cv *ControlView) SetEditing(e bool) {
	cv.editing = e
	cv.configOpen = false
	cv.addMenu = false
}

func (cv *ControlView) UpdateWithTouches(justPressed []ebiten.TouchID) {
	if gameInstance != nil && gameInstance.kbd != nil && gameInstance.kbd.IsVisible() {
		// Keyboard is up: taps belong to it, never to config rows or element
		// drags behind it.
		cv.dragTarget = ""
		cv.dragElement = -1
		return
	}
	if !cv.editing {
		cv.dragTarget = ""
		return
	}
	for _, id := range justPressed {
		x, y := ebiten.TouchPosition(id)

		if cv.addMenu {
			cv.handleAddMenu(x, y)
			return
		}
		if cv.configOpen {
			cv.handleConfig(x, y)
			return
		}

		if cv.HandleToolbarTouch(x, y) {
			return
		}

		// Check joystick drag
		if gameInstance != nil && gameInstance.controls != nil {
			p := gameInstance.controls.Profile()
			if p != nil {
				jx := int(p.JoystickX * float64(cv.sw))
				jy := int(p.JoystickY * float64(cv.sh))
				if (x-jx)*(x-jx)+(y-jy)*(y-jy) < 80*80 {
					cv.dragTarget = "joystick"
					cv.dragStartX = x
					cv.dragStartY = y
					cv.dragStartOrigX = jx
					cv.dragStartOrigY = jy
					return
				}
				// Check stat bar drag
				sx, sy := p.StatBarX, p.StatBarY
				if x >= sx && x < sx+60 && y >= sy-4 && y < sy+18 {
					cv.dragTarget = "statbar"
					cv.dragStartX = x
					cv.dragStartY = y
					cv.dragStartOrigX = sx
					cv.dragStartOrigY = sy
					return
				}
			}
		}

		profile := cv.state.Profile()
		if profile == nil {
			return
		}

		idx := cv.hitTest(x, y, profile)
		if idx >= 0 {
			cv.selectedIdx = idx
			cv.dragElement = idx
			cv.dragStartX = x
			cv.dragStartY = y
			cv.dragOrigX = profile.Elements[idx].X
			cv.dragOrigY = profile.Elements[idx].Y
		} else {
			cv.selectedIdx = -1
		}
	}

	for _, id := range ebiten.AppendTouchIDs(nil) {
		if cv.dragTarget == "joystick" {
			x, y := ebiten.TouchPosition(id)
			if gameInstance != nil && gameInstance.controls != nil {
				p := gameInstance.controls.Profile()
				if p != nil {
					dx := float64(x-cv.dragStartX) / float64(cv.sw)
					dy := float64(y-cv.dragStartY) / float64(cv.sh)
					origX := float64(cv.dragStartOrigX) / float64(cv.sw)
					origY := float64(cv.dragStartOrigY) / float64(cv.sh)
					p.JoystickX = clamp64(origX+dx, 0.05, 0.95)
					p.JoystickY = clamp64(origY+dy, 0.05, 0.95)
				}
			}
		} else if cv.dragTarget == "statbar" {
			x, y := ebiten.TouchPosition(id)
			if gameInstance != nil && gameInstance.controls != nil {
				p := gameInstance.controls.Profile()
				if p != nil {
					dx := x - cv.dragStartX
					dy := y - cv.dragStartY
					newX := cv.dragStartOrigX + dx
					newY := cv.dragStartOrigY + dy
					if newX < 0 {
						newX = 0
					}
					if newY < 0 {
						newY = 0
					}
					p.StatBarX = newX
					p.StatBarY = newY
				}
			}
		} else if cv.dragElement >= 0 {
			x, y := ebiten.TouchPosition(id)
			profile := cv.state.Profile()
			if profile == nil || cv.dragElement >= len(profile.Elements) {
				continue
			}
			dx := float64(x-cv.dragStartX) / float64(cv.sw)
			dy := float64(y-cv.dragStartY) / float64(cv.sh)
			e := &profile.Elements[cv.dragElement]
			e.X = clamp64(cv.dragOrigX+dx, 0, 1)
			e.Y = clamp64(cv.dragOrigY+dy, 0, 1)
		}
	}

	if cv.dragTarget != "" || cv.dragElement >= 0 {
		released := inpututil.AppendJustReleasedTouchIDs(nil)
		for _, id := range released {
			if _, ok := cv.activeTouches[id]; ok {
				delete(cv.activeTouches, id)
			}
			cv.dragTarget = ""
			cv.dragElement = -1
		}
	} else {
		released := inpututil.AppendJustReleasedTouchIDs(nil)
		for _, id := range released {
			if _, ok := cv.activeTouches[id]; ok {
				delete(cv.activeTouches, id)
			}
		}
	}
}

func (cv *ControlView) hitTest(x, y int, profile *ControlsProfile) int {
	for i := len(profile.Elements) - 1; i >= 0; i-- {
		e := &profile.Elements[i]
		if !e.visible() {
			continue
		}
		ex, ey, ew, eh := cv.elementBounds(e)
		if x >= ex && x < ex+ew && y >= ey && y < ey+eh {
			return i
		}
	}
	return -1
}

func (cv *ControlView) elementBounds(e *ControlElement) (x, y, w, h int) {
	switch e.Type {
	case CtrlJoystick:
		r := int(80 * e.Scale)
		return int(e.X*float64(cv.sw)) - r, int(e.Y*float64(cv.sh)) - r, r * 2, r * 2
	case CtrlButton:
		bw := int(32 * e.Scale)
		bh := int(32 * e.Scale)
		return int(e.X * float64(cv.sw)), int(e.Y * float64(cv.sh)), bw, bh
	case CtrlChatPanel:
		cw := int(e.Scale * e.Width * float64(cv.sw))
		if cw < 60 {
			cw = 180
		}
		return int(e.X * float64(cv.sw)), int(e.Y * float64(cv.sh)), cw, int(float64(cv.sh))
	case CtrlStatusBar:
		return int(e.X * float64(cv.sw)), int(e.Y * float64(cv.sh)), int(120 * e.Scale), int(30 * e.Scale)
	case CtrlDisconnect:
		return int(e.X * float64(cv.sw)), int(e.Y * float64(cv.sh)), int(76 * e.Scale), int(26 * e.Scale)
	case CtrlPlayersList:
		pw := int(e.Scale * e.Width * float64(cv.sw))
		ph := int(e.Scale * e.Height * float64(cv.sh))
		if pw < 60 {
			pw = 120
		}
		if ph < 60 {
			ph = 200
		}
		return int(e.X * float64(cv.sw)), int(e.Y * float64(cv.sh)), pw, ph
	case CtrlInventory:
		iw := int(e.Scale * e.Width * float64(cv.sw))
		ih := int(e.Scale * e.Height * float64(cv.sh))
		if iw < 60 {
			iw = 120
		}
		if ih < 40 {
			ih = 100
		}
		return int(e.X * float64(cv.sw)), int(e.Y * float64(cv.sh)), iw, ih
	case CtrlLabel:
		lw := len(e.Label) * 6
		if lw < 20 {
			lw = 20
		}
		return int(e.X * float64(cv.sw)), int(e.Y * float64(cv.sh)), lw, 14
	}
	return int(e.X * float64(cv.sw)), int(e.Y * float64(cv.sh)), 30, 30
}

func (cv *ControlView) Draw(screen *ebiten.Image) {
	profile := cv.state.Profile()
	if profile == nil {
		return
	}

	connected := gameInstance != nil && gameInstance.net != nil && gameInstance.net.Connected()
	for i := range profile.Elements {
		if !connected && !cv.editing {
			break
		}
		e := &profile.Elements[i]
		if !e.visible() {
			continue
		}
		sel := cv.editing && i == cv.selectedIdx
		cv.drawElement(screen, e, sel)
	}

	// Online / sharing stats (top-left, after disconnect button)
	if gameInstance != nil && gameInstance.net != nil && gameInstance.net.Connected() {
		online, _, shareMe, shareThem := gameInstance.net.SharingStats()
		statsText := fmt.Sprintf("%d online | sharing: %d | shared: %d", online, shareMe, shareThem)
		ebitenutil.DebugPrintAt(screen, statsText, 82, 8)
	}

	if cv.editing {
		// Draw joystick drag indicator
		if gameInstance != nil && gameInstance.controls != nil {
			p := gameInstance.controls.Profile()
			if p != nil {
				jx := int(p.JoystickX * float64(cv.sw))
				jy := int(p.JoystickY * float64(cv.sh))
				ebitenutil.DrawCircle(screen, float64(jx), float64(jy), 82,
					color.RGBA{R: 80, G: 160, B: 255, A: 60})
				ebitenutil.DebugPrintAt(screen, "Move", jx-12, jy+84)

				// Stat bar indicator
				sx, sy := p.StatBarX, p.StatBarY
				sbScale := p.StatBarScale
				if sbScale <= 0 {
					sbScale = 1.0
				}
				sbW := int(60 * sbScale)
				sbH := int(18 * sbScale)
				drawRect(screen, sx-2, sy-2, sbW+4, sbH+4, color.RGBA{R: 80, G: 160, B: 255, A: 60})
				ebitenutil.DebugPrintAt(screen, "Move", sx, sy-14)
			}
		}
		cv.drawEditorUI(screen)
	}
}

func (cv *ControlView) drawElement(screen *ebiten.Image, e *ControlElement, selected bool) {
	c := color.RGBA{R: 100, G: 100, B: 100, A: 180}
	if selected {
		c = color.RGBA{R: 80, G: 160, B: 255, A: 200}
	}

	switch e.Type {
	case CtrlJoystick:
		r := float64(80 * e.Scale)
		bx, by := e.X*float64(cv.sw), e.Y*float64(cv.sh)
		if gameInstance != nil && gameInstance.joystick != nil && gameInstance.joystick.Active() {
			drawCircleOutline(screen, bx, by, r, color.RGBA{R: 255, G: 255, B: 255, A: 220}, 3)
		} else {
			ebitenutil.DrawCircle(screen, bx, by, r, color.RGBA{R: 255, G: 255, B: 255, A: 40})
		}
		if selected {
			ebitenutil.DrawCircle(screen, bx, by, r+2, color.RGBA{R: 80, G: 160, B: 255, A: 100})
		}

	case CtrlButton:
		bw, bh := int(32*e.Scale), int(32*e.Scale)
		bx, by := int(e.X*float64(cv.sw)), int(e.Y*float64(cv.sh))
		bg := color.RGBA{R: 50, G: 70, B: 50, A: 160}
		if selected {
			bg = color.RGBA{R: 80, G: 160, B: 255, A: 180}
		}
		drawRect(screen, bx, by, bw, bh, bg)
		ebitenutil.DebugPrintAt(screen, e.Label, bx+2, by+4)

	case CtrlChatPanel:
		cw := int(e.Scale * e.Width * float64(cv.sw))
		if cw < 60 {
			cw = 180
		}
		bx, by := int(e.X*float64(cv.sw)), int(e.Y*float64(cv.sh))
		drawRect(screen, bx, by, cw, cv.sh, color.RGBA{R: 0, G: 0, B: 0, A: 100})
		if selected {
			drawRect(screen, bx, by, 4, cv.sh, c)
		}

	case CtrlStatusBar:
		bx, by := int(e.X*float64(cv.sw)), int(e.Y*float64(cv.sh))
		if selected {
			drawRect(screen, bx-2, by-2, int(124*e.Scale), int(34*e.Scale), c)
		}

	case CtrlDisconnect:
		bw, bh := int(76*e.Scale), int(26*e.Scale)
		bx, by := int(e.X*float64(cv.sw)), int(e.Y*float64(cv.sh))
		drawRect(screen, bx, by, bw, bh, color.RGBA{R: 140, G: 40, B: 40, A: 160})
		ebitenutil.DebugPrintAt(screen, "Disconnect", bx+6, by+8)
		if selected {
			drawRect(screen, bx, by, bw, bh, c)
		}

	case CtrlPlayersList:
		pw := int(e.Scale * e.Width * float64(cv.sw))
		ph := int(e.Scale * e.Height * float64(cv.sh))
		if pw < 60 {
			pw = 120
		}
		if ph < 60 {
			ph = 200
		}
		bx, by := int(e.X*float64(cv.sw)), int(e.Y*float64(cv.sh))
		drawRect(screen, bx, by, pw, ph, color.RGBA{R: 0, G: 0, B: 0, A: 100})
		ebitenutil.DebugPrintAt(screen, "Players", bx+4, by+4)
		if selected {
			drawRect(screen, bx, by, pw, ph, c)
		}

	case CtrlInventory:
		iw := int(e.Scale * e.Width * float64(cv.sw))
		ih := int(e.Scale * e.Height * float64(cv.sh))
		if iw < 60 {
			iw = 120
		}
		if ih < 40 {
			ih = 100
		}
		bx, by := int(e.X*float64(cv.sw)), int(e.Y*float64(cv.sh))
		drawRect(screen, bx, by, iw, ih, color.RGBA{R: 0, G: 0, B: 0, A: 100})
		ebitenutil.DebugPrintAt(screen, "Inventory", bx+4, by+4)
		if selected {
			drawRect(screen, bx, by, iw, ih, c)
		}

	case CtrlLabel:
		bx, by := int(e.X*float64(cv.sw)), int(e.Y*float64(cv.sh))
		ebitenutil.DebugPrintAt(screen, e.Label, bx, by)
		if selected {
			lw := len(e.Label) * 6
			drawRect(screen, bx-2, by-1, lw+4, 14, c)
		}
	}
}

func (cv *ControlView) drawEditorUI(screen *ebiten.Image) {
	for _, b := range cv.editorButtons() {
		if !b.show {
			continue
		}
		var bg color.RGBA
		switch b.label {
		case "+ Add":
			bg = color.RGBA{R: 60, G: 160, B: 80, A: 255}
		case "Edit":
			bg = color.RGBA{R: 60, G: 120, B: 200, A: 255}
		case "Del":
			bg = color.RGBA{R: 200, G: 60, B: 60, A: 255}
		default:
			bg = color.RGBA{R: 80, G: 80, B: 80, A: 255}
		}
		drawRect(screen, b.x, b.y, b.w, b.h, bg)
		ebitenutil.DebugPrintAt(screen, b.label, b.x+(b.w-len(b.label)*6)/2, b.y+8)
	}

	if cv.selectedIdx >= 0 {
		profile := cv.state.Profile()
		if profile != nil && cv.selectedIdx < len(profile.Elements) {
			e := &profile.Elements[cv.selectedIdx]
			info := fmt.Sprintf("[%s] x:%.0f%% y:%.0f%%", e.Type, e.X*100, e.Y*100)
			ebitenutil.DebugPrintAt(screen, info, cv.sw/2-len(info)*3, 8+28+8)
		}
	}

	if cv.configOpen {
		cv.drawConfigPanel(screen)
	}
	if cv.addMenu {
		cv.drawAddMenu(screen)
	}
}

func (cv *ControlView) editorButtons() []editorButton {
	const btnW, btnH = 80, 28
	pad := 8
	btns := []editorButton{
		{label: "+ Add", show: true},
		{label: "Done", show: true},
	}
	if cv.selectedIdx >= 0 {
		btns = append(btns, editorButton{label: "Edit", show: true})
		btns = append(btns, editorButton{label: "Del", show: true})
	}
	n := len(btns)
	startX := (cv.sw - (n*btnW + (n-1)*pad)) / 2
	if startX < pad {
		startX = pad
	}
	for i := range btns {
		btns[i].x = startX + i*(btnW+pad)
		btns[i].y = pad
		btns[i].w = btnW
		btns[i].h = btnH
	}
	return btns
}

type editorButton struct {
	label string
	show  bool
	x, y  int
	w, h  int
}

func (cv *ControlView) drawConfigPanel(screen *ebiten.Image) {
	sw, sh := screen.Bounds().Dx(), screen.Bounds().Dy()
	profile := cv.state.Profile()
	if profile == nil || cv.selectedIdx >= len(profile.Elements) {
		return
	}
	e := &profile.Elements[cv.selectedIdx]

	editText := ""
	if gameInstance != nil && gameInstance.kbd != nil && gameInstance.kbd.IsVisible() {
		editText = cv.editBuf
	}

	lines := []string{}
	lines = append(lines, fmt.Sprintf("X: %.0f%%", e.X*100))
	lines = append(lines, fmt.Sprintf("Y: %.0f%%", e.Y*100))
	lines = append(lines, fmt.Sprintf("Scale: %.1f", e.Scale))
	if e.Type == CtrlButton || e.Type == CtrlLabel {
		lines = append(lines, "Label: "+e.Label)
		lines = append(lines, "Cmd: "+e.Command)
	}

	lineH := 18
	headerH := 20
	pad := 8
	ph := headerH + pad + len(lines)*lineH + pad + 30
	pw := sw - 80
	px := 40
	py := (sh - ph) / 2
	if py < 40 {
		py = 40
	}

	drawRect(screen, px, py, pw, ph, color.RGBA{R: 20, G: 20, B: 30, A: 240})
	drawRect(screen, px, py, pw, headerH, color.RGBA{R: 50, G: 50, B: 80, A: 255})
	ebitenutil.DebugPrintAt(screen, "Configure: "+string(e.Type), px+4, py+4)

	for i, line := range lines {
		lx := px + 10
		ly := py + headerH + pad + i*lineH
		bg := color.RGBA{R: 40, G: 40, B: 50, A: 255}
		if i == cv.configField {
			bg = color.RGBA{R: 60, G: 80, B: 120, A: 255}
		}
		drawRect(screen, lx, ly, pw-20, 16, bg)
		ebitenutil.DebugPrintAt(screen, line, lx+4, ly+2)
	}

	// Keyboard text preview
	if editText != "" {
		previewY := py + headerH + pad + len(lines)*lineH + 2
		drawRect(screen, px+10, previewY, pw-20, 16, color.RGBA{R: 30, G: 30, B: 40, A: 255})
		ebitenutil.DebugPrintAt(screen, ">"+editText, px+14, previewY+2)
	}

	// Centered preview above the on-screen keyboard while editing a field,
	// so the typed text stays visible even when the keyboard covers the panel.
	if editText != "" && cv.configField >= 3 {
		kbd := gameInstance.kbd
		prevW := sw - 60
		prevX := (sw - prevW) / 2
		prevY := kbd.TopY(sh) - 34
		prevH := 30
		if prevY < 4 {
			prevY = 4
		}
		drawRect(screen, prevX, prevY, prevW, prevH, color.RGBA{R: 30, G: 30, B: 40, A: 255})
		fieldName := "Cmd"
		if cv.configField == 3 {
			fieldName = "Label"
		}
		initChatFont()
		drawScaledText(screen, fieldName+": "+editText, prevX+8, prevY+6, color.RGBA{R: 255, G: 255, B: 255, A: 255}, 1.5)
	}

	// Done button
	doneY := py + ph - 26
	doneW := 60
	doneX := px + pw/2 - doneW/2
	drawRect(screen, doneX, doneY, doneW, 22, color.RGBA{R: 60, G: 160, B: 80, A: 255})
	ebitenutil.DebugPrintAt(screen, "Done", doneX+14, doneY+4)
}

func (cv *ControlView) drawAddMenu(screen *ebiten.Image) {
	sw, sh := screen.Bounds().Dx(), screen.Bounds().Dy()
	px, py, pw, ph := sw/2-80, sh/2-60, 160, 120
	drawRect(screen, px, py, pw, ph, color.RGBA{R: 20, G: 20, B: 30, A: 245})
	drawRect(screen, px, py, pw, 18, color.RGBA{R: 50, G: 50, B: 80, A: 255})
	ebitenutil.DebugPrintAt(screen, "Add Element", px+20, py+3)

	types := []struct {
		t   ControlType
		lbl string
	}{
		{CtrlButton, "Button (/command)"},
		{CtrlLabel, "Label"},
	}
	for i, item := range types {
		iy := py + 22 + i*20
		drawRect(screen, px+4, iy, pw-8, 18, color.RGBA{R: 40, G: 40, B: 50, A: 255})
		ebitenutil.DebugPrintAt(screen, item.lbl, px+10, iy+3)
	}
}

func (cv *ControlView) handleAddMenu(x, y int) {
	sw, sh := cv.sw, cv.sh
	px, py := sw/2-80, sh/2-60

	types := []ControlType{CtrlButton, CtrlLabel}
	for i, t := range types {
		iy := py + 22 + i*20
		if x >= px+4 && x < px+196 && y >= iy && y < iy+18 {
			newElem := ControlElement{
				Type:    t,
				X:       0.5,
				Y:       0.5,
				Scale:   1.0,
				Label:   "new",
				Command: "/",
				Width:   0.25,
				Height:  0.3,
			}
			profile := cv.state.Profile()
			if profile != nil {
				profile.Elements = append(profile.Elements, newElem)
				cv.selectedIdx = len(profile.Elements) - 1
			}
			cv.addMenu = false
			return
		}
	}
	cv.addMenu = false
}

func (cv *ControlView) handleConfig(x, y int) {
	sw, sh := cv.sw, cv.sh

	profile := cv.state.Profile()
	if profile == nil || cv.selectedIdx >= len(profile.Elements) {
		cv.configOpen = false
		return
	}
	e := &profile.Elements[cv.selectedIdx]

	lines := []string{}
	lines = append(lines, fmt.Sprintf("X: %.0f%%", e.X*100))
	lines = append(lines, fmt.Sprintf("Y: %.0f%%", e.Y*100))
	lines = append(lines, fmt.Sprintf("Scale: %.1f", e.Scale))
	if e.Type == CtrlButton || e.Type == CtrlLabel {
		lines = append(lines, "Label: "+e.Label)
		lines = append(lines, "Cmd: "+e.Command)
	}

	lineH := 18
	headerH := 20
	pad := 8
	ph := headerH + pad + len(lines)*lineH + pad + 30
	pw := sw - 80
	px := 40
	py := (sh - ph) / 2
	if py < 40 {
		py = 40
	}

	for i := range lines {
		ly := py + headerH + pad + i*lineH
		if x >= px+10 && x < px+pw-10 && y >= ly && y < ly+16 {
			cv.configField = i
			if gameInstance != nil && gameInstance.kbd != nil {
				kbd := gameInstance.kbd
				switch i {
				case 3:
					cv.editBuf = e.Label
				case 4:
					cv.editBuf = e.Command
				default:
					return
				}
				fieldIdx := i
				kbd.Show(
					func(ch rune) { cv.editBuf += string(ch) },
					func() {
						if len(cv.editBuf) > 0 {
							cv.editBuf = cv.editBuf[:len(cv.editBuf)-1]
						}
					},
					func() {
						switch fieldIdx {
						case 3:
							e.Label = cv.editBuf
						case 4:
							e.Command = cv.editBuf
						}
						kbd.Hide()
					},
				)
			}
			return
		}
	}

	doneY := py + ph - 26
	doneW := 60
	doneX := px + pw/2 - doneW/2
	if x >= doneX && x < doneX+doneW && y >= doneY && y < doneY+22 {
		cv.configOpen = false
		return
	}
}

func (cv *ControlView) HandleToolbarTouch(x, y int) bool {
	if !cv.editing {
		return false
	}

	if cv.configOpen || cv.addMenu {
		return true
	}

	for _, b := range cv.editorButtons() {
		if !b.show {
			continue
		}
		if x >= b.x && x < b.x+b.w && y >= b.y && y < b.y+b.h {
			switch b.label {
			case "Done":
				cv.SetEditing(false)
				if cv.onDone != nil {
					cv.onDone()
				}
			case "+ Add":
				cv.addMenu = true
			case "Edit":
				cv.configOpen = true
			case "Del":
				profile := cv.state.Profile()
				if profile != nil && cv.selectedIdx < len(profile.Elements) {
					profile.Elements = append(profile.Elements[:cv.selectedIdx], profile.Elements[cv.selectedIdx+1:]...)
					cv.selectedIdx = -1
				}
			}
			return true
		}
	}

	return false
}

func (cv *ControlView) drawPlayers(screen *ebiten.Image, e *ControlElement, players []PlayerInfo, shareWith, sharingYou map[string]bool) {
	initChatFont()
	ps := settings.PlayerScale
	if ps <= 0 {
		ps = 1
	}
	pw := int(e.Scale * e.Width * float64(cv.sw))
	ph := int(e.Scale * e.Height * float64(cv.sh))
	if pw < 60 {
		pw = 120
	}
	if ph < 60 {
		ph = 200
	}
	bx, by := int(e.X*float64(cv.sw)), int(e.Y*float64(cv.sh))

	drawRect(screen, bx, by, pw, ph, color.RGBA{R: 0, G: 0, B: 0, A: 120})
	ebitenutil.DebugPrintAt(screen, fmt.Sprintf("Players (%d)", len(players)), bx+4, by+4)

	lineH := int(40 * ps)
	if lineH < 20 {
		lineH = 20
	}
	spriteSz := int(32 * ps)
	if spriteSz < 16 {
		spriteSz = 16
	}
	scrollTop := by + 18
	maxVisible := (ph - 18) / lineH
	if maxVisible < 1 {
		maxVisible = 1
	}

	// Clamp scroll
	total := len(players)
	if cv.scrollY > total-maxVisible {
		cv.scrollY = total - maxVisible
	}
	if cv.scrollY < 0 {
		cv.scrollY = 0
	}

	start := cv.scrollY
	end := start + maxVisible
	if end > total {
		end = total
	}

	for i := start; i < end; i++ {
		pi := players[i]
		ny := scrollTop + (i-start)*lineH
		if ny+lineH > by+ph {
			break
		}
		if gameInstance != nil && gameInstance.clImages != nil {
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
				op.GeoM.Translate(float64(bx+4), float64(ny+4))
				screen.DrawImage(spr, op)
			}
		}
		textX := bx + spriteSz + 8
		textY := ny + (spriteSz-int(metricsHeight(ps)))/2
		if textY < ny {
			textY = ny
		}
		drawPlayerName(screen, pi.Name, textX, textY, color.RGBA{R: 255, G: 255, B: 255, A: 255}, ps, shareWith, sharingYou)
	}

	// Draw player context menu if open for this element
	if cv.playerMenuOpen && cv.playerMenuName != "" {
		menuW := pw
		if menuW < 120 {
			menuW = 120
		}
		menuH := len(playerMenuItems)*30 + 24
		menuX := bx
		menuY := by + 20

		drawRect(screen, menuX-2, menuY-2, menuW+4, menuH+4, color.RGBA{R: 40, G: 40, B: 50, A: 230})
		drawRect(screen, menuX, menuY, menuW, 20, color.RGBA{R: 60, G: 80, B: 120, A: 255})
		ebitenutil.DebugPrintAt(screen, cv.playerMenuName, menuX+4, menuY+4)

		for i, item := range playerMenuItems {
			by2 := menuY + 20 + i*30
			drawRect(screen, menuX, by2, menuW, 28, color.RGBA{R: 50, G: 55, B: 70, A: 255})
			ebitenutil.DebugPrintAt(screen, item.label, menuX+8, by2+8)
		}
	}

	if total > maxVisible {
		scrollInfo := fmt.Sprintf("%d/%d", cv.scrollY+1, total)
		ebitenutil.DebugPrintAt(screen, scrollInfo, bx+pw-30, by+4)
	}
}

func (cv *ControlView) drawInventory(screen *ebiten.Image, e *ControlElement, items []inventoryItem) {
	initChatFont()
	is := settings.InvScale
	if is <= 0 {
		is = 1
	}
	iw := int(e.Scale * e.Width * float64(cv.sw))
	ih := int(e.Scale * e.Height * float64(cv.sh))
	if iw < 60 {
		iw = 120
	}
	if ih < 40 {
		ih = 100
	}
	bx, by := int(e.X*float64(cv.sw)), int(e.Y*float64(cv.sh))

	drawRect(screen, bx, by, iw, ih, color.RGBA{R: 0, G: 0, B: 0, A: 120})

	// Count total slots used (each unique item = 1 slot)
	totalSlots := 0
	for _, item := range items {
		totalSlots += item.quantity
	}
	freeSlots := inventoryMaxSlots - totalSlots
	title := fmt.Sprintf("Inventory %d/%d", totalSlots, inventoryMaxSlots)
	if freeSlots <= 5 {
		title = fmt.Sprintf("%s (%d free)", title, freeSlots)
	}
	ebitenutil.DebugPrintAt(screen, title, bx+4, by+4)

	if gameInstance != nil && gameInstance.clImages != nil {
		gameInstance.net.EnrichInventory(gameInstance.clImages)
	}

	lineH := int(40 * is)
	if lineH < 20 {
		lineH = 20
	}
	spriteSz := int(32 * is)
	if spriteSz < 16 {
		spriteSz = 16
	}
	scrollTop := by + 18
	maxVisible := (ih - 18) / lineH
	if maxVisible < 1 {
		maxVisible = 1
	}

	total := len(items)
	if cv.scrollY > total-maxVisible {
		cv.scrollY = total - maxVisible
	}
	if cv.scrollY < 0 {
		cv.scrollY = 0
	}

	start := cv.scrollY
	end := start + maxVisible
	if end > total {
		end = total
	}

	for i := start; i < end; i++ {
		item := items[i]
		ny := scrollTop + (i-start)*lineH
		if ny+lineH > by+ih {
			break
		}
		prefix := "  "
		if item.equipped {
			prefix = "* "
		}
		if gameInstance != nil && gameInstance.clImages != nil && item.pictID != 0 {
			spr := gameInstance.clImages.Get(uint32(item.pictID), nil, false)
			if spr != nil {
				op := &ebiten.DrawImageOptions{}
				sx0 := float64(spriteSz) / float64(spr.Bounds().Dx())
				sy0 := float64(spriteSz) / float64(spr.Bounds().Dy())
				op.GeoM.Scale(sx0, sy0)
				op.GeoM.Translate(float64(bx+4), float64(ny+4))
				screen.DrawImage(spr, op)
			}
		}
		displayName := prefix + item.name
		if item.quantity > 1 {
			displayName += fmt.Sprintf(" (%d)", item.quantity)
		}
		textX := bx + spriteSz + 6
		textY := ny + (spriteSz-int(metricsHeight(is)))/2
		if textY < ny {
			textY = ny
		}
		drawScaledText(screen, displayName, textX, textY, color.RGBA{R: 255, G: 255, B: 255, A: 255}, is)
	}

	if total > maxVisible {
		scrollInfo := fmt.Sprintf("%d/%d", cv.scrollY+1, total)
		ebitenutil.DebugPrintAt(screen, scrollInfo, bx+iw-30, by+4)
	}
}

func (cv *ControlView) drawChat(screen *ebiten.Image, e *ControlElement, net *Network, kbd *Keyboard, chatBuf string, chatActive bool, chatScroll int) int {
	initChatFont()
	cs := settings.ChatScale
	if cs <= 0 {
		cs = 1
	}
	cw := int(e.Scale * e.Width * float64(cv.sw))
	if cw < 60 {
		cw = 180
	}
	bx, by := int(e.X*float64(cv.sw)), int(e.Y*float64(cv.sh))

	contentY := by
	msgH := cv.sh - 70
	drawRect(screen, bx, contentY, cw, msgH, color.RGBA{R: 0, G: 0, B: 0, A: 60})

	var msgs []ChatMessage
	if net != nil {
		msgs = net.GetTextMessages()
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
	maxWidth := float64(cw-8) / cs

	type styledLine struct {
		text  string
		color color.RGBA
	}
	var allLines []styledLine
	for _, msg := range msgs {
		msgText := strings.ReplaceAll(msg.Text, "\r", "")
		parts := strings.Split(msgText, "\n")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				allLines = append(allLines, styledLine{color: msg.Color})
				continue
			}
			words := strings.Fields(part)
			if len(words) == 0 {
				allLines = append(allLines, styledLine{color: msg.Color})
				continue
			}
			curLine := words[0]
			for _, word := range words[1:] {
				test := curLine + " " + word
				tw, _ := text.Measure(test, chatFace, 0)
				if tw > maxWidth {
					allLines = append(allLines, styledLine{text: curLine, color: msg.Color})
					curLine = word
				} else {
					curLine = test
				}
			}
			allLines = append(allLines, styledLine{text: curLine, color: msg.Color})
		}
	}

	totalLines := len(allLines)
	maxVisible := msgH / lineH
	if maxVisible < 1 {
		maxVisible = 1
	}

	start := totalLines - maxVisible - chatScroll
	if start < 0 {
		start = 0
	}
	end := start + maxVisible
	if end > totalLines {
		end = totalLines
	}

	for i := start; i < end; i++ {
		ly := contentY + (i-start)*lineH + lineH
		if ly > contentY+msgH {
			break
		}
		sl := allLines[i]
		if sl.text == "" {
			continue
		}
		op := &text.DrawOptions{}
		op.GeoM.Scale(cs, cs)
		op.GeoM.Translate(float64(bx+4), float64(ly)-metrics.HDescent*cs)
		op.ColorScale.ScaleWithColor(sl.color)
		text.Draw(screen, sl.text, chatFace, op)
	}

	if totalLines > maxVisible {
		scrollInfo := fmt.Sprintf("%d/%d", totalLines-maxVisible-chatScroll, totalLines-maxVisible)
		ebitenutil.DebugPrintAt(screen, scrollInfo, bx+cw-40, contentY+4)
	}

	inputY := contentY + msgH
	drawRect(screen, bx, inputY, cw, 40, color.RGBA{R: 30, G: 30, B: 40, A: 180})
	if chatActive {
		display := chatBuf + "_"
		ebitenutil.DebugPrintAt(screen, display, bx+4, inputY+14)
	} else {
		ebitenutil.DebugPrintAt(screen, "[Tap to type]", bx+4, inputY+28)
	}

	return inputY
}

func (cv *ControlView) HandleButtonTap(x, y int) bool {
	profile := cv.state.Profile()
	if profile == nil {
		return false
	}
	idx := cv.hitTest(x, y, profile)
	if idx < 0 {
		return false
	}
	e := &profile.Elements[idx]
	if e.Type != CtrlButton || e.Command == "" {
		return false
	}
	if gameInstance != nil && gameInstance.net != nil && gameInstance.net.Connected() {
		gameInstance.net.EnqueueCommand(e.Command)
	}
	return true
}

// HandlePlayerTap checks if a tap landed on a player list row and opens the context menu.
func (cv *ControlView) HandlePlayerTap(x, y int) bool {
	// If menu is open, check menu taps first
	if cv.playerMenuOpen {
		return cv.handlePlayerMenuTap(x, y)
	}

	profile := cv.state.Profile()
	if profile == nil {
		return false
	}

	for i := range profile.Elements {
		e := &profile.Elements[i]
		if e.Type != CtrlPlayersList || !e.visible() {
			continue
		}
		ew := int(e.Scale * e.Width * float64(cv.sw))
		eh := int(e.Scale * e.Height * float64(cv.sh))
		if ew < 60 {
			ew = 120
		}
		if eh < 60 {
			eh = 200
		}
		ex, ey := int(e.X*float64(cv.sw)), int(e.Y*float64(cv.sh))
		if x < ex || x >= ex+ew || y < ey || y >= ey+eh {
			continue
		}

		// Hit inside player element — figure out which row
		players := gameInstance.net.GetSortedPlayers()
		ps := settings.PlayerScale
		if ps <= 0 {
			ps = 1
		}
		lineH := int(40 * ps)
		if lineH < 20 {
			lineH = 20
		}
		spriteSz := int(32 * ps)
		if spriteSz < 16 {
			spriteSz = 16
		}
		scrollTop := ey + 18
		if y < scrollTop {
			return false
		}
		maxVisible := (eh - 18) / lineH
		if maxVisible < 1 {
			maxVisible = 1
		}
		row := (y - scrollTop) / lineH
		idx := cv.scrollY + row
		if idx >= 0 && idx < len(players) {
			cv.playerMenuName = players[idx].Name
			cv.playerMenuOpen = true
			return true
		}
	}
	return false
}

func (cv *ControlView) handlePlayerMenuTap(x, y int) bool {
	if !cv.playerMenuOpen || cv.playerMenuName == "" {
		return false
	}
	// Find the player list element to get menu position (same as draw)
	profile := cv.state.Profile()
	menuW := 120
	menuX := 0
	menuY := 0
	if profile != nil {
		for i := range profile.Elements {
			e := &profile.Elements[i]
			if e.Type != CtrlPlayersList || !e.visible() {
				continue
			}
			ew := int(e.Scale * e.Width * float64(cv.sw))
			if ew < 120 {
				ew = 120
			}
			menuW = ew
			bx, by := int(e.X*float64(cv.sw)), int(e.Y*float64(cv.sh))
			menuX = bx
			menuY = by + 20
			break
		}
	}

	for i, item := range playerMenuItems {
		bx := menuX
		by := menuY + 20 + i*30
		if x >= bx && x < bx+menuW && y >= by && y < by+28 {
			cmd := fmt.Sprintf(item.cmd, cv.playerMenuName)
			if gameInstance != nil && gameInstance.net != nil && gameInstance.net.Connected() {
				gameInstance.net.EnqueueCommand(cmd)
			}
			cv.playerMenuOpen = false
			return true
		}
	}
	// Tap outside closes menu
	cv.playerMenuOpen = false
	return true
}

func clamp64(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func formatInventory(items []inventoryItem) string {
	var b strings.Builder
	for _, item := range items {
		prefix := "  "
		if item.equipped {
			prefix = "* "
		}
		fmt.Fprintf(&b, "%s%s\n", prefix, item.name)
	}
	return b.String()
}
