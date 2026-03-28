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

// InvSlot represents one inventory slot.
type InvSlot struct {
	Occupied bool
	ItemType byte // graphic ID of the item
	Name     string
}

// Item graphic IDs from the original data.
const (
	ItemKeyGreen  = 0x81
	ItemKeyRed    = 0x82 // reusing wine graphic for now
	ItemKeyCyan   = 0x83
	ItemKeyYellow = 0x84
	ItemACGKey1   = 0x8C
	ItemACGKey2   = 0x8D
	ItemACGKey3   = 0x8E
	ItemFood      = 0x50
	ItemLeaf      = 0x80
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
	lastDX      int // movement delta from last frame (for weapon direction)
	lastDY      int

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

	// Inventory (3 slots, matching original $5E30-$5E3B)
	inventory [3]InvSlot

	// Clock (hours, minutes, seconds)
	clockH, clockM, clockS byte
	clockFrame             int

	// Room tracking
	visitedRooms [20]byte // bitfield: 150 rooms
	visitPercent byte

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
	g.state = StateMenu
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
	g.inventory = [3]InvSlot{}
	g.clockH, g.clockM, g.clockS = 0, 0, 0
	g.clockFrame = 0
	g.visitedRooms = [20]byte{}
	g.visitPercent = 0

	ch := data.Characters[g.character]
	g.room = ch.StartRoom
	g.playerX = ch.StartX
	g.playerY = ch.StartY

	g.buf.Clear()
	g.spawnItems()
	g.markRoomVisited(g.room)
}

// SetCharacter sets the player character class and resets.
// Character returns the current character class.
func (g *GameEnv) Character() data.CharacterClass { return g.character }

// State returns the current game state.
func (g *GameEnv) State() GameState { return g.state }

// Buffer returns the display buffer.
func (g *GameEnv) Buffer() *screen.Buffer { return &g.buf }

