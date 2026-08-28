package godwarf

import (
	"fmt"
	"image/color"
	"log"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"godwarf/climg"
)

var logFile *os.File

func flog(msg string) {
	t := time.Now().Format("15:04:05.000")
	line := t + " " + msg + "\n"
	if logFile != nil {
		logFile.WriteString(line)
		logFile.Sync()
	} else {
		dir := debugLogsDir()
		os.MkdirAll(dir, 0755)
		f, err := os.OpenFile(dir+"/godwarf_go.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err == nil {
			f.WriteString(line)
			f.Sync()
			logFile = f
			return
		}
	}
}

func init() {
	// No-op: logging set up in initGame() after Java provides writable dir
}

// logPanic records a recovered panic (with goroutine stack) to the go log
// file, then re-panics so the app still exits but the trace survives on
// device for debugging without adb.
func logPanic(r interface{}) {
	buf := make([]byte, 32*1024)
	n := runtime.Stack(buf, true)
	flog("PANIC: " + fmt.Sprintf("%v", r))
	flog("STACK:\n" + string(buf[:n]))
}

var (
	gameInstance *Game
	once         sync.Once
)

func NewGame() *Game {
	gameInstance = &Game{}
	return gameInstance
}

type Game struct {
	joystick    *Joystick
	ui          *MobileUI
	kbd         *Keyboard
	net         *Network
	loginState  *LoginState
	screenW     int
	screenH     int
	clImages    *climg.CLImages
	clVersion   int
	controls    *ControlsState
	ctrlView    *ControlView
	dlImagesOK  bool
	dlSoundsOK  bool
	dlImagesErr string
	dlSoundsErr string
}

var initErr string

func (g *Game) Update() error {
	defer func() {
		if r := recover(); r != nil {
			logPanic(r)
			panic(r)
		}
	}()
	once.Do(func() {
		flog("Update: once.Do starting initGame")
		defer func() {
			if r := recover(); r != nil {
				initErr = fmt.Sprintf("PANIC: %v", r)
				flog("PANIC: " + fmt.Sprintf("%v", r))
			}
		}()
		initGame()
		flog("Update: initGame done")
	})
	touches := ebiten.AppendTouchIDs(nil)
	justPressed := inpututil.AppendJustPressedTouchIDs(nil)
	if g.joystick != nil {
		if (g.ctrlView != nil && g.ctrlView.Editing()) || (g.kbd != nil && g.kbd.IsVisible()) {
			g.joystick.Cancel()
		} else if g.net != nil && g.net.Connected() {
			g.joystick.Update(touches, g.screenW, g.screenH)
		} else {
			g.joystick.Cancel()
		}
	}
	if g.kbd != nil {
		g.kbd.UpdateWithTouches(justPressed, g.screenW, g.screenH)
	}
	if g.ui != nil {
		g.ui.UpdateWithTouches(justPressed, touches, g.screenW, g.screenH)
	}

	if g.net != nil && g.net.Connected() && g.joystick != nil {
		if g.ctrlView != nil && g.ctrlView.Editing() {
			g.net.SendInput(0, 0, false)
		} else if macroMoveActive {
			g.net.SendInput(macroMoveDX, macroMoveDY, true)
		} else {
			g.net.SendInput(g.joystick.DX(), g.joystick.DY(), g.joystick.Active())
		}
		g.net.RequestPlayersData()
	} else if g.loginState != nil {
		g.updateLoginInput(justPressed)
	}

	if g.ctrlView != nil {
		g.ctrlView.SetScreen(g.screenW, g.screenH)
		g.ctrlView.UpdateWithTouches(justPressed)

		if !g.ctrlView.Editing() && !g.ui.touchHandled && g.net != nil && g.net.Connected() {
			// Handle button taps when not editing
			for _, id := range justPressed {
				x, y := ebiten.TouchPosition(id)
				if g.ctrlView.HandlePlayerTap(x, y) {
					g.ui.touchHandled = true
				} else if g.ctrlView.HandleButtonTap(x, y) {
					g.ui.touchHandled = true
				}
			}
			// Handle tap on a player in the game world (runs tap macros)
			if !g.ui.touchHandled {
				for _, id := range justPressed {
					x, y := ebiten.TouchPosition(id)
					if name := g.net.FindTapTarget(x, y); name != "" {
						g.net.EnqueueCommand(HandleTapMacro(name))
						g.ui.touchHandled = true
						break
					}
				}
			}
		}
	}

	atomic.AddInt32(&macroAckFrame, 1)
	macroContinue()

	return nil
}

