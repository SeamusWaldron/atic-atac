package input

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/seamuswaldron/aticatac/action"
)

// ReadAction reads the current keyboard state and returns the combined action.
func ReadAction() action.Action {
	var a action.Action

	if ebiten.IsKeyPressed(ebiten.KeyUp) || ebiten.IsKeyPressed(ebiten.KeyW) {
		a |= action.Up
	}
	if ebiten.IsKeyPressed(ebiten.KeyDown) || ebiten.IsKeyPressed(ebiten.KeyS) {
		a |= action.Down
	}
	if ebiten.IsKeyPressed(ebiten.KeyLeft) || ebiten.IsKeyPressed(ebiten.KeyA) {
		a |= action.Left
	}
	if ebiten.IsKeyPressed(ebiten.KeyRight) || ebiten.IsKeyPressed(ebiten.KeyD) {
		a |= action.Right
	}
	if ebiten.IsKeyPressed(ebiten.KeySpace) || ebiten.IsKeyPressed(ebiten.KeyZ) {
		a |= action.Fire
	}
	if ebiten.IsKeyPressed(ebiten.KeyEnter) {
		a |= action.Pickup
	}

	return a
}
