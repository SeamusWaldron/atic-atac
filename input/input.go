package input

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/seamuswaldron/aticatac/action"
)

// ReadAction reads the current keyboard state and returns the combined action.
// Original Spectrum controls: Q=up, A=down, O=left, P=right, Space=fire.
// Arrow keys also supported as alternative.
func ReadAction() action.Action {
	var a action.Action

	if ebiten.IsKeyPressed(ebiten.KeyUp) || ebiten.IsKeyPressed(ebiten.KeyQ) {
		a |= action.Up
	}
	if ebiten.IsKeyPressed(ebiten.KeyDown) || ebiten.IsKeyPressed(ebiten.KeyA) {
		a |= action.Down
	}
	if ebiten.IsKeyPressed(ebiten.KeyLeft) || ebiten.IsKeyPressed(ebiten.KeyO) {
		a |= action.Left
	}
	if ebiten.IsKeyPressed(ebiten.KeyRight) || ebiten.IsKeyPressed(ebiten.KeyP) {
		a |= action.Right
	}
	if ebiten.IsKeyPressed(ebiten.KeySpace) {
		a |= action.Fire
	}
	if ebiten.IsKeyPressed(ebiten.KeyEnter) {
		a |= action.Pickup
	}

	return a
}