// StartGame transitions from menu to playing state.
func (g *GameEnv) StartGame() {
	g.state = StatePlaying
	g.roomDrawn = false
	g.hudDirty = true
	g.entities.Clear()
	g.spawnItems()

	ch := data.Characters[g.character]
	g.room = ch.StartRoom
	g.playerX = ch.StartX
	g.playerY = ch.StartY
	g.energy = InitialEnergy
	g.lives = InitialLives
	g.score = 0
	g.frame = 0
	g.clockH, g.clockM, g.clockS = 0, 0, 0
	g.markRoomVisited(g.room)
	g.buf.Clear()
}

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

	// Item pickup
	g.checkPickup(act)

	// Passive energy drain: 1 point every 16 frames (original: $0F mask check)
	if g.frame&0x0F == 0 && g.energy > 0 {
		g.energy--
	}

	// Clock
	g.updateClock()

	// Check win condition: all 3 ACG key pieces collected
	if g.hasACGKeys() {
		g.state = StateWin
		g.score += 5000
	}

	// Door timer
	if g.doorTimer > 0 {
		g.doorTimer--
	}

	// Render
	g.clearPlayArea()
	g.drawRoom()
	g.drawDecorations()
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

	// Only check door exit when player is actively pressing into a wall
	// (dx or dy must be non-zero on the blocked axis)
	if g.doorTimer <= 0 {
		if xBlocked && dx != 0 {
			g.checkDoorExit(dx, 0, rw, rh)
		}
		if yBlocked && dy != 0 {
			g.checkDoorExit(0, dy, rw, rh)
		}
	}

	if g.moving {
		g.walkCounter++
		g.lastDX = dx
		g.lastDY = dy
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
	g.weaponTimer = 0x30 // $30 = 48 frames (original at $8181)

	// Original throw_weapon ($817C): if player is moving, weapon velocity
	// is derived from player velocity — $04 per active axis. This allows
	// diagonal firing when moving diagonally. If stationary, fires in the
	// facing direction (cardinal only).
	const speed = 4
	if g.lastDX != 0 || g.lastDY != 0 {
		// Moving: inherit direction from player velocity (diagonal possible)
		g.weaponDX = 0
		g.weaponDY = 0
		if g.lastDX > 0 {
			g.weaponDX = speed
		} else if g.lastDX < 0 {
			g.weaponDX = -speed
		}
		if g.lastDY > 0 {
			g.weaponDY = speed
		} else if g.lastDY < 0 {
			g.weaponDY = -speed
		}
	} else {
		// Stationary: fire in facing direction (cardinal only)
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
}

func (g *GameEnv) updateWeapon() {
	if !g.weaponActive {
		return
	}

	g.weaponX += g.weaponDX
	g.weaponY += g.weaponDY
	g.weaponFrame++
	g.weaponTimer--

	// Bounce off walls (original at $825D/$824B inverts velocity on wall hit)
	ra := data.RoomAttrs[g.room]
	style := data.RoomStyles[ra.Style]
	if !inWallBounds(g.weaponX, roomCentreX, int(style.Width)) {
		g.weaponDX = -g.weaponDX
		g.weaponX += g.weaponDX
	}
	if !inWallBounds(g.weaponY, roomCentreY, int(style.Height)) {
		g.weaponDY = -g.weaponDY
		g.weaponY += g.weaponDY
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

// ---------- ITEMS & INVENTORY ----------

func (g *GameEnv) spawnItems() {
	// Spawn keys in their designated rooms from the original data
	keys := []struct {
		init data.EntityInit
		name string
		typ  byte
	}{
		{data.GreenKeyInit, "GREEN KEY", ItemKeyGreen},
		{data.RedKeyInit, "RED KEY", ItemKeyRed},
		{data.CyanKeyInit, "CYAN KEY", ItemKeyCyan},
		{data.YellowKeyInit, "YELLOW KEY", ItemKeyYellow},
		{data.ACGKeyInit[0], "ACG KEY 1", ItemACGKey1},
		{data.ACGKeyInit[1], "ACG KEY 2", ItemACGKey2},
		{data.ACGKeyInit[2], "ACG KEY 3", ItemACGKey3},
	}
	for _, k := range keys {
		e := g.entities.Spawn()
		if e == nil {
			break
		}
		e.Type = entity.TypeKey
		e.Room = k.init[1]
		e.X = int(k.init[3])
		e.Y = int(k.init[4])
		e.Attr = k.init[5]
		e.Graphic = k.typ
	}

	// Spawn some food items from the food init table
	for i := 0; i < 12 && i < len(data.FoodInit); i++ {
		f := data.FoodInit[i]
		e := g.entities.Spawn()
		if e == nil {
			break
		}
		e.Type = entity.TypeFood
		e.Room = f[1]
		e.X = int(f[3])
		e.Y = int(f[4])
		e.Attr = f[5]
		e.Graphic = f[0]
	}

	// Spawn collectible items
	collectibles := []struct {
		init data.EntityInit
		name string
	}{
		{data.LeafInit, "LEAF"},
		{data.CrucifixInit, "CRUCIFIX"},
		{data.SpannerInit, "SPANNER"},
		{data.WineInit, "WINE"},
		{data.CoinInit, "COIN"},
	}
	for _, c := range collectibles {
		e := g.entities.Spawn()
		if e == nil {
			break
		}
		e.Type = entity.TypeCollectible
		e.Room = c.init[1]
		e.X = int(c.init[3])
		e.Y = int(c.init[4])
		e.Attr = c.init[5]
		e.Graphic = c.init[0]
	}
}

func (g *GameEnv) checkPickup(act action.Action) {
	if act&action.Pickup == 0 {
		return
	}

	px := int(g.playerX)
	py := int(g.playerY)
	const pickupDist = 16

	g.entities.ForEachInRoom(g.room, func(e *entity.Entity) {
		if e.Type != entity.TypeKey && e.Type != entity.TypeFood &&
			e.Type != entity.TypeCollectible {
			return
		}
		if abs(px-e.X) >= pickupDist || abs(py-e.Y) >= pickupDist {
			return
		}

		switch e.Type {
		case entity.TypeFood:
			// Food restores energy directly (no inventory slot needed)
			g.energy += 48
			if g.energy > InitialEnergy {
				g.energy = InitialEnergy
			}
			g.score += 50
			e.Active = false

		case entity.TypeCollectible:
			// Collectibles give score
			g.score += 100
			e.Active = false

		case entity.TypeKey:
			// Keys go into inventory
			slot := g.findFreeSlot()
			if slot < 0 {
				return // inventory full
			}
			g.inventory[slot] = InvSlot{
				Occupied: true,
				ItemType: e.Graphic,
				Name:     keyName(e.Graphic),
			}
			e.Active = false
		}
		g.hudDirty = true
	})
}

func (g *GameEnv) findFreeSlot() int {
	for i := range g.inventory {
		if !g.inventory[i].Occupied {
			return i
		}
	}
	return -1
}

func keyName(graphic byte) string {
	switch graphic {
	case ItemKeyGreen:
		return "GREEN"
	case ItemKeyRed:
		return "RED"
	case ItemKeyCyan:
		return "CYAN"
	case ItemKeyYellow:
		return "YELLOW"
	case ItemACGKey1:
		return "ACG-1"
	case ItemACGKey2:
		return "ACG-2"
	case ItemACGKey3:
		return "ACG-3"
	default:
		return "KEY"
	}
}

// ---------- CLOCK & ROOM TRACKING ----------

func (g *GameEnv) updateClock() {
	g.clockFrame++
	if g.clockFrame >= 50 { // 50 frames = 1 second at 50fps
		g.clockFrame = 0
		g.clockS++
		if g.clockS >= 60 {
			g.clockS = 0
			g.clockM++
			if g.clockM >= 60 {
				g.clockM = 0
				g.clockH++
			}
		}
	}
}

func (g *GameEnv) markRoomVisited(room byte) {
	idx := int(room) / 8
	bit := byte(1) << (uint(room) % 8)
	if idx < len(g.visitedRooms) {
		g.visitedRooms[idx] |= bit
	}
	// Recalculate percentage
	visited := 0
	for _, b := range g.visitedRooms {
		for b != 0 {
			visited += int(b & 1)
			b >>= 1
		}
	}
	g.visitPercent = byte(visited * 100 / data.NumRooms)
}

func (g *GameEnv) hasACGKeys() bool {
	has := [3]bool{}
	for _, slot := range g.inventory {
		if !slot.Occupied {
			continue
		}
		switch slot.ItemType {
		case ItemACGKey1:
			has[0] = true
		case ItemACGKey2:
			has[1] = true
		case ItemACGKey3:
			has[2] = true
		}
	}
	return has[0] && has[1] && has[2]
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
		g.markRoomVisited(g.room)
		return
	}
}

// ---------- RENDERING ----------

func (g *GameEnv) clearPlayArea() {
	// Clear all 6144 pixel bytes (entire display)
	for i := range g.buf.Pixels {
		g.buf.Pixels[i] = 0
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

		case entity.TypeKey, entity.TypeFood, entity.TypeCollectible:
			// Small marker: single bright pixel cluster
			x, y := e.X, e.Y
			g.buf.SetPixel(x, y)
			g.buf.SetPixel(x+1, y)
			g.buf.SetPixel(x, y+1)
			g.buf.SetPixel(x+1, y+1)
		}
	})
}

