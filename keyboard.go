package godwarf

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

type Keyboard struct {
	visible       bool
	shift         bool
	shiftLock     bool
	onKey         func(rune)
	onBackspace   func()
	onEnter       func()
	keyH          int
	keyPad        int
	startY        int
	dismissX      int
	dismissY      int
	consumedTouch bool // true if a key was pressed this frame
	lastKeyIdx    int  // index of last pressed key (-1 = none)
	lastKeyFrame  int  // frame counter when key was pressed
}

func NewKeyboard() *Keyboard {
	return &Keyboard{
		keyH:        44,
		keyPad:      2,
		lastKeyIdx:  -1,
	}
}

func (k *Keyboard) Show(onKey func(rune), onBackspace, onEnter func()) {
	k.visible = true
	k.shift = false
	k.shiftLock = false
	k.onKey = onKey
	k.onBackspace = onBackspace
	k.onEnter = onEnter
}

func (k *Keyboard) Hide() {
	k.visible = false
	k.onKey = nil
	k.onBackspace = nil
	k.onEnter = nil
}

func (k *Keyboard) IsVisible() bool {
	return k.visible
}

// TopY returns the top of the keyboard's opaque background for a screen
// height, matching layout(). Used to position previews above the keyboard.
func (k *Keyboard) TopY(sh int) int {
	if k.keyH <= 0 {
		k.keyH = 44
	}
	return sh - 5*k.keyH - 2
}

type keyDef struct {
	label string
	x, y, w, h int
	ch    rune
	code  int // 0=char, 1=backspace, 2=enter, 3=shift, 4=space
}

func (k *Keyboard) layout(sw, sh int) []keyDef {
	k.startY = sh - 5*k.keyH
	k.dismissX = sw - 30
	k.dismissY = k.startY - 2
	kw := (sw - 16) / 12
	if kw > 44 {
		kw = 44
	}
	totalW := kw*12 + 8*11
	startX := (sw - totalW) / 2

	keys := make([]keyDef, 0, 48)

	// Row 0: 1-9 0 / BS
	r0y := k.startY
	nums := "1234567890/"
	for i, ch := range nums {
		keys = append(keys, keyDef{
			label: string(ch),
			x:     startX + i*(kw+8),
			y:     r0y,
			w:     kw,
			h:     k.keyH,
			ch:    ch,
		})
	}
	keys = append(keys, keyDef{
		label: "<-",
		x:     startX + 11*(kw+8),
		y:     r0y,
		w:     kw,
		h:     k.keyH,
		code:  1,
	})

	// Row 1: q-p
	r1y := k.startY + k.keyH + 2
	letters1 := "qwertyuiop"
	for i, ch := range letters1 {
		keys = append(keys, keyDef{
			label: string(ch),
			x:     startX + i*(kw+8),
			y:     r1y,
			w:     kw,
			h:     k.keyH,
			ch:    ch,
		})
	}

	// Row 2: a-l
	r2y := k.startY + 2*(k.keyH+2)
	letters2 := "asdfghjkl"
	offset2 := (kw + 8) / 2
	for i, ch := range letters2 {
		keys = append(keys, keyDef{
			label: string(ch),
			x:     startX + offset2 + i*(kw+8),
			y:     r2y,
			w:     kw,
			h:     k.keyH,
			ch:    ch,
		})
	}

	// Row 3: shift z-m . , ? !
	r3y := k.startY + 3*(k.keyH+2)
	letters3 := "zxcvbnm"
	shiftW := kw * 2
	keys = append(keys, keyDef{
		label: "SHIFT",
		x:     startX,
		y:     r3y,
		w:     shiftW,
		h:     k.keyH,
		code:  3,
	})
	for i, ch := range letters3 {
		keys = append(keys, keyDef{
			label: string(ch),
			x:     startX + shiftW + 8 + i*(kw+8),
			y:     r3y,
			w:     kw,
			h:     k.keyH,
			ch:    ch,
		})
	}
	puncStart := startX + shiftW + 8 + len(letters3)*(kw+8)
	puncs := []struct {
		label string
		ch    rune
	}{
		{".", '.'},
		{",", ','},
		{"?", '?'},
		{"!", '!'},
	}
	for i, p := range puncs {
		keys = append(keys, keyDef{
			label: p.label,
			x:     puncStart + i*(kw+8),
			y:     r3y,
			w:     kw,
			h:     k.keyH,
			ch:    p.ch,
		})
	}

	// Row 4: SPACE, ENTER — fill full width
	r4y := k.startY + 4*(k.keyH+2)
	totalRowW := 12*kw + 11*8
	enterW := kw*2 + 8
	spaceW := totalRowW - enterW - 8
	keys = append(keys, keyDef{
		label: "SPACE",
		x:     startX,
		y:     r4y,
		w:     spaceW,
		h:     k.keyH,
		ch:    ' ',
		code:  4,
	})
	keys = append(keys, keyDef{
		label: "ENTER",
		x:     startX + spaceW + 8,
		y:     r4y,
		w:     enterW,
		h:     k.keyH,
		code:  2,
	})

	return keys
}