func (g *Game) updateLoginInput(justPressed []ebiten.TouchID) {
	l := g.loginState
	sw := g.screenW

	// Desktop keyboard input
	chars := ebiten.AppendInputChars(nil)
	for _, ch := range chars {
		if ch == '\t' {
			continue
		}
		if ch == '\r' || ch == '\n' {
			l.HandleEnter()
			return
		}
		if ch == 8 || ch == 127 {
			l.Backspace()
			continue
		}
		if ch >= 32 && ch < 127 {
			l.FeedChar(ch)
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyTab) {
		l.selected = (l.selected + 1) % 2
	}

	scale := float64(sw) / 886.0
	if scale < 1 {
		scale = 1
	}
	nameY := int(float64(80) * scale)
	passY := int(float64(130) * scale)
	fieldH := int(float64(28) * scale)
	btnY := int(float64(230) * scale)
	btnH := int(float64(36) * scale)

	// Desktop mouse
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		mx, my := ebiten.CursorPosition()
		cx := g.screenW / 2
		if mx >= cx-120 && mx <= cx+120 {
			switch {
			case my >= nameY-4 && my <= nameY-4+fieldH:
				l.SelectField(0)
				g.openLoginKeyboard()
			case my >= passY-4 && my <= passY-4+fieldH:
				l.SelectField(1)
				g.openLoginKeyboard()
			}
		}
		if mx >= cx-60 && mx <= cx+60 && my >= btnY && my <= btnY+btnH {
			l.tryConnect()
		}
	}

	// Mobile touch input
	for _, id := range justPressed {
		mx, my := ebiten.TouchPosition(id)
		cx := g.screenW / 2
		if mx >= cx-120 && mx <= cx+120 {
			switch {
			case my >= nameY-4 && my <= nameY-4+fieldH:
				l.SelectField(0)
				g.openLoginKeyboard()
			case my >= passY-4 && my <= passY-4+fieldH:
				l.SelectField(1)
				g.openLoginKeyboard()
			}
		}
		if g.kbd == nil || !g.kbd.IsVisible() {
			if mx >= cx-60 && mx <= cx+60 && my >= btnY && my <= btnY+btnH {
				l.tryConnect()
			}
		}
		// Character list taps
		if my >= btnY+btnH+int(60*scale) && my <= g.screenH {
			l.HandleCharacterTap(mx, my, sw)
		}
	}
	// Mouse click on character list
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		mx, my := ebiten.CursorPosition()
		if my >= btnY+btnH+int(60*scale) && my <= g.screenH {
			l.HandleCharacterTap(mx, my, sw)
		}
	}
}

func (g *Game) openLoginKeyboard() {
	l := g.loginState
	kbd := g.kbd
	if kbd.IsVisible() {
		return
	}
	kbd.Show(
		func(ch rune) { l.FeedChar(ch) },
		func() { l.Backspace() },
		func() { l.HandleEnter() },
	)
}

var drawCount int

