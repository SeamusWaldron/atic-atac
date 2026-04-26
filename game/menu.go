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
	// Settings (persist across games)
	DebugMap      bool // Shift+6 map enabled
	DebugKeys     bool // Shift+0/3 key/passage jump enabled
	Immunity      bool // player never loses energy
	InfiniteLives bool // player has infinite lives
	ColourClash   bool // authentic ZX Spectrum colour clash (default on)
	View3D        bool // start in 3D view mode
	settingsOpen  bool // settings sub-menu active
	settingSel    int  // selected setting (0-5)
	wasUp         bool
	wasDown       bool
	wasEnter      bool
	wasEsc        bool
}

// NewMenuState returns a MenuState with defaults (colour clash enabled).
func NewMenuState() MenuState {
	return MenuState{ColourClash: true}
}

var menuStrings = [6]string{
	"1  KEYBOARD",
	"2  SETTINGS",
	"3  KNIGHT",
	"4  WIZARD",
	"5  SERF",
	"0  START GAME",
}

var menuYPositions = [6]int{16, 40, 72, 96, 120, 152}
var menuAttrs = [6]byte{0xC5, 0x45, 0xC5, 0x45, 0x45, 0x47}

const menuTextX = 88

// DrawMenu renders the menu screen into the buffer.
func DrawMenu(buf *screen.Buffer, ms *MenuState) {
	buf.Clear()
	buf.FillAttrArea(0, 0, 32, 24, 0x00)
	cs := &data.GenCharset

	if ms.settingsOpen {
		drawSettings(buf, ms)
		return
	}

	// Header
	buf.FillAttrArea(0, 0, 32, 1, 0x47)
	buf.DrawStringFrom(32, 0, "ATICATAC GAME SELECTION", cs)

	// Menu options
	for i := 0; i < 6; i++ {
		y := menuYPositions[i]
		attr := menuAttrs[i]

		// Flash selected character (options 2-4 = Knight/Wizard/Serf)
		if i >= 2 && i <= 4 {
			if i-2 == ms.character {
				attr |= 0x80
			} else {
				attr &^= 0x80
			}
		}
		// Keyboard always selected
		if i == 0 {
			attr |= 0x80
		}

		row := y >> 3
		textStartCol := menuTextX >> 3
		textLen := len(menuStrings[i])
		for c := textStartCol; c < textStartCol+textLen && c < 32; c++ {
			buf.SetCellAttr(c, row, attr)
		}
		buf.DrawStringFrom(menuTextX, y, menuStrings[i], cs)
	}

	// Icons
	// Keyboard
	drawMenuIcon(buf, 0x48, 32, 28, 0x43)
	drawMenuIcon(buf, 0x49, 48, 28, 0x43)

	// Settings icon (use a gear-like item sprite)
	drawMenuIcon(buf, 0x8B, 40, 55, 0x45) // spanner as settings icon

	// Character sprites
	charGraphics := [3]byte{0x01, 0x11, 0x21}
	charYPositions := [3]int{87, 111, 135}
	for i, gfx := range charGraphics {
		flatIdx := int(gfx) - 1
		group := flatIdx / 4
		frame := flatIdx % 4
		if group < len(data.GenSpriteTable) {
			sprAddr := data.GenSpriteTable[group][frame]
			if sprAddr != 0 {
				sprData := getSpriteByAddr(sprAddr)
				if sprData != nil {
					buf.DrawSpriteXOR(40, charYPositions[i], sprData)
					h := int(sprData[0])
					paintMenuAttr(buf, 40, charYPositions[i], h, 0x47)
				}
			}
		}
	}

	// Copyright
	buf.FillAttrArea(0, 23, 32, 1, 0x47)
	buf.DrawStringFrom(0, 184, "\x251983 A.C.G. ALL RIGHTS RESERVED", cs)
}

