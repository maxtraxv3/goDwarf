package godwarf

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

type Joystick struct {
	active  bool
	touchID ebiten.TouchID
	baseX   float32
	baseY   float32
	knobX   float32
	knobY   float32
	radius  float32
	knobR   float32
	dx, dy  float32
}

func NewJoystick() *Joystick {
	return &Joystick{
		radius: 80,
		knobR:  30,
	}
}

func (j *Joystick) screenPos(sw, sh int) (float32, float32) {
	if gameInstance != nil && gameInstance.controls != nil {
		p := gameInstance.controls.Profile()
		if p != nil {
			return float32(p.JoystickX * float64(sw)), float32(p.JoystickY * float64(sh))
		}
	}
	return float32(sw) * 0.12, float32(sh) * 0.55
}

func (j *Joystick) Update(touches []ebiten.TouchID, sw, sh int) {
	if j.active {
		found := false
		for _, id := range touches {
			if id == j.touchID {
				found = true
				x, y := ebiten.TouchPosition(id)
				j.knobX = float32(x)
				j.knobY = float32(y)
				break
			}
		}
		if !found {
			j.active = false
			j.dx = 0
			j.dy = 0
			return
		}
		dx := j.knobX - j.baseX
		dy := j.knobY - j.baseY
		dist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
		if dist > j.radius {
			dx = dx / dist * j.radius
			dy = dy / dist * j.radius
			j.knobX = j.baseX + dx
			j.knobY = j.baseY + dy
		}
		j.dx = dx / j.radius
		j.dy = dy / j.radius

		// Apply dead zone
		dz := float32(settings.JoyDeadZone)
		mag := float32(math.Sqrt(float64(j.dx*j.dx + j.dy*j.dy)))
		if mag < dz {
			j.dx = 0
			j.dy = 0
		} else {
			// Rescale so dead zone edge = 0, full radius = 1
			remap := (mag - dz) / (1 - dz)
			scale := remap / mag
			j.dx *= scale
			j.dy *= scale
		}

		// Apply speed multiplier
		speed := float32(settings.JoySpeed)
		if speed != 1.0 {
			j.dx *= speed
			j.dy *= speed
			// Clamp to [-1, 1]
			if j.dx > 1 {
				j.dx = 1
			} else if j.dx < -1 {
				j.dx = -1
			}
			if j.dy > 1 {
				j.dy = 1
			} else if j.dy < -1 {
				j.dy = -1
			}
		}

		return
	}

	for _, id := range touches {
		x, y := ebiten.TouchPosition(id)
		jx, jy := j.screenPos(sw, sh)
		dx := float32(x) - jx
		dy := float32(y) - jy
		if dx*dx+dy*dy < float32(j.radius*j.radius) {
			j.active = true
			j.touchID = id
			j.baseX = jx
			j.baseY = jy
			j.knobX = float32(x)
			j.knobY = float32(y)
			j.dx = 0
			j.dy = 0
			return
		}
	}
}

func (j *Joystick) Draw(screen *ebiten.Image) {
	if !j.active {
		return
	}
	drawCircleOutline(screen, float64(j.baseX), float64(j.baseY), float64(j.radius),
		color.RGBA{R: 255, G: 255, B: 255, A: 140}, 2)
	drawCircleOutline(screen, float64(j.knobX), float64(j.knobY), float64(j.knobR),
		color.RGBA{R: 255, G: 255, B: 255, A: 200}, 2)
}

func (j *Joystick) DX() float32  { return j.dx }
func (j *Joystick) DY() float32  { return j.dy }
func (j *Joystick) Active() bool { return j.active }

func (j *Joystick) Cancel() {
	if j.active {
		j.active = false
		j.dx = 0
		j.dy = 0
	}
}
