package game

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/seamuswaldron/aticatac/data"
	"github.com/seamuswaldron/aticatac/engine"
	"github.com/seamuswaldron/aticatac/input"
	"github.com/seamuswaldron/aticatac/screen"
)

const (
	screenW = screen.ScreenWidthPx
	screenH = screen.ScreenHeightPx
	scale   = 3
)

// Game is the Ebitengine wrapper around the headless engine.
type Game struct {
	eng    *engine.GameEnv
	img    *ebiten.Image
	pixels []byte
	result engine.StepResult
	room   byte

	// Debounce for room switching keys
	nWasPressed bool
	pWasPressed bool
}

// New creates a new Ebitengine game.
func New() *Game {
	g := &Game{
		eng:    engine.New(),
		img:    ebiten.NewImage(screenW, screenH),
		pixels: make([]byte, screenW*screenH*4),
	}
	g.result = g.eng.Step(0)
	return g
}

// Update is called every tick (target: 50 TPS).
func (g *Game) Update() error {
	// Room browsing: N = next, P = previous
	nPressed := ebiten.IsKeyPressed(ebiten.KeyN)
	if nPressed && !g.nWasPressed {
		g.room++
		if int(g.room) >= data.NumRooms {
			g.room = 0
		}
		g.eng.ChangeRoom(g.room)
	}
	g.nWasPressed = nPressed

	pPressed := ebiten.IsKeyPressed(ebiten.KeyP)
	if pPressed && !g.pWasPressed {
		if g.room == 0 {
			g.room = byte(data.NumRooms - 1)
		} else {
			g.room--
		}
		g.eng.ChangeRoom(g.room)
	}
	g.pWasPressed = pPressed

	act := input.ReadAction()
	g.result = g.eng.Step(act)
	return nil
}

// Draw renders the current frame.
func (g *Game) Draw(scr *ebiten.Image) {
	screen.RenderToRGBA(g.result.Buffer, g.pixels)
	g.img.WritePixels(g.pixels)
	op := &ebiten.DrawImageOptions{}
	scr.DrawImage(g.img, op)
}

// Layout returns the game's logical screen size.
func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return screenW, screenH
}

// ScreenSize returns the window dimensions.
func ScreenSize() (int, int) {
	return screenW * scale, screenH * scale
}
