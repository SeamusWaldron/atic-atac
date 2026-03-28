package game

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/seamuswaldron/aticatac/data"
	"github.com/seamuswaldron/aticatac/engine"
	"github.com/seamuswaldron/aticatac/screen"
)

// MenuState holds the menu selection state.
type MenuState struct {
	character int // 0=Knight, 1=Wizard, 2=Serf
	frame     int
}

// DrawMenu renders the menu screen into the buffer.
func DrawMenu(buf *screen.Buffer, ms *MenuState) {
	buf.Clear()

	// Black background with white text
	buf.FillAttrArea(0, 0, 32, 24, 0x47) // bright white on black

	// Title
	buf.DrawString(24, 8, "ATICATAC GAME SELECTION")

	// Menu options — Y positions from Z80: $10,$28,$40,$58,$70,$88,$A0
	options := []struct {
		y    int
		text string
		attr byte
	}{
		{16, "1  KEYBOARD", 0x45},     // cyan
		{40, "2  KEMPSTON JOYSTICK", 0x45},
		{64, "3  CURSOR   JOYSTICK", 0x45},
		{88, "4  KNIGHT", 0x45},
		{112, "5  WIZARD", 0x45},
		{136, "6  SERF", 0x45},
		{160, "0  START GAME", 0x47}, // white
	}

	for i, opt := range options {
		// Set attribute row colour
		row := opt.y >> 3
		buf.FillAttrArea(0, row, 32, 1, opt.attr)

		// Flash the selected character option
		if i == 3+ms.character {
			// Flash effect: alternate between normal and inverse
			if (ms.frame/16)%2 == 0 {
				buf.FillAttrArea(0, row, 32, 1, opt.attr|0x80) // FLASH bit
			}
		}

		buf.DrawString(64, opt.y, opt.text)
	}

	// Draw character preview sprites next to options 4-6
	charClasses := [3]data.CharacterClass{data.Knight, data.Wizard, data.Serf}
	for i, cls := range charClasses {
		sprites := data.CharacterSprites(cls)
		sprData := sprites[data.DirDown][0]
		y := 88 + i*24
		buf.DrawSpriteXOR(32, y+int(sprData[0]), sprData) // draw at left of text
	}

	// Copyright
	buf.FillAttrArea(0, 23, 32, 1, 0x47)
	buf.DrawString(16, 184, "1983 A.C.G. ALL RIGHTS RESERVED")
}

// UpdateMenu handles menu input. Returns true if game should start.
func UpdateMenu(ms *MenuState, eng *engine.GameEnv) bool {
	ms.frame++

	// Character select: 4=Knight, 5=Wizard, 6=Serf
	if ebiten.IsKeyPressed(ebiten.Key4) {
		ms.character = 0
	}
	if ebiten.IsKeyPressed(ebiten.Key5) {
		ms.character = 1
	}
	if ebiten.IsKeyPressed(ebiten.Key6) {
		ms.character = 2
	}

	// Start game
	if ebiten.IsKeyPressed(ebiten.Key0) {
		charClasses := [3]data.CharacterClass{data.Knight, data.Wizard, data.Serf}
		eng.SetCharacter(charClasses[ms.character])
		return true
	}

	return false
}
