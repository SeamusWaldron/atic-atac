package engine

import (
	"github.com/seamuswaldron/aticatac/action"
	"github.com/seamuswaldron/aticatac/data"
	"github.com/seamuswaldron/aticatac/screen"
)

// GameEnv is the headless game engine with Step/Reset API.
type GameEnv struct {
	buf screen.Buffer

	// Game state
	state     GameState
	room      byte
	lives     byte
	energy    byte
	score     uint32
	character data.CharacterClass

	// Player position (original coordinate system)
	playerX    byte
	playerY    byte
	oldPlayerX byte
	oldPlayerY byte
	playerDrawn bool

	// Room rendering state
	roomDrawn bool

	// HUD needs redraw
	hudDirty bool
}

// New creates a new game engine.
func New() *GameEnv {
	g := &GameEnv{}
	g.Reset()
	return g
}

// Reset resets the game to initial state.
func (g *GameEnv) Reset() {
	g.state = StatePlaying
	g.character = data.Knight
	g.lives = InitialLives
	g.energy = InitialEnergy
	g.score = 0
	g.roomDrawn = false
	g.hudDirty = true
	g.playerDrawn = false

	ch := data.Characters[g.character]
	g.room = ch.StartRoom
	g.playerX = ch.StartX
	g.playerY = ch.StartY

	g.buf.Clear()
}

// SetCharacter sets the player character class and resets.
func (g *GameEnv) SetCharacter(c data.CharacterClass) {
	g.character = c
	g.Reset()
}

// Step advances the game by one frame with the given action.
func (g *GameEnv) Step(act action.Action) StepResult {
	switch g.state {
	case StatePlaying:
		g.stepPlaying(act)
	}

	return StepResult{
		Buffer:   &g.buf,
		Score:    g.score,
		Lives:    g.lives,
		Energy:   g.energy,
		Room:     g.room,
		State:    g.state,
		GameOver: g.state == StateGameOver || g.state == StateWin,
	}
}

// ChangeRoom switches to a different room.
func (g *GameEnv) ChangeRoom(room byte) {
	if int(room) >= data.NumRooms {
		return
	}
	g.room = room
	g.roomDrawn = false
	g.hudDirty = true
	g.playerDrawn = false

	// Reset player to centre of new room
	g.playerX = 0x60
	g.playerY = 0x60
}

// stepPlaying handles one frame of gameplay.
func (g *GameEnv) stepPlaying(act action.Action) {
	if !g.roomDrawn {
		g.clearPlayArea()
		g.drawRoom()
		g.roomDrawn = true
		g.hudDirty = true
	}

	// Erase old player (XOR to undo)
	if g.playerDrawn {
		g.xorPlayerAt(int(g.oldPlayerX), int(g.oldPlayerY))
	}

	// Player movement
	g.movePlayer(act)

	// Draw player at new position (XOR)
	g.xorPlayerAt(int(g.playerX), int(g.playerY))
	g.oldPlayerX = g.playerX
	g.oldPlayerY = g.playerY
	g.playerDrawn = true

	// Draw HUD (only full redraw when dirty, otherwise just update dynamic parts)
	if g.hudDirty {
		g.clearHUDArea()
		g.drawHUD()
		g.hudDirty = false
	}
}

// clearPlayArea clears the 24×24 character play area (192×192 pixels).
func (g *GameEnv) clearPlayArea() {
	for y := 0; y < 192; y++ {
		addr := screen.PixelAddr(0, y)
		for col := 0; col < 24; col++ {
			g.buf.Pixels[addr+uint16(col)] = 0
		}
	}
}

// clearHUDArea clears the right 8-column HUD area.
func (g *GameEnv) clearHUDArea() {
	for y := 0; y < 192; y++ {
		addr := screen.PixelAddr(192, y)
		for col := 0; col < 8; col++ {
			g.buf.Pixels[addr+uint16(col)] = 0
		}
	}
}

