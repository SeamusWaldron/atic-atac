package engine

import (
	"github.com/seamuswaldron/aticatac/action"
	"github.com/seamuswaldron/aticatac/data"
	"github.com/seamuswaldron/aticatac/entity"
	"github.com/seamuswaldron/aticatac/screen"
)

// Room centre coordinates — hardcoded in the original Z80 at $8FED/$8FF8.
const (
	roomCentreX = 0x58
	roomCentreY = 0x68
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
	frame     uint32 // global frame counter

	// Player
	playerX     byte
	playerY     byte
	playerDir   int
	walkCounter int
	moving      bool

	// Entities
	entities   *entity.Pool
	spawnDelay int
	rand       uint16 // simple PRNG

	// Weapon
	weaponActive bool
	weaponX      int
	weaponY      int
	weaponDX     int
	weaponDY     int
	weaponFrame  int
	weaponTimer  int

	// Door system
	roomDoors map[byte][]data.RoomDoor
	doorTimer int

	// Rendering
	roomDrawn bool
	hudDirty  bool
}

// New creates a new game engine.
func New() *GameEnv {
	g := &GameEnv{
		roomDoors: data.BuildRoomDoors(),
		entities:  entity.NewPool(),
		rand:      0xACE1,
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
	g.frame = 0
	g.roomDrawn = false
	g.hudDirty = true
	g.playerDir = data.DirDown
	g.walkCounter = 0
	g.weaponActive = false
	g.spawnDelay = 32
	g.entities.Clear()

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
	case StateDead:
		g.stepDead()
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
	g.spawnDelay = 32
}

// stepPlaying handles one frame of gameplay.
func (g *GameEnv) stepPlaying(act action.Action) {
	g.frame++

	if !g.roomDrawn {
		g.clearPlayArea()
		g.drawRoom()
		g.roomDrawn = true
		g.hudDirty = true
	}

	// Player movement
	g.movePlayer(act)

	// Weapon
	if act&action.Fire != 0 && !g.weaponActive {
		g.fireWeapon()
	}
	g.updateWeapon()

	// Creatures
	g.spawnCreatures()
	g.updateCreatures()
	g.checkCreaturePlayerCollision()

	// Passive energy drain: 1 point every 16 frames (original: $0F mask check)
	if g.frame&0x0F == 0 && g.energy > 0 {
		g.energy--
		g.hudDirty = true
	}

	// Door timer
	if g.doorTimer > 0 {
		g.doorTimer--
	}

	// Render
	g.clearPlayArea()
	g.drawRoom()
	g.drawDoors()
	g.drawEntities()
	g.drawWeapon()
	g.drawPlayer()

	// Always redraw HUD — it's cheap and ensures score/energy/lives stay current
	g.clearHUDArea()
	g.drawHUD()
}

// stepDead handles the death animation.
func (g *GameEnv) stepDead() {
	g.frame++
	// Simple death pause then respawn or game over
	if g.frame%60 == 0 {
		if g.lives == 0 {
			g.state = StateGameOver
		} else {
			g.state = StatePlaying
			g.roomDrawn = false
			g.hudDirty = true
			g.playerX = 0x60
			g.playerY = 0x60
			g.energy = InitialEnergy
			g.weaponActive = false
			g.entities.Clear()
		}
	}
}

// nextRand returns a pseudo-random byte.
func (g *GameEnv) nextRand() byte {
	// LFSR-style PRNG
	g.rand ^= g.rand << 7
	g.rand ^= g.rand >> 9
	g.rand ^= g.rand << 8
	return byte(g.rand)
}

// ---------- MOVEMENT ----------

func (g *GameEnv) movePlayer(act action.Action) {
	speed := int(2)
	ra := data.RoomAttrs[g.room]
	style := data.RoomStyles[ra.Style]
	rw := int(style.Width)
	rh := int(style.Height)

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

	if g.doorTimer <= 0 && (xBlocked || yBlocked) {
		g.checkDoorExit(dx, dy, rw, rh)
	}

	if g.moving {
		g.walkCounter++
	}
}

func inWallBounds(pos, centre, dimension int) bool {
	d := pos - centre
	if d < 0 {
		d = -d
	}
	return d < dimension
}

// ---------- CREATURES ----------

func (g *GameEnv) spawnCreatures() {
	if g.spawnDelay > 0 {
		g.spawnDelay--
		return
	}
	// Original: only check spawn on every 4th frame, then 1/16 random chance.
	// At 50fps this gives roughly one spawn attempt every 1.3 seconds.
	if g.frame&0x03 != 0 {
		return
	}
	if g.nextRand()&0x0F != 0 {
		return
	}
	if g.entities.CountInRoom(g.room, entity.TypeCreature) >= entity.MaxCreaturesPerRoom {
		return
	}

	ra := data.RoomAttrs[g.room]
	style := data.RoomStyles[ra.Style]
	rw := int(style.Width) - 8
	rh := int(style.Height) - 8

	e := g.entities.Spawn()
	if e == nil {
		return
	}

	kind := int(g.nextRand() & 0x0F)
	e.Type = entity.TypeCreature
	e.Room = g.room
	e.Graphic = entity.CreatureGraphics[kind]
	e.Attr = 0x44 + byte(kind&0x07) // vary colour
	e.X = roomCentreX - rw + int(g.nextRand())%(rw*2)
	e.Y = roomCentreY - rh + int(g.nextRand())%(rh*2)
	e.Timer = byte(kind)

	// Random initial velocity
	g.setRandomVelocity(e)
}

func (g *GameEnv) setRandomVelocity(e *entity.Entity) {
	r := g.nextRand()
	e.VX = int(int8(r&0x03) - 1) // -1, 0, 1, or 2
	r = g.nextRand()
	e.VY = int(int8(r&0x03) - 1)
	if e.VX == 0 && e.VY == 0 {
		e.VX = 1
	}
}

func (g *GameEnv) updateCreatures() {
	ra := data.RoomAttrs[g.room]
	style := data.RoomStyles[ra.Style]
	rw := int(style.Width)
	rh := int(style.Height)

	g.entities.ForEachInRoom(g.room, func(e *entity.Entity) {
		if e.Type != entity.TypeCreature {
			return
		}

		// Animate
		e.Frame++

		// Move
		e.X += e.VX
		e.Y += e.VY

		// Bounce off walls
		if !inWallBounds(e.X, roomCentreX, rw) {
			e.VX = -e.VX
			e.X += e.VX * 2
		}
		if !inWallBounds(e.Y, roomCentreY, rh) {
			e.VY = -e.VY
			e.Y += e.VY * 2
		}

		// Random direction change every ~64 frames
		if e.Frame%64 == 0 {
			g.setRandomVelocity(e)
		}
	})
}

func (g *GameEnv) checkCreaturePlayerCollision() {
	const collisionDist = 12 // $0C from original

	px := int(g.playerX)
	py := int(g.playerY)

	g.entities.ForEachInRoom(g.room, func(e *entity.Entity) {
		if e.Type != entity.TypeCreature {
			return
		}
		if abs(px-e.X) < collisionDist && abs(py-e.Y) < collisionDist {
			// Original: damage_32 ($8ED7) drains $20 (32) per contact event.
			// Gate to every 8 frames (~6 hits/sec) to avoid instant death.
			if g.frame&0x07 == 0 {
				if g.energy > 32 {
					g.energy -= 32
				} else {
					g.energy = 0
				}
				g.hudDirty = true
				if g.energy == 0 {
					g.playerDeath()
				}
			}
		}
	})
}

func (g *GameEnv) playerDeath() {
	if g.lives > 0 {
		g.lives--
	}
	g.state = StateDead
	g.hudDirty = true
}

// ---------- WEAPON ----------

func (g *GameEnv) fireWeapon() {
	g.weaponActive = true
	g.weaponX = int(g.playerX)
	g.weaponY = int(g.playerY)
	g.weaponFrame = 0
	g.weaponTimer = 30 // weapon lives for 30 frames

	speed := 4
	switch g.playerDir {
	case data.DirUp:
		g.weaponDX, g.weaponDY = 0, -speed
	case data.DirDown:
		g.weaponDX, g.weaponDY = 0, speed
	case data.DirLeft:
		g.weaponDX, g.weaponDY = -speed, 0
	case data.DirRight:
		g.weaponDX, g.weaponDY = speed, 0
	}
}

func (g *GameEnv) updateWeapon() {
	if !g.weaponActive {
		return
	}

	g.weaponX += g.weaponDX
	g.weaponY += g.weaponDY
	g.weaponFrame++
	g.weaponTimer--

	// Check wall bounds
	ra := data.RoomAttrs[g.room]
	style := data.RoomStyles[ra.Style]
	if !inWallBounds(g.weaponX, roomCentreX, int(style.Width)) ||
		!inWallBounds(g.weaponY, roomCentreY, int(style.Height)) {
		g.weaponActive = false
		return
	}

	if g.weaponTimer <= 0 {
		g.weaponActive = false
		return
	}

	// Check hit on creatures
	g.entities.ForEachInRoom(g.room, func(e *entity.Entity) {
		if e.Type != entity.TypeCreature {
			return
		}
		if abs(g.weaponX-e.X) < 12 && abs(g.weaponY-e.Y) < 12 {
			// Kill creature — turn into explosion
			e.Type = entity.TypeExplosion
			e.Frame = 0
			e.Timer = 16 // pop animation duration
			e.VX = 0
			e.VY = 0
			g.weaponActive = false
			g.score += 155
			g.hudDirty = true
		}
	})
}

// ---------- DOORS ----------

func (g *GameEnv) checkDoorExit(dx, dy, rw, rh int) {
	doors := g.roomDoors[g.room]
	px := int(g.playerX)
	py := int(g.playerY)

	for _, d := range doors {
		doorX := int(d.X)
		doorY := int(d.Y)

		onTop := doorY < roomCentreY-rh
		onBottom := doorY > roomCentreY+rh
		onLeft := doorX < roomCentreX-rw
		onRight := doorX > roomCentreX+rw

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

		destRA := data.RoomAttrs[d.DestRoom]
		destStyle := data.RoomStyles[destRA.Style]
		destRW := int(destStyle.Width)
		destRH := int(destStyle.Height)

		newX := int(d.DestX)
		newY := int(d.DestY)

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
		g.spawnDelay = 32
		g.weaponActive = false
		return
	}
}

// ---------- RENDERING ----------

func (g *GameEnv) clearPlayArea() {
	for y := 0; y < 192; y++ {
		addr := screen.PixelAddr(0, y)
		for col := 0; col < 24; col++ {
			g.buf.Pixels[addr+uint16(col)] = 0
		}
	}
}

func (g *GameEnv) clearHUDArea() {
	for y := 0; y < 192; y++ {
		addr := screen.PixelAddr(192, y)
		for col := 0; col < 8; col++ {
			g.buf.Pixels[addr+uint16(col)] = 0
		}
	}
}

func (g *GameEnv) drawPlayer() {
	sprites := data.CharacterSprites(g.character)
	frame := data.AnimFrame(g.walkCounter)
	sprData := sprites[g.playerDir][frame]
	g.buf.DrawSpriteXOR(int(g.playerX), int(g.playerY), sprData)
}

func (g *GameEnv) drawEntities() {
	g.entities.ForEachInRoom(g.room, func(e *entity.Entity) {
		switch e.Type {
		case entity.TypeCreature:
			f1, f2 := data.CreatureSpriteFrames(int(e.Timer))
			var spr []byte
			if e.Frame&0x08 == 0 {
				spr = f1
			} else {
				spr = f2
			}
			g.buf.DrawSpriteXOR(e.X, e.Y, spr)

		case entity.TypeExplosion:
			e.Timer--
			spr := data.PopFrames(int(e.Frame >> 2))
			g.buf.DrawSpriteXOR(e.X, e.Y, spr)
			e.Frame++
			if e.Timer == 0 {
				e.Active = false
			}
		}
	})
}

func (g *GameEnv) drawWeapon() {
	if !g.weaponActive {
		return
	}
	// Simple weapon projectile: small cross
	x, y := g.weaponX, g.weaponY
	for d := -2; d <= 2; d++ {
		g.buf.SetPixel(x+d, y)
		g.buf.SetPixel(x, y+d)
	}
}

func (g *GameEnv) drawDoors() {
	doors := g.roomDoors[g.room]
	ra := data.RoomAttrs[g.room]
	style := data.RoomStyles[ra.Style]
	rw := int(style.Width)
	rh := int(style.Height)

	for _, d := range doors {
		dx := int(d.X)
		dy := int(d.Y)

		// Only draw doors that are on room edges (within screen area)
		// Clamp door marker to room boundary
		if dx < roomCentreX-rw {
			dx = roomCentreX - rw
		} else if dx > roomCentreX+rw {
			dx = roomCentreX + rw
		}
		if dy < roomCentreY-rh {
			dy = roomCentreY - rh
		} else if dy > roomCentreY+rh {
			dy = roomCentreY + rh
		}

		// Draw a small gap/opening marker
		for i := -3; i <= 3; i++ {
			g.buf.SetPixel(dx+i, dy-4)
			g.buf.SetPixel(dx+i, dy+4)
			g.buf.SetPixel(dx-3, dy+i)
			g.buf.SetPixel(dx+3, dy+i)
		}
	}
}

func (g *GameEnv) drawRoom() {
	if int(g.room) >= data.NumRooms {
		return
	}
	ra := data.RoomAttrs[g.room]
	if int(ra.Style) >= len(data.RoomStyles) {
		return
	}
	style := data.RoomStyles[ra.Style]

	g.buf.FillAttrArea(0, 0, 24, 24, ra.Colour)

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

// ---------- HELPERS ----------

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
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