func (g *Game) Draw(screen *ebiten.Image) {
	defer func() {
		if r := recover(); r != nil {
			logPanic(r)
			panic(r)
		}
	}()
	if screen == nil {
		flog("Draw: nil screen!")
		return
	}
	drawCount++
	if drawCount <= 3 {
		flog(fmt.Sprintf("Draw: #%d %dx%d", drawCount, g.screenW, g.screenH))
	}
	screen.Fill(color.RGBA{R: 136, G: 136, B: 136, A: 255})

	// Show download progress screen if downloads are not all done
	if !g.dlImagesOK || !g.dlSoundsOK {
		g.drawDownloadScreen(screen)
		return
	}

	if g.net != nil && g.net.Connected() {
		g.net.DrawWorld(screen, g.screenW, g.screenH, g.clImages)
		g.ui.Draw(screen)
	} else if g.loginState != nil {
		g.loginState.Draw(screen, g.screenW, g.screenH)
	}

	if g.joystick != nil {
		g.joystick.Draw(screen)
	}

	if g.ctrlView != nil {
		g.ctrlView.Draw(screen)
	}

	g.kbd.Draw(screen, g.screenW, g.screenH)
}

func (g *Game) drawDownloadScreen(screen *ebiten.Image) {
	sw, sh := g.screenW, g.screenH

	title := "goDwarf"
	subtitle := "Updating game data..."
	ebitenutil.DebugPrintAt(screen, title, sw/2-len(title)*3, sh/3)
	ebitenutil.DebugPrintAt(screen, subtitle, sw/2-len(subtitle)*3, sh/3+20)

	active, name, err, done, bytesIn, _ := dlStateImages.snapshot()
	sndActive, sndName, sndErr, sndDone, sndBytesIn, _ := dlStateSounds.snapshot()

	y := sh/3 + 60
	lineH := 18

	// CL_Images status
	if g.dlImagesOK {
		ebitenutil.DebugPrintAt(screen, "  CL_Images  ✓ loaded", sw/4, y)
	} else if active && name != "" && len(name) > 2 && name[:9] == "CL_Images" {
		mb := float64(bytesIn) / 1048576.0
		status := fmt.Sprintf("  CL_Images  downloading... %.1f MB", mb)
		ebitenutil.DebugPrintAt(screen, status, sw/4, y)
		// Progress bar
		barX := sw / 4
		barY := y + lineH
		barW := sw / 2
		barH := 8
		drawProgressBar(screen, barX, barY, barW, barH, -1)
	} else if g.dlImagesErr != "" {
		ebitenutil.DebugPrintAt(screen, "  CL_Images  ERROR: "+g.dlImagesErr, sw/4, y)
	} else if done {
		ebitenutil.DebugPrintAt(screen, "  CL_Images  installing...", sw/4, y)
	} else {
		ebitenutil.DebugPrintAt(screen, "  CL_Images  waiting...", sw/4, y)
	}
	y += lineH * 2

	// CL_Sounds status
	if g.dlSoundsOK {
		ebitenutil.DebugPrintAt(screen, "  CL_Sounds  ✓ loaded", sw/4, y)
	} else if sndActive && sndName != "" && len(sndName) > 2 && sndName[:9] == "CL_Sounds" {
		mb := float64(sndBytesIn) / 1048576.0
		status := fmt.Sprintf("  CL_Sounds  downloading... %.1f MB", mb)
		ebitenutil.DebugPrintAt(screen, status, sw/4, y)
		barX := sw / 4
		barY := y + lineH
		barW := sw / 2
		barH := 8
		drawProgressBar(screen, barX, barY, barW, barH, -1)
	} else if g.dlSoundsErr != "" {
		ebitenutil.DebugPrintAt(screen, "  CL_Sounds  ERROR: "+g.dlSoundsErr, sw/4, y)
	} else if sndDone {
		ebitenutil.DebugPrintAt(screen, "  CL_Sounds  installing...", sw/4, y)
	} else {
		ebitenutil.DebugPrintAt(screen, "  CL_Sounds  waiting...", sw/4, y)
	}
	y += lineH * 2

	// Show fatal error
	fatalErr := ""
	if done && err != "" {
		fatalErr = err
	} else if sndDone && sndErr != "" {
		fatalErr = sndErr
	}
	if fatalErr != "" {
		errMsg := "Download failed: " + fatalErr
		ebitenutil.DebugPrintAt(screen, errMsg, sw/2-len(errMsg)*3, y)
	}
}