// movePlayer handles player movement input.
func (g *GameEnv) movePlayer(act action.Action) {
	speed := byte(2)
	ra := data.RoomAttrs[g.room]
	style := data.RoomStyles[ra.Style]

	minX, minY, maxX, maxY := g.roomBounds(style)

	newX := g.playerX
	newY := g.playerY

	if act&action.Up != 0 && newY > minY+speed {
		newY -= speed
	}
	if act&action.Down != 0 && newY < maxY-speed {
		newY += speed
	}
	if act&action.Left != 0 && newX > minX+speed {
		newX -= speed
	}
	if act&action.Right != 0 && newX < maxX-speed {
		newX += speed
	}

	g.playerX = newX
	g.playerY = newY
}

// roomBounds returns the playable area bounds from the inner frame points.
func (g *GameEnv) roomBounds(style data.RoomStyle) (minX, minY, maxX, maxY byte) {
	minX = 0x20
	minY = 0x20
	maxX = 0xA0
	maxY = 0xA0

	pts := style.Points
	if len(pts) >= 8 {
		// For square/rect styles, inner frame is points 4-7
		minX = pts[5].X
		minY = pts[5].Y
		maxX = pts[6].X
		maxY = pts[4].Y
	}
	return
}

// xorPlayerAt draws/erases a player marker at the given position using XOR.
func (g *GameEnv) xorPlayerAt(x, y int) {
	for d := -2; d <= 2; d++ {
		g.buf.XORPixel(x+d, y)
		g.buf.XORPixel(x, y+d)
	}
}

// drawRoom renders the current room frame to the buffer.
func (g *GameEnv) drawRoom() {
	if int(g.room) >= data.NumRooms {
		return
	}
	ra := data.RoomAttrs[g.room]
	if int(ra.Style) >= len(data.RoomStyles) {
		return
	}
	style := data.RoomStyles[ra.Style]

	// Fill attributes with room colour (24×24 character area)
	g.buf.FillAttrArea(0, 0, 24, 24, ra.Colour)

	// Draw room frame lines
	for _, lg := range style.Lines {
		if len(lg.Dsts) == 0 {
			continue
		}
		srcIdx := int(lg.Src)
		if srcIdx >= len(style.Points) {
			continue
		}
		src := style.Points[srcIdx]

		for _, di := range lg.Dsts {
			dstIdx := int(di)
			if dstIdx >= len(style.Points) {
				continue
			}
			dst := style.Points[dstIdx]
			g.buf.DrawLine(int(src.X), int(src.Y), int(dst.X), int(dst.Y))
		}
	}
}

// drawHUD draws the heads-up display.
func (g *GameEnv) drawHUD() {
	// Right side panel: columns 24-31
	g.buf.FillAttrArea(24, 0, 8, 24, 0x47) // white ink, black paper

	g.buf.DrawString(200, 8, "SCORE")
	g.buf.DrawString(200, 16, formatBCD(g.score))

	g.buf.DrawString(200, 32, "LIVES")
	livesStr := make([]byte, g.lives)
	for i := range livesStr {
		livesStr[i] = '*'
	}
	g.buf.DrawString(200, 40, string(livesStr))

	g.buf.DrawString(200, 56, "ENRGY")
	energyBars := int(g.energy) >> 4
	barStr := make([]byte, energyBars)
	for i := range barStr {
		barStr[i] = '|'
	}
	g.buf.DrawString(200, 64, string(barStr))

	g.buf.DrawString(200, 80, "ROOM")
	g.buf.DrawString(200, 88, formatByte(g.room))

	// Character name
	g.buf.DrawString(200, 104, data.Characters[g.character].Name)
}

func formatBCD(val uint32) string {
	digits := [6]byte{}
	for i := 5; i >= 0; i-- {
		digits[i] = byte(val%10) + '0'
		val /= 10
	}
	return string(digits[:])
}

func formatByte(val byte) string {
	hi := val >> 4
	lo := val & 0x0F
	hexChar := func(n byte) byte {
		if n < 10 {
			return n + '0'
		}
		return n - 10 + 'A'
	}
	return string([]byte{hexChar(hi), hexChar(lo)})
}