func (g *GameEnv) drawDecorations() {
	entities := data.GenRoomEntityData[int(g.room)]
	for _, pair := range entities {
		// Each entry is a 16-byte linked pair: side A (bytes 0-7) + side B (bytes 8-15).
		// The Z80 checks side A's room (byte 1) — if it doesn't match the
		// current room, it uses side B (+8 bytes). This is the XOR $08 trick.
		var e [8]byte
		if pair[1] == g.room {
			copy(e[:], pair[0:8])
		} else if pair[9] == g.room {
			copy(e[:], pair[8:16])
		} else {
			continue // neither side matches
		}

		typeID := int(e[0])
		x := int(e[3])
		y := int(e[4]) - 1 // Z80 does dec d ($9204) before rendering

		// gfx_data index is type-1 (Z80 does dec c at $9998)
		gfxIdx := typeID - 1
		if gfxIdx < 0 || gfxIdx >= 39 {
			continue
		}

		// Skip chicken sprites (gfx types 18/19 = HUD energy bar, not room decor)
		if gfxIdx == 18 || gfxIdx == 19 {
			continue
		}

		// Look up sprite data from the generated gfx_data table
		sprData, ok := data.GenDecoSprites[gfxIdx]
		if !ok || len(sprData) < 2 {
			continue
		}
		w := int(sprData[0])
		h := int(sprData[1])
		if w == 0 || h == 0 || len(sprData) < 2+w*h {
			continue
		}
		pixels := sprData[2:]
		g.buf.DrawSpriteWideOR(x, y, w, h, pixels)

		// TODO: attribute painting disabled for debugging
		_ = data.GenDecoAttrs
	}
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
	// Door gaps are rendered by clearing pixels on the room frame walls
	// where doors exist, creating visible openings.
	doors := g.roomDoors[g.room]
	ra := data.RoomAttrs[g.room]
	style := data.RoomStyles[ra.Style]
	rw := int(style.Width)
	rh := int(style.Height)

	for _, d := range doors {
		doorX := int(d.X)
		doorY := int(d.Y)
		const gapSize = 12

		onTop := doorY < roomCentreY-rh
		onBottom := doorY > roomCentreY+rh
		onLeft := doorX < roomCentreX-rw
		onRight := doorX > roomCentreX+rw

		if onTop || onBottom {
			wallY := roomCentreY - rh
			if onBottom {
				wallY = roomCentreY + rh
			}
			for px := doorX - gapSize; px <= doorX+gapSize; px++ {
				for dy := -4; dy <= 4; dy++ {
					g.buf.ClearPixel(px, wallY+dy)
				}
			}
		}
		if onLeft || onRight {
			wallX := roomCentreX - rw
			if onRight {
				wallX = roomCentreX + rw
			}
			for py := doorY - gapSize; py <= doorY+gapSize; py++ {
				for dx := -4; dx <= 4; dx++ {
					g.buf.ClearPixel(wallX+dx, py)
				}
			}
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
	// The panel character grid already contains decorative text:
	//   Row 1:  "Scroll" (chars $4F-$53)
	//   Row 7:  "TIME" (chars $59-$5C)
	//   Row 8:  ":" colon (char $5D)
	//   Row 9:  "SCORE" (chars $49-$4E)
	//   Row 18-23: Bottom rosette
	// Game code renders values and sprites into the empty interior rows.

	g.drawScrollBorder()

	// Base panel colour from room attribute (inverted)
	panelAttr := g.panelColour()
	g.buf.FillAttrArea(24, 0, 8, 24, panelAttr)

	// Attribute overrides for specific regions (from Z80 $A240):
	//   Row 7  (Y=56):  "TIME" label  → bright magenta $43
	//   Row 8  (Y=64):  time value    → bright white $47
	//   Row 9  (Y=72):  "SCORE" label → bright cyan $45
	//   Row 10 (Y=80):  score value   → bright white $47
	//   Row 11-14 (Y=88): chicken     → bright yellow $46
	//   Row 15-17 (Y=120): lives      → bright white $47
	g.buf.FillAttrArea(25, 7, 6, 1, 0x43) // TIME label: magenta
	g.buf.FillAttrArea(25, 8, 6, 1, 0x47) // time value: white
	g.buf.FillAttrArea(25, 9, 6, 1, 0x45) // SCORE label: cyan
	g.buf.FillAttrArea(25, 10, 6, 1, 0x47) // score value: white
	g.buf.FillAttrArea(25, 11, 6, 4, 0x46) // chicken: yellow
	g.buf.FillAttrArea(25, 15, 6, 3, 0x47) // lives: white

	// Time digits (row 8, Y=64)
	g.buf.DrawString(200, 64, formatClockShort(g.clockM, g.clockS))

	// Score digits (row 10, Y=80)
	g.buf.DrawString(200, 80, formatBCD(g.score))

	// Chicken energy bar (rows 11-14, Y=88-119)
	chickenRows := int(g.energy) * 30 / int(InitialEnergy)
	if chickenRows > 30 {
		chickenRows = 30
	}
	if chickenRows > 0 {
		startRow := 30 - chickenRows
		g.buf.DrawSpriteWideOR(200, 119, 6, chickenRows,
			data.ChickenFull[startRow*6:])
	}

	// Lives (rows 15-17, Y=120-143) — up to 3 character sprites
	for i := byte(0); i < g.lives && i < 3; i++ {
		lx := 200 + int(i)*16
		sprites := data.CharacterSprites(g.character)
		g.buf.DrawSpriteXOR(lx, 139, sprites[data.DirDown][0])
	}

	// Inventory slots (rows 5-6, Y=40-55)
	g.buf.FillAttrArea(25, 5, 6, 2, 0x47)
	for i, slot := range g.inventory {
		ix := 200 + i*16
		if slot.Occupied {
			g.buf.DrawString(ix, 44, slot.Name[:1])
		}
	}
}

// drawScrollBorder renders the ornate scroll border from PanelChars/PanelGrid.
func (g *GameEnv) drawScrollBorder() {
	for row := 0; row < 24; row++ {
		for col := 0; col < 8; col++ {
			charIdx := data.PanelGrid[row][col]
			if charIdx == 0 {
				continue // blank
			}
			if int(charIdx) >= len(data.PanelChars) {
				continue
			}
			px := 192 + col*8
			py := row * 8
			g.buf.DrawCharFrom(px, py, data.PanelChars[charIdx][:])
		}
	}
}

// panelColour returns the attribute byte for the scroll border.
// Original Z80: invert room colour, map blue to green.
func (g *GameEnv) panelColour() byte {
	ra := data.RoomAttrs[g.room]
	ink := (^ra.Colour) & 0x07
	if ink < 2 {
		ink = 4 // blue/black → green
	}
	return ink
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

func formatClockShort(m, s byte) string {
	return string([]byte{
		'0' + m/10, '0' + m%10, ':',
		'0' + s/10, '0' + s%10,
	})
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
