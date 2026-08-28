package mobile

import (
	"github.com/hajimehoshi/ebiten/v2/mobile"
	godwarf "godwarf"
)

func init() {
	mobile.SetGame(godwarf.NewGame())
}

// Dummy is a dummy exported function.
// gomobile doesn't compile a package that doesn't include any exported function.
func Dummy() {}
