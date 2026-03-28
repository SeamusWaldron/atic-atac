package main

import (
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/seamuswaldron/aticatac/game"
)

func main() {
	w, h := game.ScreenSize()
	ebiten.SetWindowSize(w, h)
	ebiten.SetWindowTitle("Atic Atac")
	ebiten.SetTPS(50) // match ZX Spectrum 50Hz PAL

	g := game.New()
	if err := ebiten.RunGame(g); err != nil {
		log.Fatal(err)
	}
}