func drawProgressBar(screen *ebiten.Image, x, y, w, h int, pct float64) {
	// Background
	ebitenutil.DrawRect(screen, float64(x), float64(y), float64(w), float64(h), color.RGBA{60, 60, 60, 255})
	if pct < 0 {
		// Indeterminate: animated stripe
		t := time.Now().UnixMilli() / 100
		stripeW := w / 4
		for sx := -stripeW; sx < w+stripeW; sx += stripeW * 2 {
			offset := int(t) % (stripeW * 2)
			rx := x + sx + offset
			if rx < x {
				rx += stripeW * 2
			}
			rw := stripeW
			if rx < x {
				rw -= (x - rx)
				rx = x
			}
			if rx+rw > x+w {
				rw = x + w - rx
			}
			if rw > 0 {
				ebitenutil.DrawRect(screen, float64(rx), float64(y), float64(rw), float64(h), color.RGBA{80, 160, 255, 255})
			}
		}
	} else {
		fw := int(pct * float64(w) / 100.0)
		if fw > 0 {
			ebitenutil.DrawRect(screen, float64(x), float64(y), float64(fw), float64(h), color.RGBA{80, 160, 255, 255})
		}
	}
}

var layoutCount int

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	g.screenW = outsideWidth
	g.screenH = outsideHeight
	layoutCount++
	if layoutCount <= 5 {
		flog(fmt.Sprintf("Layout: #%d %dx%d", layoutCount, outsideWidth, outsideHeight))
	}
	return outsideWidth, outsideHeight
}

func initGame() {
	// Set up Go log file using dir Java wrote
	if data, err := os.ReadFile("/storage/emulated/0/Documents/goDwarf/debuglogs/godwarf_dir.txt"); err == nil {
		dir := string(data)
		os.MkdirAll(dir, 0755)
		f, err := os.OpenFile(dir+"/godwarf_go.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err == nil {
			logFile = f
			flog("go log opened at " + dir)
		}
	}

	flog("initGame starting")
	macroInit()
	g := gameInstance
	g.joystick = NewJoystick()
	g.ui = NewMobileUI()
	g.kbd = NewKeyboard()
	g.net = &Network{}
	g.loginState = NewLoginState(g.net)
	g.loginState.onConnectAttempt = func() { g.kbd.Hide() }
	g.controls = NewControlsState()
	g.ctrlView = NewControlView(g.controls)
	g.controls.Save(ControlsPath(dataDir()))
	if err := g.controls.Load(ControlsPath(dataDir())); err == nil {
		flog("controls loaded from disk")
	}
	g.ui.onEditLayout = func() {
		g.ctrlView.SetEditing(true)
	}
	g.ctrlView.onDone = func() {
		if err := g.controls.Save(ControlsPath(dataDir())); err != nil {
			flog(fmt.Sprintf("controls save: %v", err))
		}
	}
	loadSettingsMobile()
	loadCharacters()
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("goDwarf: audio init failed: %v", r)
			}
		}()
		initAudio()
	}()

	go func() {
		flog("CL_Images: starting check/update")
		cl, ver, err := checkAndUpdateImages(dataDir())
		if err != nil {
			flog(fmt.Sprintf("CL_Images ERROR: %v", err))
			g.dlImagesErr = err.Error()
			g.dlImagesOK = true // mark done (with error) so UI can proceed
			return
		}
		g.clImages = cl
		g.clVersion = ver
		g.dlImagesOK = true
		flog(fmt.Sprintf("CL_Images: loaded v%d", ver))
	}()

	go func() {
		flog("CL_Sounds: starting check/update")
		snd, err := checkAndUpdateSounds(dataDir())
		if err != nil {
			flog(fmt.Sprintf("CL_Sounds ERROR: %v", err))
			g.dlSoundsErr = err.Error()
			g.dlSoundsOK = true
			return
		}
		loadCLSounds(snd)
		g.dlSoundsOK = true
		flog("CL_Sounds: loaded")
	}()
}

func maskPass(p string) string {
	out := ""
	for range p {
		out += "*"
	}
	if out == "" {
		return "(empty)"
	}
	return out
}
