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

	if g.doorTimer > 0 {
		g.doorTimer--
	}

	// Redraw: clear play area, draw room frame, draw player sprite
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

// Room centre coordinates — hardcoded in the original Z80 at $8FED/$8FF8.
const (
	roomCentreX = 0x58 // 88 decimal
	roomCentreY = 0x68 // 104 decimal
)

// movePlayer handles player movement input.
// Matches the original Z80 collision system: rectangular bounds centred at
// (0x58, 0x68) with room_width and room_height from the style table.
// X and Y axes are checked independently so the player slides along walls.
func (g *GameEnv) movePlayer(act action.Action) {
	speed := int(2)
	ra := data.RoomAttrs[g.room]
	style := data.RoomStyles[ra.Style]
	rw := int(style.Width)  // room interior half-width
	rh := int(style.Height) // room interior half-height

	g.moving = false
	dx, dy := 0, 0

	if act&action.Up != 0 {
		dy = -speed
		g.playerDir = data.DirUp
		g.moving = true
	}
	if act&action.Down != 0 {
		dy = speed
		g.playerDir = data.DirDown
		g.moving = true
	}
	if act&action.Left != 0 {
		dx = -speed
		g.playerDir = data.DirLeft
		g.moving = true
	}
	if act&action.Right != 0 {
		dx = speed
		g.playerDir = data.DirRight
		g.moving = true
	}

	// Wall check: abs(pos - centre) < dimension means INSIDE the room.
	// Original checks each axis independently so the player slides along walls.
	newX := int(g.playerX) + dx
	xBlocked := !inWallBounds(newX, roomCentreX, rw)
	if !xBlocked {
		g.playerX = byte(newX)
	}

	newY := int(g.playerY) + dy
	yBlocked := !inWallBounds(newY, roomCentreY, rh)
	if !yBlocked {
		g.playerY = byte(newY)
	}

	// If movement was blocked by a wall, check for door exit on that edge.
	if g.doorTimer <= 0 && (xBlocked || yBlocked) {
		g.checkDoorExit(dx, dy, rw, rh)
	}

	if g.moving {
		g.walkCounter++
	}
}

// inWallBounds returns true if pos is inside the room boundary.
// Original Z80: abs(pos - centre) < dimension.
func inWallBounds(pos, centre, dimension int) bool {
	d := pos - centre
	if d < 0 {
		d = -d
	}
	return d < dimension
}

// drawPlayer draws the player character sprite at the current position.
func (g *GameEnv) drawPlayer() {
	sprites := data.CharacterSprites(g.character)
	frame := data.AnimFrame(g.walkCounter)
	sprData := sprites[g.playerDir][frame]
	g.buf.DrawSpriteXOR(int(g.playerX), int(g.playerY), sprData)
}

// checkDoorExit checks if the player is pressing against a wall where a door
// exists and transitions to the connected room. Door positions in the data are
// outside the room bounds (on the wall), so we determine which wall each door
// is on and match it to the direction the player is pressing.
func (g *GameEnv) checkDoorExit(dx, dy, rw, rh int) {
	doors := g.roomDoors[g.room]
	px := int(g.playerX)
	py := int(g.playerY)

	for _, d := range doors {
		doorX := int(d.X)
		doorY := int(d.Y)

		// Determine which wall this door is on by comparing its position to
		// the room bounds.
		onTop := doorY < roomCentreY-rh
		onBottom := doorY > roomCentreY+rh
		onLeft := doorX < roomCentreX-rw
		onRight := doorX > roomCentreX+rw

		// Check if the player is pressing toward this wall AND is roughly
		// aligned with the door on the other axis (within 24 pixels).
		const align = 24
		matched := false

		if onTop && dy < 0 {
			matched = abs(px-doorX) < align
		} else if onBottom && dy > 0 {
			matched = abs(px-doorX) < align
		} else if onLeft && dx < 0 {
			matched = abs(py-doorY) < align
		} else if onRight && dx > 0 {
			matched = abs(py-doorY) < align
		}

		if !matched {
			continue
		}

		// Transition to connected room. Place the player at the opposite
		// wall of the destination room so they enter from the right side.
		destRA := data.RoomAttrs[d.DestRoom]
		destStyle := data.RoomStyles[destRA.Style]
		destRW := int(destStyle.Width)
		destRH := int(destStyle.Height)

		newX := int(d.DestX)
		newY := int(d.DestY)

		// Clamp destination to inside the new room bounds.
		if newX <= roomCentreX-destRW {
			newX = roomCentreX - destRW + 4
		} else if newX >= roomCentreX+destRW {
			newX = roomCentreX + destRW - 4
		}
		if newY <= roomCentreY-destRH {
			newY = roomCentreY - destRH + 4
		} else if newY >= roomCentreY+destRH {
			newY = roomCentreY + destRH - 4
		}

		g.room = d.DestRoom
		g.playerX = byte(newX)
		g.playerY = byte(newY)
		g.roomDrawn = false
		g.hudDirty = true
		g.doorTimer = 25
		return
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
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
