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

// Menu text strings from Z80 $7CF8-$7D50
var menuStrings = [7]string{
	"1  KEYBOARD",
	"2  KEMPSTON JOYSTICK",
	"3  CURSOR   JOYSTICK",
	"4  KNIGHT",
	"5  WIZARD",
	"6  SERF",
	"0  START GAME",
}

// Menu Y positions from Z80 $7CF1-$7CF7
var menuYPositions = [7]int{16, 40, 64, 88, 112, 136, 160}

// Menu attr colours from Z80 $7CEA-$7CF0
var menuAttrs = [7]byte{0xC5, 0x45, 0x45, 0xC5, 0x45, 0x45, 0x47}

// Menu text X position from Z80 $7CCD
const menuTextX = 88

// DrawMenu renders the menu screen into the buffer.
func DrawMenu(buf *screen.Buffer, ms *MenuState) {
	buf.Clear()

	// Black background
	buf.FillAttrArea(0, 0, 32, 24, 0x00)

	// Header: "ATICATAC GAME SELECTION" at (0, 0) in bright white
	// Z80: attr=$47, coords $0020 → X=32, Y=0... but looking at original
	// the header is at the top. Z80 coords: H=Y=$00, L=X=$20 → X=32, Y=0
	headerAttr := byte(0x47) // bright white
	buf.FillAttrArea(0, 0, 32, 1, headerAttr)
	buf.DrawStringFrom(32, 0, "ATICATAC GAME SELECTION", &data.GenCharset)

	// Menu options
	for i := 0; i < 7; i++ {
		y := menuYPositions[i]
		attr := menuAttrs[i]

		// Flash the selected character option (4=Knight, 5=Wizard, 6=Serf)
		if i >= 3 && i <= 5 && i-3 == ms.character {
			if (ms.frame/16)%2 == 0 {
				attr |= 0x80 // set FLASH bit
			}
		}
		// Flash the selected input option (just flash option 1=keyboard by default)
		if i == 0 {
			if (ms.frame/16)%2 == 0 {
				attr |= 0x80
			}
		}

		// Set attribute only for the text cells (starting at menuTextX),
		// not the icon area to the left. Text starts at column 11 (88/8).
		row := y >> 3
		textStartCol := menuTextX >> 3 // = 11
		textLen := len(menuStrings[i])
		for c := textStartCol; c < textStartCol+textLen && c < 32; c++ {
			if row >= 0 && row < 24 {
				buf.Attrs[row*32+c] = attr
			}
		}

		buf.DrawStringFrom(menuTextX, y, menuStrings[i], &data.GenCharset)
	}

	// Menu icons from Z80 $A331-$A379
	// Keyboard icon (2 parts): graphic $48/$49 at (32,28)/(48,28), attr $43 (magenta)
	drawMenuIcon(buf, 0x48, 32, 28, 0x43)
	drawMenuIcon(buf, 0x49, 48, 28, 0x43)

	// Joystick icon (2 parts): graphic $4A/$4B at (32,55)/(48,55), attr $44 (green)
	drawMenuIcon(buf, 0x4A, 32, 55, 0x44)
	drawMenuIcon(buf, 0x4B, 48, 55, 0x44)

	// Cursor icon (2 parts): graphic $32/$33 at (32,79)/(48,79), attr $46 (yellow)
	drawMenuIcon(buf, 0x32, 32, 79, 0x46)
	drawMenuIcon(buf, 0x33, 48, 79, 0x46)

	// Character sprites: Knight, Wizard, Serf
	charGraphics := [3]byte{0x01, 0x11, 0x21}
	charYPositions := [3]int{103, 127, 151}
	for i, gfx := range charGraphics {
		// Look up sprite from sprite table
		group := int(gfx) / 4
		frame := int(gfx) % 4
		if group < len(data.GenSpriteTable) {
			sprAddr := data.GenSpriteTable[group][frame]
			if sprAddr != 0 {
				// Read sprite from memory via extraction
				sprData := getSpriteByAddr(sprAddr)
				if sprData != nil {
					buf.DrawSpriteXOR(40, charYPositions[i], sprData)
					// Paint attr for character sprite
					h := int(sprData[0])
					paintMenuAttr(buf, 40, charYPositions[i], h, 0x47)
				}
			}
		}
		_ = i
	}

	// Copyright at bottom: Z80 coords $B800 → X=0, Y=184
	buf.FillAttrArea(0, 23, 32, 1, 0x47)
	buf.DrawStringFrom(0, 184, "\x251983 A.C.G. ALL RIGHTS RESERVED", &data.GenCharset)
}

// drawMenuIcon draws a menu icon sprite by graphic ID.
func drawMenuIcon(buf *screen.Buffer, graphicID byte, x, y int, attr byte) {
	group := int(graphicID) / 4
	frame := int(graphicID) % 4
	if group >= len(data.GenSpriteTable) {
		return
	}
	sprAddr := data.GenSpriteTable[group][frame]
	if sprAddr == 0 {
		return
	}
	sprData := getSpriteByAddr(sprAddr)
	if sprData == nil {
		return
	}
	buf.DrawSpriteXOR(x, y, sprData)
	h := int(sprData[0])
	paintMenuAttr(buf, x, y, h, attr)
}

// paintMenuAttr paints attr for a menu sprite.
func paintMenuAttr(buf *screen.Buffer, x, y, heightPx int, attr byte) {
	startCol := x >> 3
	startRow := y >> 3
	topRow := (y - heightPx + 1) >> 3
	w := 2
	if x&7 != 0 {
		w = 3
	}
	for r := topRow; r <= startRow; r++ {
		for c := 0; c < w; c++ {
			cc := startCol + c
			if cc >= 0 && cc < 32 && r >= 0 && r < 24 {
				buf.Attrs[r*32+cc] = attr
			}
		}
	}
}

// getSpriteByAddr returns sprite data for a given address.
func getSpriteByAddr(addr uint16) []byte {
	if spr, ok := data.GenMenuIcons[addr]; ok {
		return spr
	}
	return nil
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
