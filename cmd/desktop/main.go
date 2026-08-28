package main

import (
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	godwarf "godwarf"
)

func main() {
	g := godwarf.NewGame()
	ebiten.SetWindowTitle("goDwarf")
	ebiten.SetWindowSize(800, 480)
	if err := ebiten.RunGame(g); err != nil {
		log.Fatal(err)
	}
}
