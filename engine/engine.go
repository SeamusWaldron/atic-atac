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
	playerX       byte
	playerY       byte
	playerDir     int
	walkCounter   int
	moving        bool
	lastDX        int // movement delta from last frame (for weapon direction)
	lastDY        int
	playerAnim    int // >0: spawning (height grows), <0: dying (height shrinks)
	playerAnimH   int // current visible height during animation
	playerAnimClr byte // colour cycle during spawn animation
	deathX, deathY byte // position where player died (for tombstone)

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
	roomDoors  map[byte][]data.RoomDoor
	doorTimer  int
	doorStates map[uint32]bool // key = (room<<16 | entityIdx), value = true=open, false=closed
	doorCycleTimer int         // Z80 $5E2E: counts down from 94, toggles a door when 0

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
	// Z80: creature_delay starts at 0, last_creat_room starts at 0.
	// Room 0 matches last_creat_room so no 32-frame delay is applied.
	// Creatures spawn via 1/16 random chance immediately.
	g.spawnDelay = 0
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
	g.state = StateSpawning // start with materialise animation
	g.playerAnimH = 0
	g.playerAnimClr = 1
	g.roomDrawn = false
	g.hudDirty = true
	g.entities.Clear()
	g.spawnItems()
	g.randomiseDoorStates()

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
	case StateDying:
		g.stepDying()
	case StateSpawning:
		g.stepSpawning()
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

	// Food auto-consumption on contact (Z80 h_food at $8C63)
	g.checkFoodPickup()
	// Secret passage check (Z80 h_barrel/$9421, h_bookcase/$9428, h_clock/$942F)
	g.checkSecretPassage()
	// Key/collectible pickup on Enter key
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

	// Door transition cooldown timer
	if g.doorTimer > 0 {
		g.doorTimer--
	}

	// Door open/close cycling (Z80 $5E2E timer, 94 frames)
	g.cycleDoors()

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

// stepDying handles the death shrink animation.
// Z80 h_death at $8D45: player height decreases each frame (3/4 rate).
func (g *GameEnv) stepDying() {
	g.frame++

	// Shrink every other frame (Z80 uses 3/4 rate)
	if g.frame&0x01 == 0 {
		g.playerAnimH--
	}

	// Render: room + decorations + shrinking player
	g.clearPlayArea()
	g.drawRoom()
	g.drawDecorations()
	g.drawEntities()

	// Draw player at shrinking height
	if g.playerAnimH > 0 {
		sprites := data.CharacterSprites(g.character)
		sprData := sprites[g.playerDir][0]
		fullH := int(sprData[0])
		// Draw only the bottom 'playerAnimH' rows of the sprite
		if g.playerAnimH <= fullH {
			partialData := make([]byte, 1+g.playerAnimH*2)
			partialData[0] = byte(g.playerAnimH)
			copy(partialData[1:], sprData[1:1+g.playerAnimH*2])
			g.buf.DrawSpriteXOR(int(g.playerX), int(g.playerY), partialData)
			g.paintEntityAttr(int(g.playerX), int(g.playerY), 2, g.playerAnimH, 0x47)
		}
	}

	// When fully shrunk: place tombstone and transition
	if g.playerAnimH <= 0 {
		// Place tombstone at death position (graphic $8F, attr $45 = bright cyan)
		tombstoneGfx := byte(0x8F)
		flatIdx := int(tombstoneGfx) - 1
		group := flatIdx / 4
		frame := flatIdx % 4
		if group < len(data.GenSpriteTable) {
			addr := data.GenSpriteTable[group][frame]
			if spr := data.GenMenuIcons[addr]; spr != nil {
				g.buf.DrawSpriteXOR(int(g.deathX), int(g.deathY), spr)
				g.paintEntityAttr(int(g.deathX), int(g.deathY), 2, int(spr[0]), 0x45)
			}
		}

		if g.lives == 0 {
			g.state = StateGameOver
		} else {
			g.state = StateDead // brief pause before respawn
		}
	}

	g.clearHUDArea()
	g.drawHUD()
}

// stepDead handles the pause between death and respawn.
func (g *GameEnv) stepDead() {
	g.frame++
	if g.frame%30 == 0 { // half-second pause
		// Start spawn materialise animation
		g.state = StateSpawning
		g.roomDrawn = false
		g.hudDirty = true
		g.playerX = 0x60
		g.playerY = 0x60
		g.energy = InitialEnergy
		g.weaponActive = false
		g.playerAnimH = 0 // start at zero height, grow upward
		g.playerAnimClr = 1 // start colour cycle
	}
}