func (k *Keyboard) UpdateWithTouches(justPressed []ebiten.TouchID, sw, sh int) {
	k.consumedTouch = false
	if !k.visible || k.onKey == nil {
		return
	}
	for _, id := range justPressed {
		x, y := ebiten.TouchPosition(id)

		if x >= k.dismissX && x <= k.dismissX+52 && y >= k.dismissY && y <= k.dismissY+28 {
			k.consumedTouch = true
			k.Hide()
			return
		}

		keys := k.layout(sw, sh)
		for idx, kd := range keys {
			if x >= kd.x && x < kd.x+kd.w && y >= kd.y && y < kd.y+kd.h {
				k.consumedTouch = true
				k.lastKeyIdx = idx
				k.lastKeyFrame = 0
				k.handleKey(kd)
				return
			}
		}
	}
}

func (k *Keyboard) handleKey(kd keyDef) {
	switch kd.code {
	case 1:
		if k.onBackspace != nil {
			k.onBackspace()
		}
	case 2:
		if k.onEnter != nil {
			k.onEnter()
		}
	case 3:
		if k.shiftLock {
			k.shift = false
			k.shiftLock = false
		} else if k.shift {
			k.shiftLock = true
		} else {
			k.shift = true
		}
		return
	case 4:
		if k.onKey != nil {
			k.onKey(' ')
		}
		return
	}

	if kd.code == 0 && k.onKey != nil {
		ch := kd.ch
		if ch >= 'a' && ch <= 'z' && (k.shift || k.shiftLock) {
			ch = ch - 'a' + 'A'
		} else if ch >= '1' && ch <= '0' && k.shift {
			return
		}
		k.onKey(ch)
	}

	if k.shift && !k.shiftLock {
		k.shift = false
	}
}

func (k *Keyboard) Draw(screen *ebiten.Image, sw, sh int) {
	if !k.visible {
		return
	}
	if k.lastKeyIdx >= 0 {
		k.lastKeyFrame++
		if k.lastKeyFrame > 8 {
			k.lastKeyIdx = -1
		}
	}
	keys := k.layout(sw, sh)

	bg := color.RGBA{R: 20, G: 20, B: 30, A: 230}
	drawRect(screen, 0, k.startY-2, sw, 5*k.keyH+14, bg)

	// Draw dismiss "X" button at top-right of keyboard area
	k.dismissX = sw - 56
	k.dismissY = k.startY - 30
	drawRect(screen, k.dismissX, k.dismissY, 52, 28, color.RGBA{R: 100, G: 40, B: 40, A: 255})
	ebitenutil.DebugPrintAt(screen, "X", k.dismissX+22, k.dismissY+8)

	for idx, kd := range keys {
		kc := color.RGBA{R: 50, G: 55, B: 70, A: 255}
		if kd.code == 3 && (k.shift || k.shiftLock) {
			kc = color.RGBA{R: 80, G: 120, B: 200, A: 255}
		}
		if kd.code == 2 {
			kc = color.RGBA{R: 60, G: 140, B: 80, A: 255}
		}
		if kd.code == 1 {
			kc = color.RGBA{R: 140, G: 60, B: 60, A: 255}
		}
		// Highlight last pressed key for ~8 frames
		if idx == k.lastKeyIdx && k.lastKeyFrame < 8 {
			kc = color.RGBA{R: 220, G: 220, B: 255, A: 255}
		}
		drawRect(screen, kd.x, kd.y, kd.w, kd.h, kc)

		label := kd.label
		if kd.code == 0 && kd.ch >= 'a' && kd.ch <= 'z' && (k.shift || k.shiftLock) {
			label = string(kd.ch - 'a' + 'A')
		}
		ebitenutil.DebugPrintAt(screen, label, kd.x+kd.w/2-len(label)*3, kd.y+kd.h/2-4)
	}

}