// drawSettings renders the settings sub-menu.
func drawSettings(buf *screen.Buffer, ms *MenuState) {
	cs := &data.GenCharset

	buf.FillAttrArea(0, 0, 24, 1, 0x47)
	buf.DrawStringFrom(48, 0, "SETTINGS", cs)

	settings := [6]struct {
		name string
		on   bool
	}{
		{"DEBUG MAP", ms.DebugMap},
		{"KEY JUMP", ms.DebugKeys},
		{"IMMUNITY", ms.Immunity},
		{"INFINITE LIVES", ms.InfiniteLives},
		{"COLOUR CLASH", ms.ColourClash},
		{"3D VIEW", ms.View3D},
	}

	for i, s := range settings {
		// 16-pixel spacing: every y is a multiple of 8 (cell-aligned) so
		// the per-cell attribute correctly highlights the entire text row.
		y := 24 + i*16
		attr := byte(0x45) // cyan

		// Draw option
		label := s.name
		if s.on {
			label += "  ON"
		} else {
			label += "  OFF"
		}
		buf.DrawStringFrom(32, y, label, cs)

		// Highlight selected
		if i == ms.settingSel {
			attr |= 0x80 // FLASH
		}

		// Set attrs
		row := y >> 3
		for c := 4; c < 24; c++ {
			buf.SetCellAttr(c, row, attr)
		}
	}

	// Instructions (cell-aligned positions: y=144 → row 18, y=160 → row 20)
	buf.DrawStringFrom(16, 144, "Q/A#SELECT  ENT#TOGGLE", cs)
	buf.FillAttrArea(0, 18, 24, 1, 0x46)
	buf.DrawStringFrom(48, 160, "ESC#BACK", cs)
	buf.FillAttrArea(0, 20, 24, 1, 0x45)
}

func drawMenuIcon(buf *screen.Buffer, graphicID byte, x, y int, attr byte) {
	flatIdx := int(graphicID) - 1
	group := flatIdx / 4
	frame := flatIdx % 4
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
			buf.SetCellAttr(startCol+c, r, attr)
		}
	}
}

func getSpriteByAddr(addr uint16) []byte {
	if spr, ok := data.GenMenuIcons[addr]; ok {
		return spr
	}
	return nil
}

// UpdateMenu handles menu input. Returns true if game should start.
func UpdateMenu(ms *MenuState, eng *engine.GameEnv) bool {
	ms.frame++

	if ms.settingsOpen {
		return updateSettings(ms)
	}

	// Character select: 3=Knight, 4=Wizard, 5=Serf
	if ebiten.IsKeyPressed(ebiten.KeyDigit3) {
		ms.character = 0
	}
	if ebiten.IsKeyPressed(ebiten.KeyDigit4) {
		ms.character = 1
	}
	if ebiten.IsKeyPressed(ebiten.KeyDigit5) {
		ms.character = 2
	}

	// Settings: key 2
	if ebiten.IsKeyPressed(ebiten.KeyDigit2) {
		ms.settingsOpen = true
		ms.settingSel = 0
	}

	// Start game
	if ebiten.IsKeyPressed(ebiten.KeyDigit0) {
		charClasses := [3]data.CharacterClass{data.Knight, data.Wizard, data.Serf}
		eng.SetCharacter(charClasses[ms.character])
		// Apply settings to engine
		eng.SetImmunity(ms.Immunity)
		eng.SetInfiniteLives(ms.InfiniteLives)
		return true
	}

	return false
}

// updateSettings handles input in the settings sub-menu.
func updateSettings(ms *MenuState) bool {
	const numSettings = 6
	// Q = up, A = down
	up := ebiten.IsKeyPressed(ebiten.KeyQ)
	if up && !ms.wasUp {
		ms.settingSel--
		if ms.settingSel < 0 {
			ms.settingSel = numSettings - 1
		}
	}
	ms.wasUp = up

	down := ebiten.IsKeyPressed(ebiten.KeyA)
	if down && !ms.wasDown {
		ms.settingSel++
		if ms.settingSel >= numSettings {
			ms.settingSel = 0
		}
	}
	ms.wasDown = down

	// Enter = toggle
	enter := ebiten.IsKeyPressed(ebiten.KeyEnter)
	if enter && !ms.wasEnter {
		switch ms.settingSel {
		case 0:
			ms.DebugMap = !ms.DebugMap
		case 1:
			ms.DebugKeys = !ms.DebugKeys
		case 2:
			ms.Immunity = !ms.Immunity
		case 3:
			ms.InfiniteLives = !ms.InfiniteLives
		case 4:
			ms.ColourClash = !ms.ColourClash
			if ms.ColourClash {
				screen.Mode = screen.ColourModeAuthentic
			} else {
				screen.Mode = screen.ColourModePerPixel
			}
		case 5:
			ms.View3D = !ms.View3D
		}
	}
	ms.wasEnter = enter

	// Escape = back
	esc := ebiten.IsKeyPressed(ebiten.KeyEscape)
	if esc && !ms.wasEsc {
		ms.settingsOpen = false
	}
	ms.wasEsc = esc

	return false
}