// stepSpawning handles the materialise animation.
// Z80 h_player_appear at $8CB7: height grows, colour cycles.
func (g *GameEnv) stepSpawning() {
	g.frame++

	sprites := data.CharacterSprites(g.character)
	sprData := sprites[data.DirDown][0]
	fullH := int(sprData[0])

	// Grow height every other frame
	if g.frame&0x01 == 0 {
		g.playerAnimH++
	}

	// Cycle colour every 4 frames (Z80: and $03)
	if g.frame&0x03 == 0 {
		g.playerAnimClr++
		if g.playerAnimClr > 7 {
			g.playerAnimClr = 1
		}
	}

	// Render
	g.clearPlayArea()
	g.drawRoom()
	g.drawDecorations()
	g.drawEntities()

	// Draw player at growing height with cycling colour
	if g.playerAnimH > 0 && g.playerAnimH <= fullH {
		partialData := make([]byte, 1+g.playerAnimH*2)
		partialData[0] = byte(g.playerAnimH)
		copy(partialData[1:], sprData[1:1+g.playerAnimH*2])
		g.buf.DrawSpriteXOR(int(g.playerX), int(g.playerY), partialData)
		// Colour: bright + cycling ink
		attr := byte(0x40) | g.playerAnimClr
		g.paintEntityAttr(int(g.playerX), int(g.playerY), 2, g.playerAnimH, attr)
	}

	// When fully grown: switch to playing
	if g.playerAnimH >= fullH {
		g.state = StatePlaying
		g.playerDir = data.DirDown
		g.roomDrawn = false
		g.spawnDelay = 32
	}

	g.clearHUDArea()
	g.drawHUD()
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
		// Check decoration collision on X axis
		if g.checkDecoCollision(newX, int(g.playerY)) {
			xBlocked = true
		} else {
			g.playerX = byte(newX)
		}
	}

	newY := int(g.playerY) + dy
	yBlocked := !inWallBounds(newY, roomCentreY, rh)
	if !yBlocked {
		// Check decoration collision on Y axis
		if g.checkDecoCollision(int(g.playerX), newY) {
			yBlocked = true
		} else {
			g.playerY = byte(newY)
		}
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

// checkDecoCollision returns true if position (px, py) overlaps with any
// solid decoration in the current room. Z80 chk_decor_move at $900A.
// Doors are excluded (bit 3 of flags = passable). Wall items excluded.
func (g *GameEnv) checkDecoCollision(px, py int) bool {
	entities := data.GenRoomEntityData[int(g.room)]
	for _, pair := range entities {
		var e [8]byte
		if pair[1] == g.room {
			copy(e[:], pair[0:8])
		} else if pair[9] == g.room {
			copy(e[:], pair[8:16])
		} else {
			continue
		}

		typeID := e[0]

		// Skip doors (types $01-$0F and $20-$23) — player walks through them
		if typeID >= 0x01 && typeID <= 0x0F {
			continue
		}
		if typeID >= 0x20 && typeID <= 0x23 {
			continue
		}

		// Skip wall-mounted items (shields, trophies) — they don't block
		// These are small items on the outer frame, not floor obstacles
		if typeID == 0x1B || typeID == 0x1C { // shields
			continue
		}
		if typeID == 0x15 || typeID == 0x16 { // trophies
			continue
		}
		if typeID == 0x25 || typeID == 0x26 || typeID == 0x27 { // pictures
			continue
		}

		// Get sprite dimensions for collision box
		gfxIdx := int(typeID) - 1
		if gfxIdx < 0 || gfxIdx >= 39 {
			continue
		}
		sprData, ok := data.GenDecoSprites[gfxIdx]
		if !ok || len(sprData) < 2 {
			continue
		}
		w := int(sprData[0]) * 8 // width in pixels
		h := int(sprData[1])     // height in pixels

		// Decoration position (entity Y = bottom of sprite)
		ex := int(e[3])
		ey := int(e[4])

		// Collision box: player is roughly 16x18 pixels
		const playerW = 8
		const playerH = 8

		// Check overlap: decoration occupies (ex, ey-h+1) to (ex+w-1, ey)
		if px+playerW > ex && px-playerW < ex+w &&
			py > ey-h && py-playerH < ey {
			return true
		}
	}
	return false
}

// randomiseDoorStates sets initial door open/closed states.
// Z80 randomise_doors at $94F5: ~56% of paired doors get toggled.
func (g *GameEnv) randomiseDoorStates() {
	g.doorStates = make(map[uint32]bool)
	g.doorCycleTimer = 94

	// For each room's entity pairs, if it's a normal door (type $01-$02),
	// randomly set it to open (true) or closed (false).
	// Default: all doors start open. ~56% get toggled to closed.
	for room, entities := range data.GenRoomEntityData {
		for i, pair := range entities {
			var e [8]byte
			if pair[1] == byte(room) {
				copy(e[:], pair[0:8])
			} else if pair[9] == byte(room) {
				copy(e[:], pair[8:16])
			} else {
				continue
			}
			typeID := e[0]
			// Only normal doors toggle (types $01-$02, $20-$23)
			// Locked doors ($08-$0F) stay locked
			if typeID == 0x01 || typeID == 0x02 {
				key := uint32(room)<<16 | uint32(i)
				// ~56% chance of being closed (Z80 uses ROM data, ~43% >= $70)
				if g.nextRand() > 0x70 {
					g.doorStates[key] = false // closed
				} else {
					g.doorStates[key] = true // open
				}
			}
		}
	}
}

// isDoorOpen checks if a specific door in a room is open.
func (g *GameEnv) isDoorOpen(room byte, entityIdx int) bool {
	key := uint32(room)<<16 | uint32(entityIdx)
	open, exists := g.doorStates[key]
	if !exists {
		return true // default to open if not tracked
	}
	return open
}

// cycleDoors toggles a random door in the current room every 94 frames.
// Z80: door timer $5E2E counts down from $5E (94), toggles on zero.
func (g *GameEnv) cycleDoors() {
	g.doorCycleTimer--
	if g.doorCycleTimer > 0 {
		return
	}
	g.doorCycleTimer = 94 // reset

	// Find a normal door in the current room and toggle it
	entities := data.GenRoomEntityData[int(g.room)]
	for i, pair := range entities {
		var e [8]byte
		if pair[1] == g.room {
			copy(e[:], pair[0:8])
		} else if pair[9] == g.room {
			copy(e[:], pair[8:16])
		} else {
			continue
		}
		typeID := e[0]
		if typeID == 0x01 || typeID == 0x02 {
			key := uint32(g.room)<<16 | uint32(i)
			g.doorStates[key] = !g.doorStates[key]
			g.roomDrawn = false // force redraw to show new door state
			return
		}
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
	// Z80 chk_creatures at $83EA: 1/16 random chance per frame (no frame gating).
	// Uses R register AND $0F — spawn if result is 0.
	if g.nextRand()&0x0F != 0 {
		return
	}
	// Count both active creatures AND spawning sparkles toward the limit
	creatureCount := g.entities.CountInRoom(g.room, entity.TypeCreature) +
		g.entities.CountInRoom(g.room, entity.TypeSpawning)
	if creatureCount >= entity.MaxCreaturesPerRoom {
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
	// Spawn as sparkle first (Z80 h_sparkles at $85F7)
	e.Type = entity.TypeSpawning
	e.Room = g.room
	e.Graphic = entity.CreatureGraphics[kind]
	// Creature colours from Z80 handler routines
	creatureColours := [16]byte{
		0x46, 0x46, 0x42, 0x42, // Spider=yellow, Spikey=yellow, Bat=red, Bat=red
		0x43, 0x43, 0x42, 0x42, // Witch=magenta, Witch=magenta, Monk=red, Monk=red
		0x46, 0x46, 0x44, 0x46, // Spider=yellow, Spikey=yellow, Blob=green, Ghoul=yellow
		0x46, 0x45, 0x47, 0x42, // Pumpkin=yellow, Ghostlet=cyan, Ghost=white, Batlet=red
	}
	e.Attr = creatureColours[kind]
	e.X = roomCentreX - rw + int(g.nextRand())%(rw*2)
	e.Y = roomCentreY - rh + int(g.nextRand())%(rh*2)
	e.Timer = byte(kind)
	e.Frame = 16 // sparkle for 16 frames before becoming creature

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
	// Start death shrink animation (Z80 h_death at $8D45)
	g.state = StateDying
	g.deathX = g.playerX
	g.deathY = g.playerY
	sprites := data.CharacterSprites(g.character)
	sprData := sprites[g.playerDir][0]
	g.playerAnimH = int(sprData[0]) // start at full height
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
	// --- ACG key pieces: randomised to one of 8 room sets ---
	// Z80 place_key_pieces at $94B6: (FRAMES + counter_low) & 7 selects set
	acgRoomTable := [8][3]byte{
		{0x81, 0x45, 0x7C}, {0x85, 0x49, 0x2B}, {0x6A, 0x3B, 0x7C}, {0x69, 0x71, 0x2B},
		{0x67, 0x85, 0x7C}, {0x68, 0x7F, 0x2B}, {0x4D, 0x73, 0x7C}, {0x17, 0x10, 0x2B},
	}
	acgSet := int(g.nextRand()) & 0x07
	for i, k := range data.ACGKeyInit {
		e := g.entities.Spawn()
		if e == nil {
			break
		}
		e.Type = entity.TypeKey
		e.Room = acgRoomTable[acgSet][0]
		e.X = int(acgRoomTable[acgSet][1])
		e.Y = int(acgRoomTable[acgSet][2])
		e.Attr = k[5]
		e.Graphic = k[0]
		_ = i
	}

	// --- Coloured keys: each randomised to one of 8 rooms ---
	// Z80 set_key_positions at $98D2
	greenRooms := [8]byte{0x05, 0x06, 0x07, 0x6D, 0x25, 0x24, 0x23, 0x22}
	redRooms := [8]byte{0x17, 0x13, 0x09, 0x0D, 0x89, 0x87, 0x80, 0x85}
	cyanRooms := [8]byte{0x53, 0x8F, 0x41, 0x94, 0x33, 0x91, 0x39, 0x4C}

	colourKeys := []struct {
		init  data.EntityInit
		rooms *[8]byte
	}{
		{data.GreenKeyInit, &greenRooms},
		{data.RedKeyInit, &redRooms},
		{data.CyanKeyInit, &cyanRooms},
	}
	for _, ck := range colourKeys {
		e := g.entities.Spawn()
		if e == nil {
			break
		}
		e.Type = entity.TypeKey
		e.Room = ck.rooms[int(g.nextRand())&0x07]
		e.X = int(ck.init[3])
		e.Y = int(ck.init[4])
		e.Attr = ck.init[5]
		e.Graphic = ck.init[0]
	}

	// Yellow key: fixed room (no randomisation)
	if e := g.entities.Spawn(); e != nil {
		e.Type = entity.TypeKey
		e.Room = data.YellowKeyInit[1]
		e.X = int(data.YellowKeyInit[3])
		e.Y = int(data.YellowKeyInit[4])
		e.Attr = data.YellowKeyInit[5]
		e.Graphic = data.YellowKeyInit[0]
	}

	// --- ALL 48 food items ---
	for i := 0; i < len(data.FoodInit); i++ {
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

	// --- ALL collectible items (11 total) ---
	allCollectibles := []data.EntityInit{
		data.LeafInit,
		data.CrucifixInit,
		data.SpannerInit,
		data.WineInit,
		data.CoinInit,
	}
	// Add the remaining collectibles from gen_items if available
	for i := 0; i < len(data.GenCollectibleInit); i++ {
		init := data.GenCollectibleInit[i]
		if init[0] != 0 {
			allCollectibles = append(allCollectibles, init)
		}
	}
	// Deduplicate (GenCollectibleInit includes the first 5 already)
	seen := make(map[byte]bool)
	for _, c := range allCollectibles {
		if seen[c[0]] {
			continue
		}
		seen[c[0]] = true
		e := g.entities.Spawn()
		if e == nil {
			break
		}
		e.Type = entity.TypeCollectible
		e.Room = c[1]
		e.X = int(c[3])
		e.Y = int(c[4])
		e.Attr = c[5]
		e.Graphic = c[0]
	}
}

// checkFoodPickup auto-consumes food on contact — no key press needed.
// Z80 h_food at $8C63: adds $40 (64) energy, caps at $F0 (240).
func (g *GameEnv) checkFoodPickup() {
	px := int(g.playerX)
	py := int(g.playerY)
	const touchDist = 12 // same as creature collision distance

	g.entities.ForEachInRoom(g.room, func(e *entity.Entity) {
		if e.Type != entity.TypeFood {
			return
		}
		if abs(px-e.X) >= touchDist || abs(py-e.Y) >= touchDist {
			return
		}
		// Auto-consume: +$40 (64) energy, cap at $F0 (240)
		g.energy += 64
		if g.energy > InitialEnergy {
			g.energy = InitialEnergy
		}
		e.Active = false
		g.hudDirty = true
	})
}

// checkPickup handles key/collectible pickup on Enter key press.
func (g *GameEnv) checkPickup(act action.Action) {
	if act&action.Pickup == 0 {
		return
	}

	px := int(g.playerX)
	py := int(g.playerY)
	const pickupDist = 16

	g.entities.ForEachInRoom(g.room, func(e *entity.Entity) {
		if e.Type != entity.TypeKey && e.Type != entity.TypeCollectible {
			return
		}
		if abs(px-e.X) >= pickupDist || abs(py-e.Y) >= pickupDist {
			return
		}

		switch e.Type {
		case entity.TypeCollectible:
			g.score += 100
			e.Active = false

		case entity.TypeKey:
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

// checkSecretPassage checks if the player is overlapping a character-specific
// secret passage decoration and triggers room transition if so.
// Z80: h_clock ($942F) = Knight, h_bookcase ($9428) = Wizard, h_barrel ($9421) = Serf.
func (g *GameEnv) checkSecretPassage() {
	if g.doorTimer > 0 {
		return // cooldown from recent door/passage transition
	}

	// Map character class to passage entity type
	var passageType byte
	switch g.character {
	case data.Knight:
		passageType = 0x10 // clock
	case data.Wizard:
		passageType = 0x17 // bookcase — wait, need to verify
	case data.Serf:
		passageType = 0x1A // barrel — need to verify
	default:
		return
	}
	// Z80 handler_table2: type $10 entry 1 = $942F (clock/knight)
	// Type $14 entry 1 = $9428... actually let me use the actual types
	// from the handler table analysis:
	// h_barrel at $9421 for type $1A (handler_table2 row $18, entry 2)
	// h_bookcase at $9428 for type $17 (handler_table2 row $14, entry 3)
	// h_clock at $942F for type $10 (handler_table2 row $10, entry 0)
	// These types need to be checked against the entity data in the room.

	px := int(g.playerX)
	py := int(g.playerY)
	const passageDist = 12

	entities := data.GenRoomEntityData[int(g.room)]
	for _, pair := range entities {
		// Check both sides of the pair for matching passage type in current room
		for side := 0; side < 2; side++ {
			var e [8]byte
			if side == 0 {
				copy(e[:], pair[0:8])
			} else {
				copy(e[:], pair[8:16])
			}
			if e[1] != g.room {
				continue
			}
			if e[0] != passageType {
				continue
			}

			// Check proximity
			ex := int(e[3])
			ey := int(e[4])
			if abs(px-ex) >= passageDist || abs(py-ey) >= passageDist {
				continue
			}

			// Found matching passage — get destination from the other side
			var dest [8]byte
			if side == 0 {
				copy(dest[:], pair[8:16])
			} else {
				copy(dest[:], pair[0:8])
			}

			destRoom := dest[1]
			destX := int(dest[3])
			destY := int(dest[4])

			// Clamp destination inside new room bounds
			destRA := data.RoomAttrs[destRoom]
			destStyle := data.RoomStyles[destRA.Style]
			destRW := int(destStyle.Width)
			destRH := int(destStyle.Height)
			if destX <= roomCentreX-destRW {
				destX = roomCentreX - destRW + 4
			} else if destX >= roomCentreX+destRW {
				destX = roomCentreX + destRW - 4
			}
			if destY <= roomCentreY-destRH {
				destY = roomCentreY - destRH + 4
			} else if destY >= roomCentreY+destRH {
				destY = roomCentreY + destRH - 4
			}

			g.room = destRoom
			g.playerX = byte(destX)
			g.playerY = byte(destY)
			g.roomDrawn = false
			g.hudDirty = true
			g.doorTimer = 25
			g.spawnDelay = 32
			g.weaponActive = false
			g.markRoomVisited(g.room)
			return
		}
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

		// Check if this door is closed — find the matching entity pair
		// and check its state. Closed doors block passage.
		if d.Type == 0x01 || d.Type == 0x02 {
			doorClosed := false
			entities := data.GenRoomEntityData[int(g.room)]
			for ei, pair := range entities {
				var e [8]byte
				if pair[1] == g.room {
					copy(e[:], pair[0:8])
				} else if pair[9] == g.room {
					copy(e[:], pair[8:16])
				} else {
					continue
				}
				if (e[0] == 0x01 || e[0] == 0x02) &&
					int(e[3]) == int(d.X) && int(e[4]) == int(d.Y) {
					if !g.isDoorOpen(g.room, ei) {
						doorClosed = true
					}
					break
				}
			}
			if doorClosed {
				continue // can't pass through closed door
			}
		}

		// Locked door check: types $08-$0F require matching colour key
		// Z80 check_key_colour at $9222: door type & $03 = colour index
		// Key attrs table at $925C: [$42=red, $44=green, $45=cyan, $46=yellow]
		if d.Type >= 0x08 && d.Type <= 0x0F {
			// Z80 key colour table at $925C: [red, green, cyan, yellow]
			keyNames := [4]string{"RED", "GREEN", "CYAN", "YELLOW"}
			requiredKey := keyNames[d.Type&0x03]
			keySlot := -1
			for si, slot := range g.inventory {
				if slot.Occupied && slot.Name == requiredKey {
					keySlot = si
					break
				}
			}
			if keySlot < 0 {
				continue // locked — no matching key
			}
			// Consume the key
			g.inventory[keySlot] = InvSlot{}
			g.hudDirty = true
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

	// Paint player attribute colour — bright white
	sprH := int(sprData[0])
	g.paintEntityAttr(int(g.playerX), int(g.playerY), 2, sprH, 0x47)
	// Also need to restore room colour around the player's previous position
	// but for now just paint the sprite's cells
}

// paintEntityAttr paints a single attribute colour over the cells an entity
// sprite covers. Matches Z80 set_entity_attrs at $A00E.
// Entity sprites draw UPWARD from Y, so attr cells go from Y upward.
// Z80 uses $5E10 (width_bytes = 2 or 3) based on sub-byte alignment.
func (g *GameEnv) paintEntityAttr(x, y, widthCells, heightPx int, attr byte) {
	if attr == 0 {
		return
	}
	// Width: 2 cells if byte-aligned, 3 if sprite straddles a cell boundary
	// (matching Z80 $5E10 which is set to 2 or 3 by sprite draw setup)
	actualWidth := widthCells
	if x&7 != 0 {
		actualWidth = widthCells + 1
	}

	// Height: Z80 formula from $A02E: height >> 2, inc, >> 1, & $1F, inc
	// This gives a tighter cell count than ceil(height/8)
	attrH := ((heightPx >> 2) + 1) >> 1
	attrH = (attrH & 0x1F) + 1

	startCol := x >> 3
	startRow := y >> 3

	for r := 0; r < attrH; r++ {
		for c := 0; c < actualWidth; c++ {
			cellCol := startCol + c
			cellRow := startRow - r
			if cellCol >= 0 && cellCol < 24 && cellRow >= 0 && cellRow < 24 {
				g.buf.Attrs[cellRow*32+cellCol] = attr
			}
		}
	}
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
			g.paintEntityAttr(e.X, e.Y, 2, int(spr[0]), e.Attr)

		case entity.TypeExplosion:
			e.Timer--
			spr := data.PopFrames(int(e.Frame >> 2))
			g.buf.DrawSpriteXOR(e.X, e.Y, spr)
			e.Frame++
			if e.Timer == 0 {
				e.Active = false
			}

		case entity.TypeSpawning:
			// Sparkle animation: graphics $58-$5B, 4 frames
			// Z80: ix+$0E counts down, and $03 selects frame, add $58
			sparkleFrame := e.Frame & 0x03
			sparkleGfx := byte(0x58) + sparkleFrame
			// Look up sparkle sprite via (graphicID-1) indexing
			flatIdx := int(sparkleGfx) - 1
			group := flatIdx / 4
			frame := flatIdx % 4
			if group < len(data.GenSpriteTable) {
				addr := data.GenSpriteTable[group][frame]
				if spr := data.GenMenuIcons[addr]; spr != nil {
					g.buf.DrawSpriteXOR(e.X, e.Y, spr)
					g.paintEntityAttr(e.X, e.Y, 2, int(spr[0]), e.Attr)
				}
			}
			e.Frame--
			if e.Frame == 0 {
				// Convert to actual creature
				e.Type = entity.TypeCreature
				e.Frame = 0
			}

		case entity.TypeKey, entity.TypeFood, entity.TypeCollectible:
			// Draw item sprite using graphic ID from entity data.
			// Z80 uses (graphicID-1) indexing into sprite_table.
			graphicID := e.Graphic
			if graphicID == 0 {
				break
			}
			flatIdx := int(graphicID) - 1
			group := flatIdx / 4
			frame := flatIdx % 4
			if group < len(data.GenSpriteTable) {
				addr := data.GenSpriteTable[group][frame]
				if spr := data.GenMenuIcons[addr]; spr != nil {
					g.buf.DrawSpriteXOR(e.X, e.Y, spr)
					g.paintEntityAttr(e.X, e.Y, 2, int(spr[0]), e.Attr)
				}
			}
		}
	})
}

// clearDoorFrameLines erases pixels where door sprites sit on the room frame,
// so the frame line doesn't show through the door base.
func (g *GameEnv) clearDoorFrameLines() {
	ra := data.RoomAttrs[g.room]
	style := data.RoomStyles[ra.Style]
	rw := int(style.Width)
	rh := int(style.Height)

	entities := data.GenRoomEntityData[int(g.room)]
	for _, pair := range entities {
		var e [8]byte
		if pair[1] == g.room {
			copy(e[:], pair[0:8])
		} else if pair[9] == g.room {
			copy(e[:], pair[8:16])
		} else {
			continue
		}

		typeID := int(e[0])
		// Only door types need frame clearing
		if typeID < 0x01 || typeID > 0x0F {
			continue
		}

		x := int(e[3])
		y := int(e[4]) // raw Y — draw_rot_obj reloads without dec d

		onTop := y < roomCentreY-rh
		onBottom := y > roomCentreY+rh
		onLeft := x < roomCentreX-rw
		onRight := x > roomCentreX+rw

		// Clear the area the door sprite will occupy.
		// Base door sprite is 4 bytes (32px) wide × 24 rows.
		// Rotated doors (modes 2,3,6,7) become 24px wide × 32 rows.
		mode := (int(e[5]) >> 5) & 0x07
		sprW := 32
		sprH := 24
		if mode >= 2 && mode <= 3 || mode >= 6 && mode <= 7 {
			sprW = 24
			sprH = 32
		}
		if onTop || onBottom || onLeft || onRight {
			for py := y - sprH + 1; py <= y; py++ {
				for px := x; px < x+sprW; px++ {
					g.buf.ClearPixel(px, py)
				}
			}
		}
	}
}

func (g *GameEnv) drawDecorations() {
	entities := data.GenRoomEntityData[int(g.room)]
	for ei, pair := range entities {
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
		// Z80: h_room_item does dec d for ATTRS only ($9204). draw_rot_obj
		// at $9213 RELOADS raw Y from (ix+$04) for PIXEL rendering.
		y := int(e[4])

		// gfx_data index is type-1 (Z80 does dec c at $9998)
		gfxIdx := typeID - 1
		if gfxIdx < 0 || gfxIdx >= 39 {
			continue
		}

		// For normal doors: swap to closed sprite if door is closed
		// Open door = gfxIdx 1 (door_frame), Closed door = gfxIdx 31 (door_shut)
		if (typeID == 0x01 || typeID == 0x02) && !g.isDoorOpen(g.room, ei) {
			gfxIdx = 31 - 1 // door_shut sprite (type $20, gfxIdx=31)
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

		// Clip decorations that would overflow into HUD panel (X >= 192)
		if x >= 192 {
			continue
		}

		// Pixel rotation mode from attr byte bits 7-5.
		// From Z80: h_room_item ($91FE) calls $9980 for attrs, then falls through
		// to draw_rot_obj ($9213) for pixels when room not yet drawn ($9212: ret nz).
		// draw_rot_obj dispatches through $9970 pixel rotation table.
		// So ALL entity types rendered via h_room_item get pixel rotation.
		mode := (int(e[5]) >> 5) & 0x07
		// Z80 blend mode from attr bits 1-0: 0=overwrite, 1=OR, 2=XOR
		// Door sprites use overwrite (NOP) to erase frame lines beneath.
		useOverwrite := (e[5] & 0x03) == 0
		drawDecoSprite(&g.buf, x, y, w, h, pixels, mode, useOverwrite)

		// Attribute painting: h_room_item uses Y-1 for attrs (dec d at $9204).
		// xy_to_attr maps pixel (X, Y-1) to character cell (X/8, (Y-1)/8).
		// Attr data paints UPWARD from that cell (sbc hl, $0020 in Z80).
		attrData, hasAttr := data.GenDecoAttrs[gfxIdx]
		if hasAttr && len(attrData) >= 2 {
			aw := int(attrData[0])
			ah := int(attrData[1])
			if aw > 0 && ah > 0 && len(attrData) >= 2+aw*ah {
				attrY := int(e[4]) - 1 // dec d for attrs only
				startCol := x >> 3
				startRow := attrY >> 3
				roomAttr := data.RoomAttrs[g.room].Colour

				paintDecoAttrs(&g.buf, startCol, startRow, aw, ah,
				attrData[2:], mode, roomAttr)
			}
		}
	}
}

// paintDecoAttrs paints a decoration's per-cell attribute grid.
// Starts at (startCol, startRow) and paints UPWARD (decreasing row).
// Mode controls iteration order matching the Z80 draw_attr_0 through draw_attr_7.
//
// For non-rotation modes (0,1,4,5): outer=ah rows, inner=aw columns.
// For rotation modes (2,3,6,7): outer=aw rows, inner=ah columns.
// Screen mapping: inner increments column, outer decrements row (upward).
func paintDecoAttrs(buf *screen.Buffer, startCol, startRow, aw, ah int,
	attrValues []byte, mode int, roomAttr byte) {

	// Rotation modes swap the loop dimensions
	outerCount := ah
	innerCount := aw
	if mode == 2 || mode == 3 || mode == 6 || mode == 7 {
		outerCount = aw
		innerCount = ah
	}

	for outer := 0; outer < outerCount; outer++ {
		for inner := 0; inner < innerCount; inner++ {
			// Map screen cell to source data index based on mode.
			// The source data is aw columns × ah rows.
			// Screen cell is at (startCol+inner, startRow-outer).
			var srcCol, srcRow int
			switch mode {
			case 0: // Normal
				srcCol = inner
				srcRow = outer
			case 1: // H-flip
				srcCol = aw - 1 - inner
				srcRow = outer
			case 4: // 180°
				srcCol = inner
				srcRow = ah - 1 - outer
			case 5: // 180° + h-flip
				srcCol = aw - 1 - inner
				srcRow = ah - 1 - outer
			case 2: // 90° CW rotation
				srcCol = aw - 1 - outer
				srcRow = inner
			case 3: // 90° CCW rotation (RIGHT wall)
				srcCol = outer
				srcRow = inner
			case 6: // 270° CW
				srcCol = aw - 1 - outer
				srcRow = ah - 1 - inner
			case 7: // LEFT wall: flip column axis compared to mode 3
				srcCol = aw - 1 - outer
				srcRow = ah - 1 - inner
			default:
				srcCol = inner
				srcRow = outer
			}

			dataIdx := srcRow*aw + srcCol
			if dataIdx < 0 || dataIdx >= len(attrValues) {
				continue
			}

			av := attrValues[dataIdx]
			if av == 0x00 {
				continue // skip transparent
			}
			if av == 0xFF {
				av = roomAttr
			}

			cellCol := startCol + inner
			cellRow := startRow - outer
			if cellCol >= 0 && cellCol < 24 && cellRow >= 0 && cellRow < 24 {
				buf.Attrs[cellRow*32+cellCol] = av
			}
		}
	}
}

// drawDecoSprite renders a decoration sprite with orientation mode 0-7.
// Mode is derived from bits 7-5 of the entity's attr byte.
//
// Mode 0: Normal (upward from Y, left-to-right)
// Mode 1: Horizontal flip (upward, right-to-left bytes)
// Mode 2: 90° CW + h-flip (rotated)
// Mode 3: 90° CCW (rotated)
// Mode 4: 180° (downward from Y, left-to-right = vertical flip)
// Mode 5: 180° + h-flip (downward, right-to-left)
// Mode 6: 90° CW + h-flip variant
// Mode 7: 90° CCW + flip variant
func drawWide(buf *screen.Buffer, x, y, w, h int, pixels []byte, overwrite bool) {
	if overwrite {
		buf.DrawSpriteWideOverwrite(x, y, w, h, pixels)
	} else {
		buf.DrawSpriteWideOR(x, y, w, h, pixels)
	}
}

func drawDecoSprite(buf *screen.Buffer, x, y, w, h int, pixels []byte, mode int, overwrite bool) {
	switch mode {
	case 0: // Normal: draw upward from Y, left-to-right
		drawWide(buf, x, y, w, h, pixels, overwrite)

	case 1: // Horizontal flip: upward from Y, reverse bytes per row
		flipped := make([]byte, len(pixels))
		for row := 0; row < h; row++ {
			for col := 0; col < w; col++ {
				flipped[row*w+col] = reverseBits(pixels[row*w+(w-1-col)])
			}
		}
		drawWide(buf, x, y, w, h, flipped, overwrite)

	case 2: // 90° CW
		ow, oh, op := rotateCW(w, h, pixels)
		drawWide(buf, x, y, ow, oh, op, overwrite)

	case 3: // 90° CCW + horizontal flip (right wall doors)
		ow, oh, op := rotateCCW(w, h, pixels)
		// Horizontal flip: reverse bits and byte order per row
		for row := 0; row < oh; row++ {
			for col := 0; col < ow/2; col++ {
				ri := ow - 1 - col
				op[row*ow+col], op[row*ow+ri] = reverseBits(op[row*ow+ri]), reverseBits(op[row*ow+col])
			}
			if ow%2 == 1 {
				mid := ow / 2
				op[row*ow+mid] = reverseBits(op[row*ow+mid])
			}
		}
		drawWide(buf, x, y, ow, oh, op, overwrite)

	case 4: // 180°: draw upward from Y, rows in reverse order
		reversed := make([]byte, len(pixels))
		for row := 0; row < h; row++ {
			copy(reversed[row*w:(row+1)*w], pixels[(h-1-row)*w:(h-row)*w])
		}
		drawWide(buf, x, y, w, h, reversed, overwrite)

	case 5: // 180° + h-flip: rows reversed AND bytes reversed
		flipped := make([]byte, len(pixels))
		for row := 0; row < h; row++ {
			srcRow := h - 1 - row
			for col := 0; col < w; col++ {
				flipped[row*w+col] = reverseBits(pixels[srcRow*w+(w-1-col)])
			}
		}
		drawWide(buf, x, y, w, h, flipped, overwrite)

	case 6: // 270° CW = 90° CW + 180°
		ow, oh, op := rotateCW(w, h, pixels)
		flipped := make([]byte, len(op))
		for row := 0; row < oh; row++ {
			copy(flipped[row*ow:(row+1)*ow], op[(oh-1-row)*ow:(oh-row)*ow])
		}
		drawWide(buf, x, y, ow, oh, flipped, overwrite)

	case 7: // 270° CCW = 90° CCW + 180°
		ow, oh, op := rotateCCW(w, h, pixels)
		flipped := make([]byte, len(op))
		for row := 0; row < oh; row++ {
			copy(flipped[row*ow:(row+1)*ow], op[(oh-1-row)*ow:(oh-row)*ow])
		}
		drawWide(buf, x, y, ow, oh, flipped, overwrite)

	default:
		drawWide(buf, x, y, w, h, pixels, overwrite)
	}
}

// drawRotated90 draws a sprite rotated 90° by reading columns from source
// and packing bits into output bytes. Matches the Z80 draw_disp_2/3/6/7.
//
// The Z80 algorithm:
//   - For each source byte-column (outer loop over w):
//     - For each source row (inner loop over h):
//       - Test one bit of the source byte (selected by bitMask)
//       - Pack into output byte H' via RL H' (shift left, carry in)
//       - Every 8 rows, write the packed byte and advance display column
//     - After all rows: advance display up one pixel line
//     - Rotate bitMask to select next bit position
//     - Every 8 bits: move to next/prev source column
//
// mode 2 (CW):  bitMask starts $01 (LSB), rlc (left), columns from end (dec de)
// mode 3 (CCW): bitMask starts $80 (MSB), rrc (right), columns from start (inc de)
// mode 6:       like mode 2 but starts from bottom (sbc_de_b to go up in source)
// mode 7:       like mode 3 but starts from bottom (sbc_de_b to go up in source)
// getPixel reads one pixel from sprite data at pixel position (px, py).
func getPixel(pixels []byte, w, h, px, py int) bool {
	if px < 0 || py < 0 || px >= w*8 || py >= h {
		return false
	}
	return pixels[py*w+px/8]&(0x80>>uint(px%8)) != 0
}

// setPixelIn sets one pixel in an output buffer at pixel position (px, py).
func setPixelIn(out []byte, w, px, py int) {
	out[py*w+px/8] |= 0x80 >> uint(px%8)
}

// rotateCW rotates sprite 90° clockwise at pixel level.
// Input pixel (sx, sy) → output pixel (sy, srcPxW-1-sx).
func rotateCW(w, h int, pixels []byte) (int, int, []byte) {
	srcPxW, srcPxH := w*8, h
	outPxW, outPxH := srcPxH, srcPxW
	outW := (outPxW + 7) / 8
	out := make([]byte, outW*outPxH)
	for sy := 0; sy < srcPxH; sy++ {
		for sx := 0; sx < srcPxW; sx++ {
			if getPixel(pixels, w, h, sx, sy) {
				setPixelIn(out, outW, sy, srcPxW-1-sx)
			}
		}
	}
	return outW, outPxH, out
}

// rotateCCW rotates sprite 90° counter-clockwise at pixel level.
// Input pixel (sx, sy) → output pixel (srcPxH-1-sy, sx).
func rotateCCW(w, h int, pixels []byte) (int, int, []byte) {
	srcPxW, srcPxH := w*8, h
	outPxW, outPxH := srcPxH, srcPxW
	outW := (outPxW + 7) / 8
	out := make([]byte, outW*outPxH)
	for sy := 0; sy < srcPxH; sy++ {
		for sx := 0; sx < srcPxW; sx++ {
			if getPixel(pixels, w, h, sx, sy) {
				setPixelIn(out, outW, srcPxH-1-sy, sx)
			}
		}
	}
	return outW, outPxH, out
}

// reverseBits reverses the bit order of a byte (mirror horizontally).
func reverseBits(b byte) byte {
	b = (b&0xF0)>>4 | (b&0x0F)<<4
	b = (b&0xCC)>>2 | (b&0x33)<<2
	b = (b&0xAA)>>1 | (b&0x55)<<1
	return b
}

func (g *GameEnv) drawWeapon() {
	if !g.weaponActive {
		return
	}

	// Weapon graphic and colour per character class from Z80:
	// Knight: axe base $40, 8 rotating frames, colour $42 (red)
	// Wizard: fireball base $34, 4 cycling frames, colour $45/$47 (cyan/white)
	// Serf: sword base $38, 8 directional frames, colour $46 (yellow)
	var graphicID byte
	var weaponAttr byte

	switch g.character {
	case data.Knight:
		// Axe: 8 rotating frames. Z80: cpl, rra, and $07 on frame counter
		frame := (^byte(g.weaponFrame) >> 1) & 0x07
		graphicID = 0x40 + frame
		weaponAttr = 0x42 // bright red

	case data.Wizard:
		// Fireball: 4 cycling frames. Z80: inc, and $03
		frame := byte(g.weaponFrame) & 0x03
		graphicID = 0x34 + frame
		// Colour alternates: Z80 uses ($5C78) rla, and $02, add $45
		if g.weaponFrame&0x02 == 0 {
			weaponAttr = 0x45 // bright cyan
		} else {
			weaponAttr = 0x47 // bright white
		}

	case data.Serf:
		// Sword: 8 directional frames based on velocity
		dir := byte(0)
		if g.weaponDY < 0 {
			dir = 2 // up
		} else if g.weaponDY > 0 {
			dir = 6 // down
		}
		if g.weaponDX > 0 {
			dir++ // right
		} else if g.weaponDX < 0 {
			if dir == 0 {
				dir = 7
			} else {
				dir--
			}
		}
		graphicID = 0x38 + (dir & 0x07)
		weaponAttr = 0x46 // bright yellow
	}

	// Look up sprite via (graphicID-1) indexing
	flatIdx := int(graphicID) - 1
	group := flatIdx / 4
	frame := flatIdx % 4
	if group >= len(data.GenSpriteTable) {
		return
	}
	addr := data.GenSpriteTable[group][frame]
	spr := data.GenMenuIcons[addr]
	if spr == nil {
		return
	}

	g.buf.DrawSpriteXOR(g.weaponX, g.weaponY, spr)
	g.paintEntityAttr(g.weaponX, g.weaponY, 2, int(spr[0]), weaponAttr)
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
		// Z80 draw_lives at $A2CE: uses graphic $01/$11/$21 = LEFT-facing sprite
		g.buf.DrawSpriteXOR(lx, 139, sprites[data.DirLeft][0])
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
