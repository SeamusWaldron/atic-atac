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

	// Player position and movement
	playerX     byte
	playerY     byte
	playerDir   int // data.DirLeft/Right/Up/Down
	walkCounter int // animation counter
	moving      bool

	// Door system
	roomDoors map[byte][]data.RoomDoor
	doorTimer int // cooldown to prevent instant re-entry

	// Room rendering state
	roomDrawn bool
	hudDirty  bool
}

// New creates a new game engine.
func New() *GameEnv {
	g := &GameEnv{
		roomDoors: data.BuildRoomDoors(),
	}
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
	g.playerDir = data.DirDown
	g.walkCounter = 0

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

	// Player movement
	g.movePlayer(act)

	// Check door collision
	if g.doorTimer > 0 {
		g.doorTimer--
	} else {
		g.checkDoors()
	}

	// Redraw: clear play area, draw room frame, draw doors, draw player sprite
	g.clearPlayArea()
	g.drawRoom()
	g.drawDoors()
	g.drawPlayer()

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

	g.moving = false
	newX := g.playerX
	newY := g.playerY

	if act&action.Up != 0 && newY > minY+speed {
		newY -= speed
		g.playerDir = data.DirUp
		g.moving = true
	}
	if act&action.Down != 0 && newY < maxY-speed {
		newY += speed
		g.playerDir = data.DirDown
		g.moving = true
	}
	if act&action.Left != 0 && newX > minX+speed {
		newX -= speed
		g.playerDir = data.DirLeft
		g.moving = true
	}
	if act&action.Right != 0 && newX < maxX-speed {
		newX += speed
		g.playerDir = data.DirRight
		g.moving = true
	}

	g.playerX = newX
	g.playerY = newY

	if g.moving {
		g.walkCounter++
	}
}

// roomBounds returns the playable area bounds from the inner frame points.
func (g *GameEnv) roomBounds(style data.RoomStyle) (minX, minY, maxX, maxY byte) {
	minX = 0x20
	minY = 0x20
	maxX = 0xA0
	maxY = 0xA0

	pts := style.Points
	if len(pts) >= 8 {
		minX = pts[5].X
		minY = pts[5].Y
		maxX = pts[6].X
		maxY = pts[4].Y
	}
	return
}

// drawPlayer draws the player character sprite at the current position.
func (g *GameEnv) drawPlayer() {
	sprites := data.CharacterSprites(g.character)
	frame := data.AnimFrame(g.walkCounter)
	sprData := sprites[g.playerDir][frame]
	g.buf.DrawSpriteXOR(int(g.playerX), int(g.playerY), sprData)
}

// checkDoors checks if the player is near a door and transitions rooms.
func (g *GameEnv) checkDoors() {
	doors := g.roomDoors[g.room]
	const doorRadius = 10

	for _, d := range doors {
		dx := int(g.playerX) - int(d.X)
		dy := int(g.playerY) - int(d.Y)
		if dx < 0 {
			dx = -dx
		}
		if dy < 0 {
			dy = -dy
		}
		if dx < doorRadius && dy < doorRadius {
			g.room = d.DestRoom
			g.playerX = d.DestX
			g.playerY = d.DestY
			g.roomDrawn = false
			g.hudDirty = true
			g.doorTimer = 15 // cooldown frames to prevent bouncing
			return
		}
	}
}

// drawDoors renders door markers in the current room.
func (g *GameEnv) drawDoors() {
	doors := g.roomDoors[g.room]
	for _, d := range doors {
		x := int(d.X)
		y := int(d.Y)
		// Draw a simple door marker (small rectangle)
		for dx := -3; dx <= 3; dx++ {
			g.buf.SetPixel(x+dx, y-4)
			g.buf.SetPixel(x+dx, y+4)
		}
		for dy := -4; dy <= 4; dy++ {
			g.buf.SetPixel(x-3, y+dy)
			g.buf.SetPixel(x+3, y+dy)
		}
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
	g.buf.FillAttrArea(24, 0, 8, 24, 0x47)

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
