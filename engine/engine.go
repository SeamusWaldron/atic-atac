package engine

import (
	"fmt"
	"time"

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
// Z80 inventory at $5E30: 4 bytes per slot (entity_lo, entity_hi, graphic, attr).
type InvSlot struct {
	Occupied bool
	ItemType byte   // graphic ID of the item
	Attr     byte   // colour attribute (for key colour matching)
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
	fallTimer      int  // trap door falling animation countdown (128 frames)
	fallDestRoom   byte // destination room after falling
	fallDestX      byte
	fallDestY      byte

	// Entities
	entities   *entity.Pool
	spawnDelay    int
	pickupBlock   int    // frames to block pickup after a successful pickup
	foodReplenIdx int    // round-robin index for food replenishment
	rand          uint16 // simple PRNG

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
	roomDoors      map[byte][]data.RoomDoor
	doorTimer      int
	// doorTypes stores runtime entity types for doors that have been modified
	// by randomise_doors or door cycling. Key = (room<<16 | entityIdx).
	// Z80 modifies the entity type byte directly: $01/$02 become $20-$23.
	// Bit 0 = open(1)/closed(0). XOR $01 toggles state.
	doorTypes      map[uint32]byte
	doorCycleTimer int
	doorCycleIdx   int  // round-robin index for which door to toggle next
	mummyRoom      byte // Mummy spawns in same room as red key (Z80 $98EF)

	// Cheat modes
	immunity      bool
	infiniteLives bool

	// Audio
	sounds []SoundEvent

	// Rendering
	roomDrawn bool
	hudDirty  bool

	// Trap door tunnel — 24×24 attr grid for expanding ring effect.
	// Z80 draw_tunnel_attrs at $9774: spiral fill propagates colour outward.
	tunnelAttrs [24][24]byte
}

// New creates a new game engine.
func New() *GameEnv {
	g := &GameEnv{
		roomDoors: data.BuildRoomDoors(),
		entities:  entity.NewPool(),
		character: data.Knight,
		rand:      uint16(time.Now().UnixNano() & 0xFFFF),
	}
	g.Reset()
	return g
}

// Reset resets the game to initial state.
func (g *GameEnv) Reset() {
	g.state = StateMenu
	// Don't reset g.character here — SetCharacter sets it before calling Reset.
	// Initial creation sets it in New().
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

	// Re-seed PRNG each game — Z80 uses (FRAMES + counter_low) which
	// varies based on when the player starts from the menu.
	g.rand = uint16(time.Now().UnixNano() & 0xFFFF)
	if g.rand == 0 {
		g.rand = 1
	}

	g.doorTypes = make(map[uint32]byte)
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

// Room returns the current room number.
func (g *GameEnv) Room() byte { return g.room }

// SetImmunity enables/disables immunity mode (no energy loss).
func (g *GameEnv) SetImmunity(on bool) { g.immunity = on }

// SetInfiniteLives enables/disables infinite lives mode.
func (g *GameEnv) SetInfiniteLives(on bool) { g.infiniteLives = on }

// Immunity returns whether immunity mode is on.
func (g *GameEnv) Immunity() bool { return g.immunity }

// InfiniteLives returns whether infinite lives mode is on.
func (g *GameEnv) InfiniteLives() bool { return g.infiniteLives }

// Entities returns the entity pool (for debug overlay).
func (g *GameEnv) Entities() *entity.Pool { return g.entities }

// KeyRooms returns the rooms containing key entities (for debug browsing).
func (g *GameEnv) KeyRooms() []byte {
	var rooms []byte
	for i := range g.entities.Entities {
		e := &g.entities.Entities[i]
		if e.Active && e.Type == entity.TypeKey {
			rooms = append(rooms, e.Room)
		}
	}
	return rooms
}

// KeyInfo returns debug info about all key entities.
func (g *GameEnv) KeyInfo() []string {
	var info []string
	for i := range g.entities.Entities {
		e := &g.entities.Entities[i]
		if e.Active && e.Type == entity.TypeKey {
			name := "?"
			switch e.Graphic {
			case ItemACGKey1:
				name = "ACG-1"
			case ItemACGKey2:
				name = "ACG-2"
			case ItemACGKey3:
				name = "ACG-3"
			case 0x81:
				switch e.Attr {
				case 0x42:
					name = "RED"
				case 0x44:
					name = "GREEN"
				case 0x45:
					name = "CYAN"
				case 0x46:
					name = "YELLOW"
				}
			}
			info = append(info, fmt.Sprintf("%s room=%02X pos=(%d,%d) gfx=%02X", name, e.Room, e.X, e.Y, e.Graphic))
		}
	}
	return info
}

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
	case StateFalling:
		g.stepFalling()
	case StateGameOver:
		g.stepGameOver()
	case StateWin:
		g.stepWin()
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
	g.updateBosses()
	g.checkCreaturePlayerCollision()
	g.checkBossPlayerCollision()

	// Food auto-consumption on contact (Z80 h_food at $8C63)
	g.checkFoodPickup()
	g.checkMushroomPoison()
	// Pickup cooldown — Z80 $5E1F blocks pickup for a few frames after
	// each successful pickup, preventing chain-pickup of dropped items.
	if g.pickupBlock > 0 {
		g.pickupBlock--
	} else {
		g.checkColourKeyPickup()
		g.checkPickup(act)
	}
	// Secret passage check (Z80 h_barrel/$9421, h_bookcase/$9428, h_clock/$942F)
	g.checkSecretPassage()
	// Trap door check (Z80 h_trap_closed/$91BC, h_trap_open/$91C5)
	g.checkTrapDoor()

	// Passive energy drain: 1 point every 16 frames (original: $0F mask check)
	if !g.immunity && g.frame&0x0F == 0 && g.energy > 0 {
		g.energy--
	}

	// Clock
	g.updateClock()
	// Food replenishment (Z80 replenish_food at $9924)
	g.replenishFood()

	// Check win condition: all 3 ACG key pieces collected
	// Win condition: Z80 h_acg_exit at $961B — player must touch the ACG
	// exit decoration (type $24) with all 3 ACG key pieces in inventory
	// in the correct order: slot 0=$8C, slot 1=$8D, slot 2=$8E.
	g.checkACGExit()

	// Door transition cooldown timer
	if g.doorTimer > 0 {
		g.doorTimer--
	}

	// Door open/close cycling (Z80 $5E2E timer, XOR $01 toggles bit 0)
	g.cycleDoors()
	g.cycleTraps()

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
			g.paintEntityAttr(int(g.playerX), int(g.playerY), partialData, 0x47)
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
				g.paintEntityAttr(int(g.deathX), int(g.deathY), spr, 0x45)
			}
		}

		if g.lives == 0 && !g.infiniteLives {
			g.state = StateGameOver
			g.frame = 0
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
	if g.frame == 1 {
		g.emitSound(SoundPlayerSpawn)
	}

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
		g.paintEntityAttr(int(g.playerX), int(g.playerY), partialData, attr)
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

// stepGameOver shows "GAME OVER" with stats, then returns to menu.
// Z80 $8C35: clear play area, show "GAME OVER", show stats (time, score, %),
// long delay (~7 seconds), then jump to menu at $7C29.
func (g *GameEnv) stepGameOver() {
	g.frame++

	if g.frame == 1 {
		// Clear play area
		g.clearPlayArea()
		// Z80 $8C35: "GAME OVER" at (64,48) using custom font from $BF4C
		cs := &data.GenCharset
		g.buf.DrawStringFrom(64, 48, "GAME OVER", cs)
		g.buf.FillAttrArea(0, 0, 24, 24, 0x00) // black background
		g.buf.FillAttrArea(8, 6, 12, 1, 0x47)   // bright white for text
		// Stats
		g.drawGameStats()
	}

	// HUD stays visible
	g.clearHUDArea()
	g.drawHUD()

	// After ~7 seconds (350 frames at 50fps), return to menu
	if g.frame > 350 {
		g.Reset()
	}
}

// stepWin shows congratulations with stats, then returns to menu.
// Z80 $96EC: show "CONGRATULATIONS" and "YOU HAVE ESCAPED", stats, delay, menu.
func (g *GameEnv) stepWin() {
	g.frame++

	if g.frame == 1 {
		g.clearPlayArea()
		cs := &data.GenCharset
		g.buf.DrawStringFrom(40, 32, "CONGRATULATIONS", cs)
		g.buf.DrawStringFrom(40, 48, "YOU HAVE ESCAPED", cs)
		g.buf.FillAttrArea(0, 0, 24, 24, 0x00) // black background
		g.buf.FillAttrArea(5, 4, 16, 1, 0x47)   // bright white for "CONGRATULATIONS" (15 chars from col 5)
		g.buf.FillAttrArea(5, 6, 16, 1, 0x47)   // bright white for "YOU HAVE ESCAPED" (16 chars from col 5)
		g.drawGameStats()
	}

	g.clearHUDArea()
	g.drawHUD()

	if g.frame > 350 {
		g.Reset()
	}
}

// drawGameStats renders TIME, SCORE, and room visit % on the play area.
// Z80 game_stats at $9641: prints at rows 8, 10, 12 of play area.
func (g *GameEnv) drawGameStats() {
	// Z80 game_stats at $9641: uses custom font at $BF4C for all text.
	// Labels in bright cyan ($45), values in bright white ($47).
	cs := &data.GenCharset

	// TIME row (Y=64)
	g.buf.DrawStringFrom(64, 64, "TIME", cs)
	g.buf.DrawStringFrom(128, 64, formatClockShort(g.clockM, g.clockS), cs)
	g.buf.FillAttrArea(8, 8, 6, 1, 0x45)  // TIME label: cyan
	g.buf.FillAttrArea(16, 8, 6, 1, 0x47) // time value: white

	// SCORE row (Y=80)
	g.buf.DrawStringFrom(64, 80, "SCORE", cs)
	g.buf.DrawStringFrom(128, 80, formatBCD(g.score), cs)
	g.buf.FillAttrArea(8, 10, 6, 1, 0x45)
	g.buf.FillAttrArea(16, 10, 6, 1, 0x47)

	// % row (Y=96) — Z80 percent_msg at $969F uses "$" character,
	// which maps to a "%" glyph in the custom font.
	g.buf.DrawStringFrom(64, 96, "$", cs)
	g.buf.DrawStringFrom(128, 96, formatBCD(uint32(g.visitPercent)), cs)
	g.buf.FillAttrArea(8, 12, 6, 1, 0x45)
	g.buf.FillAttrArea(16, 12, 6, 1, 0x47)
}

// stepFalling handles the trap door falling tunnel animation.
// Z80 chk_trap_exit at $9731:
//   1. Clear screen, draw room 150 lines (12 concentric squares) — STATIC
//   2. 128-frame loop: inject flash colour at 2 central attr cells,
//      then spiral-fill attrs from outside inward. Each ring reads its
//      colour from the cell one-row-up, one-col-right. This propagates
//      the flash colour outward by one ring per frame, creating expanding
//      concentric white rings — the falling tunnel effect.
func (g *GameEnv) stepFalling() {
	g.frame++
	g.fallTimer--

	// Clear pixels each frame.
	for i := range g.buf.Pixels {
		g.buf.Pixels[i] = 0
	}

	// Progressive reveal: expand outward over 30 frames.
	// 30 frames / 12 squares ≈ 2.5 frames per square.
	elapsed := 30 - g.fallTimer // 1→30
	current := (elapsed * 12) / 30 // scale to 0-11
	if current > 11 {
		current = 11
	}
	// Draw current square and the one before it (2 visible at a time)
	for sq := current; sq >= 0 && sq >= current-1; sq-- {
		g.drawTrapTunnelSquare(sq)
	}

	// Keep attrs white so lines are visible
	g.buf.FillAttrArea(0, 0, 24, 24, 0x47)

	// Keep HUD visible
	g.clearHUDArea()
	g.drawHUD()

	// When animation complete, transition to destination room
	if g.fallTimer <= 0 {
		g.room = g.fallDestRoom
		g.playerX = g.fallDestX
		g.playerY = g.fallDestY
		g.roomDrawn = false
		g.hudDirty = true
		g.doorTimer = 25
		g.spawnDelay = 32
		g.markRoomVisited(g.room)
		g.state = StatePlaying
	}
}

// drawTrapTunnelN draws the first n concentric squares (of 12 total).
func (g *GameEnv) drawTrapTunnelN(n int) {
	if n > 12 {
		n = 12
	}
	for sq := 0; sq < n; sq++ {
		g.drawTrapTunnelSquare(sq)
	}
}

// trapTunnelPoints are the 12 concentric squares from Z80 points_trap ($97A9).
var trapTunnelPoints = [48][2]int{
	{0x5C, 0x63}, {0x63, 0x63}, {0x63, 0x5C}, {0x5C, 0x5C}, // 0: innermost
	{0x54, 0x6B}, {0x6B, 0x6B}, {0x6B, 0x54}, {0x54, 0x54}, // 1
	{0x4C, 0x73}, {0x73, 0x73}, {0x73, 0x4C}, {0x4C, 0x4C}, // 2
	{0x44, 0x7B}, {0x7B, 0x7B}, {0x7B, 0x44}, {0x44, 0x44}, // 3
	{0x3C, 0x83}, {0x83, 0x83}, {0x83, 0x3C}, {0x3C, 0x3C}, // 4
	{0x34, 0x8B}, {0x8B, 0x8B}, {0x8B, 0x34}, {0x34, 0x34}, // 5
	{0x2C, 0x93}, {0x93, 0x93}, {0x93, 0x2C}, {0x2C, 0x2C}, // 6
	{0x24, 0x9B}, {0x9B, 0x9B}, {0x9B, 0x24}, {0x24, 0x24}, // 7
	{0x1C, 0xA3}, {0xA3, 0xA3}, {0xA3, 0x1C}, {0x1C, 0x1C}, // 8
	{0x14, 0xAB}, {0xAB, 0xAB}, {0xAB, 0x14}, {0x14, 0x14}, // 9
	{0x0C, 0xB3}, {0xB3, 0xB3}, {0xB3, 0x0C}, {0x0C, 0x0C}, // 10
	{0x04, 0xBB}, {0xBB, 0xBB}, {0xBB, 0x04}, {0x04, 0x04}, // 11: outermost
}

// drawTrapTunnelSquare draws a single concentric rectangle by index (0=innermost).
func (g *GameEnv) drawTrapTunnelSquare(sq int) {
	if sq < 0 || sq > 11 {
		return
	}
	base := sq * 4
	p0 := trapTunnelPoints[base+0] // BL
	p1 := trapTunnelPoints[base+1] // BR
	p2 := trapTunnelPoints[base+2] // TR
	p3 := trapTunnelPoints[base+3] // TL
	g.buf.DrawLine(p0[0], p0[1], p1[0], p1[1]) // bottom
	g.buf.DrawLine(p0[0], p0[1], p3[0], p3[1]) // left
	g.buf.DrawLine(p2[0], p2[1], p1[0], p1[1]) // right
	g.buf.DrawLine(p2[0], p2[1], p3[0], p3[1]) // top
}

// spiralFillTunnelAttrs replicates Z80 draw_tunnel_attrs at $9774 exactly.
// Uses a flat 768-byte buffer matching the Z80 attr area at $5800.
// Operates with the same address arithmetic as the Z80 code.
func (g *GameEnv) spiralFillTunnelAttrs() {
	// Map tunnelAttrs [24][24] ↔ flat Z80 attr buffer (32 bytes per row)
	// Z80 attr offset = row*32 + col. We only use cols 0-23.
	//
	// get/set helpers to translate between flat Z80 offset and our grid:
	get := func(offset int) byte {
		r := offset / 32
		c := offset % 32
		if r >= 0 && r < 24 && c >= 0 && c < 24 {
			return g.tunnelAttrs[r][c]
		}
		return 0
	}
	set := func(offset int, v byte) {
		r := offset / 32
		c := offset % 32
		if r >= 0 && r < 24 && c >= 0 && c < 24 {
			g.tunnelAttrs[r][c] = v
		}
	}

	// Z80: BC=$170B, HL=$5AE0, DE=$0020
	// We use offsets relative to $5800, so HL starts at $2E0
	hl := 0x2E0 // $5AE0 - $5800
	de := 0x020 // $0020
	b := 23     // $17
	c := 11     // $0B

	for c > 0 {
		savedHL := hl

		// $977F: HL = HL - DE
		hl -= de
		// $9781: inc L (increment low byte only — column +1)
		hl = (hl & 0xFFE0) | ((hl + 1) & 0x1F)
		// $9782: A = (HL)
		a := get(hl)
		// $9783: restore HL
		hl = savedHL

		// $9785: horizontal right — write A, inc L, B times
		for i := 0; i < b; i++ {
			set(hl, a)
			hl = (hl & 0xFFE0) | ((hl + 1) & 0x1F) // inc L
		}

		// $978B: vertical up — write A, HL -= DE, B times
		for i := 0; i < b; i++ {
			set(hl, a)
			hl -= de // sbc hl, de
		}

		// $9793: horizontal left — write A, dec L, B times
		for i := 0; i < b; i++ {
			set(hl, a)
			hl = (hl & 0xFFE0) | ((hl - 1) & 0x1F) // dec L
		}

		// $9799: vertical down — write A, HL += DE, B times
		for i := 0; i < b; i++ {
			set(hl, a)
			hl += de // add hl, de
		}

		// $979D: (HL) = A
		set(hl, a)
		// $979F: HL -= DE
		hl -= de
		// $97A1: inc L
		hl = (hl & 0xFFE0) | ((hl + 1) & 0x1F)

		// $97A3-$97A4: B -= 2
		b -= 2
		// $97A5: C -= 1
		c--
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
		// Z80 walk_sound at $A3C7: click every 2 frames
		if g.walkCounter%4 == 0 {
			g.emitSound(SoundWalkClick)
		}
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

		// Skip trap doors (types $18/$19) — player walks over them
		// (fall detection handled separately in checkTrapDoor)
		if typeID == 0x18 || typeID == 0x19 {
			continue
		}

		// Skip secret passages for the matching character class.
		// Z80: h_clock($942F)/h_bookcase($9428)/h_barrel($9421) check
		// player graphic base; matching character passes through.
		if (typeID == 0x10 && g.character == data.Knight) ||
			(typeID == 0x17 && g.character == data.Wizard) ||
			(typeID == 0x1A && g.character == data.Serf) {
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

// randomiseDoorStates initialises the runtime door type map.
// Z80 randomise_doors at $94F5: overwrites types $01/$02 with $20/$22
// (closed handler types) for ~56% of doors. Others become $21/$23 (open).
func (g *GameEnv) randomiseDoorStates() {
	g.doorTypes = make(map[uint32]byte)
	g.doorCycleTimer = 94

	// Z80 randomise_doors at $94F5: for each door pair, read a random byte.
	// If value >= $70 (~56%): door stays OPEN (default state).
	// If value < $70 (~44%): door becomes CLOSED.
	// Both sides of the pair get the SAME state.
	for room, entities := range data.GenRoomEntityData {
		for i, pair := range entities {
			// Use side A's type to determine if this is a door
			typeA := pair[0]
			typeB := pair[8]

			// Only process door pairs (types $01=cave, $02=normal)
			isDoor := false
			var closedType, openType byte
			if typeA == 0x01 || typeB == 0x01 {
				isDoor = true
				closedType = 0x22 // closed cave
				openType = 0x23   // open cave
			} else if typeA == 0x02 || typeB == 0x02 {
				isDoor = true
				closedType = 0x20 // closed normal
				openType = 0x21   // open normal
			}

			if !isDoor {
				continue
			}

			// One random check per pair — same state for both sides
			key := uint32(room)<<16 | uint32(i)
			if g.nextRand() >= 0x70 {
				g.doorTypes[key] = openType // ~56% stay open
			} else {
				g.doorTypes[key] = closedType // ~44% become closed
			}
		}
	}
}

// getDoorType returns the runtime entity type for a door.
// If not in doorTypes map, returns the original type from entity data.
// permanentlyOpenDoor changes a locked door to type $02 (open) for both
// linked entities. Z80 set_door_type at $9260: writes to (ix+$00) and
// the XOR $08 linked entry. The door stays open for the rest of the game.
func (g *GameEnv) permanentlyOpenDoor(d data.RoomDoor) {
	entities := data.GenRoomEntityData[int(g.room)]
	for ei, pair := range entities {
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
			if int(e[3]) != int(d.X) || int(e[4]) != int(d.Y) {
				continue
			}
			if e[0] < 0x08 || e[0] > 0x0F {
				continue
			}
			// Set this door to open ($02) in the runtime type map
			key := uint32(g.room)<<16 | uint32(ei)
			g.doorTypes[key] = 0x02
			// Also set the linked door (other room side) to open
			otherRoom := pair[9]
			if side == 1 {
				otherRoom = pair[1]
			}
			// Find the entity index in the other room
			otherEntities := data.GenRoomEntityData[int(otherRoom)]
			for oei, opair := range otherEntities {
				if opair[1] == otherRoom || opair[9] == otherRoom {
					// Check if this is the same pair
					if opair == pair {
						oKey := uint32(otherRoom)<<16 | uint32(oei)
						g.doorTypes[oKey] = 0x02
					}
				}
			}
			return
		}
	}
}

func (g *GameEnv) getDoorType(room byte, entityIdx int) byte {
	key := uint32(room)<<16 | uint32(entityIdx)
	if rt, ok := g.doorTypes[key]; ok {
		return rt
	}
	return 0 // not a managed door
}

// isDoorOpenRuntime checks if a door is open using the runtime type.
// Z80: bit 0 of type = 1 means open, 0 means closed.
func (g *GameEnv) isDoorOpenRuntime(room byte, entityIdx int) bool {
	rt := g.getDoorType(room, entityIdx)
	if rt == 0 {
		return true // unmanaged doors default to open
	}
	return rt&0x01 != 0 // bit 0 = open
}

// cycleDoors toggles a door in the current room every 94 frames.
// Z80: XOR $01 on the entity type toggles bit 0 (open/closed).
func (g *GameEnv) cycleDoors() {
	g.doorCycleTimer--
	if g.doorCycleTimer > 0 {
		return
	}
	g.doorCycleTimer = 94

	// Collect all managed doors in this room, then toggle the next one
	// in round-robin order so all doors get a chance to cycle.
	entities := data.GenRoomEntityData[int(g.room)]
	var doorKeys []uint32
	for i, pair := range entities {
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
			key := uint32(g.room)<<16 | uint32(i)
			if rt, ok := g.doorTypes[key]; ok && rt >= 0x20 && rt <= 0x23 {
				doorKeys = append(doorKeys, key)
			}
			break // one entry per pair
		}
	}
	if len(doorKeys) > 0 {
		idx := g.doorCycleIdx % len(doorKeys)
		g.doorTypes[doorKeys[idx]] ^= 0x01
		g.doorCycleIdx++
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

		// Direction change rate varies by creature type (Z80 per-handler masks)
		interval := byte(16) // default
		switch entity.CreatureKind(e.Timer) {
		case entity.KindGhoul:
			interval = 8  // and $07
		case entity.KindSpider, entity.KindPumpkin, entity.KindBatlet, entity.KindGhostlet:
			interval = 16 // and $0F
		case entity.KindBlob:
			interval = 17 // dec from $11
		case entity.KindWitch:
			interval = 16 // dec from $10
		case entity.KindMonk, entity.KindBat:
			interval = 32 // dec from $20
		case entity.KindGhost:
			interval = 24
		}
		if e.Frame%interval == 0 {
			g.setRandomVelocity(e)
		}
	})
}

// updateBosses updates boss creature AI each frame.
func (g *GameEnv) updateBosses() {
	px := int(g.playerX)
	py := int(g.playerY)

	g.entities.ForEachInRoom(g.room, func(e *entity.Entity) {
		if e.Type != entity.TypeBoss {
			return
		}
		e.Frame++ // animation counter

		speed := 1

		switch e.Timer {
		case entity.BossMummy:
			// Z80 h_mummy at $8862: hunts leaf, then red key, then chases player
			leafFound := false
			g.entities.ForEachInRoom(e.Room, func(item *entity.Entity) {
				if item.Active && item.Graphic == 0x80 && item.Room == e.Room { // leaf
					// Move toward leaf
					if e.X < item.X { e.X += speed } else if e.X > item.X { e.X -= speed }
					if e.Y < item.Y { e.Y += speed } else if e.Y > item.Y { e.Y -= speed }
					// Check if reached leaf
					if abs(e.X-item.X) < 4 && abs(e.Y-item.Y) < 4 {
						// Move leaf to room $6B
						item.Room = 0x6B
						item.X = 0x50
						item.Y = 0x50
					}
					leafFound = true
				}
			})
			if !leafFound {
				// Chase player (simplified from Z80's red key tracking)
				if e.X < px { e.X += speed } else if e.X > px { e.X -= speed }
				if e.Y < py { e.Y += speed } else if e.Y > py { e.Y -= speed }
			}

		case entity.BossDracula:
			// Z80 h_dracula at $8906: chase player, flee crucifix, room hop
			if e.X < px { e.X += speed } else if e.X > px { e.X -= speed }
			if e.Y < py { e.Y += speed } else if e.Y > py { e.Y -= speed }
			for _, slot := range g.inventory {
				if slot.Occupied && slot.Name == "CRUCIX" {
					if e.X < px { e.X -= speed * 3 } else { e.X += speed * 3 }
					if e.Y < py { e.Y -= speed * 3 } else { e.Y += speed * 3 }
					break
				}
			}
			if e.Frame%50 == 0 {
				newRoom := g.nextRand()
				if int(newRoom) < data.NumRooms && newRoom != g.room {
					ra := data.RoomAttrs[newRoom]
					if ra.Style < 3 { e.Room = newRoom }
				}
			}

		case entity.BossDevil:
			// Z80 h_devil at $89ED: always chases player
			if e.X < px { e.X += speed } else if e.X > px { e.X -= speed }
			if e.Y < py { e.Y += speed } else if e.Y > py { e.Y -= speed }

		case entity.BossFrankenstein:
			// Z80 h_frankenstein at $8988: chase player, killed by spanner
			if e.X < px { e.X += speed } else if e.X > px { e.X -= speed }
			if e.Y < py { e.Y += speed } else if e.Y > py { e.Y -= speed }
			for _, slot := range g.inventory {
				if slot.Occupied && slot.Name == "SPANNER" {
					e.Active = false
					g.score += 1000
					g.hudDirty = true
					return
				}
			}

		case entity.BossHunchback:
			// Z80 h_hunchback at $8AFF: hunts 8 specific floor items, steals them.
			// Items: collectibles with specific graphics.
			itemFound := false
			g.entities.ForEachInRoom(e.Room, func(item *entity.Entity) {
				if itemFound { return }
				if !item.Active || item.Room != e.Room { return }
				if item.Type != entity.TypeCollectible && item.Type != entity.TypeKey { return }
				// Hunchback targets collectibles on the floor
				if item.Type == entity.TypeCollectible {
					// Move toward item
					if e.X < item.X { e.X += speed } else if e.X > item.X { e.X -= speed }
					if e.Y < item.Y { e.Y += speed } else if e.Y > item.Y { e.Y -= speed }
					// Check if reached — steal and remove
					if abs(e.X-item.X) < 4 && abs(e.Y-item.Y) < 4 {
						item.Active = false
					}
					itemFound = true
				}
			})
			// If no item to hunt, stay still (Z80: velocity = 0)
			if !itemFound {
				// Idle — don't chase player
			}

		default:
			// Unknown boss: chase player
			if e.X < px { e.X += speed } else if e.X > px { e.X -= speed }
			if e.Y < py { e.Y += speed } else if e.Y > py { e.Y -= speed }
		}

		// Wall bounds for bosses
		ra := data.RoomAttrs[e.Room]
		style := data.RoomStyles[ra.Style]
		rw := int(style.Width) - 4
		rh := int(style.Height) - 4
		if !inWallBounds(e.X, roomCentreX, rw) {
			if e.X < roomCentreX {
				e.X = roomCentreX - rw + 1
			} else {
				e.X = roomCentreX + rw - 1
			}
		}
		if !inWallBounds(e.Y, roomCentreY, rh) {
			if e.Y < roomCentreY {
				e.Y = roomCentreY - rh + 1
			} else {
				e.Y = roomCentreY + rh - 1
			}
		}
	})
}

// checkBossPlayerCollision checks boss-player touch damage.
func (g *GameEnv) checkBossPlayerCollision() {
	const collisionDist = 12
	px := int(g.playerX)
	py := int(g.playerY)

	g.entities.ForEachInRoom(g.room, func(e *entity.Entity) {
		if e.Type != entity.TypeBoss {
			return
		}
		if abs(px-e.X) >= collisionDist || abs(py-e.Y) >= collisionDist {
			return
		}
		// Boss damage: Hunchback=16, others=8
		damage := byte(8)
		if e.Timer == entity.BossHunchback {
			damage = 16
		}
		if !g.immunity && g.frame&0x07 == 0 {
			if g.energy > damage {
				g.energy -= damage
			} else {
				g.energy = 0
			}
			g.hudDirty = true
			if g.energy == 0 {
				g.playerDeath()
			}
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
			if !g.immunity && g.frame&0x07 == 0 {
				if g.energy > 32 {
					g.energy -= 32
				} else {
					g.energy = 0
				}
				g.hudDirty = true
				g.emitSound(SoundMonsterTouch)
				if g.energy == 0 {
					g.playerDeath()
				}
			}
		}
	})
}

func (g *GameEnv) playerDeath() {
	if !g.infiniteLives && g.lives > 0 {
		g.lives--
	}
	g.emitSound(SoundPlayerDeath)
	// Start death shrink animation (Z80 h_death at $8D45)
	g.state = StateDying
	g.deathX = g.playerX
	g.deathY = g.playerY
	sprites := data.CharacterSprites(g.character)
	sprData := sprites[g.playerDir][0]
	g.playerAnimH = int(sprData[0]) // start at full height
	g.hudDirty = true
}

// cycleTraps toggles trap doors between open ($19) and closed ($18).
// Z80 h_trap_closed at $91BC: toggles when $5E12 (frame counter low byte) = 0,
// which happens every 256 frames (~5 seconds).
func (g *GameEnv) cycleTraps() {
	if g.frame&0xFF != 0 {
		return // only toggle when low byte of frame counter is 0
	}

	entities := data.GenRoomEntityData[int(g.room)]
	for ei, pair := range entities {
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
			if e[0] != 0x18 && e[0] != 0x19 {
				continue
			}
			// Toggle: XOR $01 on the runtime type (Z80 trap_common at $91CC)
			key := uint32(g.room)<<16 | uint32(ei)
			rt, ok := g.doorTypes[key]
			if !ok {
				rt = e[0] // use original type if not yet tracked
			}
			g.doorTypes[key] = rt ^ 0x01
			g.emitSound(SoundDoorNoise)
			break // one per pair
		}
	}
}

// ---------- WEAPON ----------

func (g *GameEnv) fireWeapon() {
	// Z80: knight=$A41B(axe), wizard=$A438(fireball), serf=$A427(sword)
	switch g.character {
	case data.Knight:
		g.emitSound(SoundAxeThrow)
	case data.Wizard:
		g.emitSound(SoundFireball)
	case data.Serf:
		g.emitSound(SoundSwordThrow)
	}
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
		g.emitSound(SoundWeaponBounce)
	}
	if !inWallBounds(g.weaponY, roomCentreY, int(style.Height)) {
		g.weaponDY = -g.weaponDY
		g.weaponY += g.weaponDY
		g.emitSound(SoundWeaponBounce)
	}

	if g.weaponTimer <= 0 {
		g.weaponActive = false
		return
	}

	// Check hit on creatures (NOT bosses — bosses are immune to weapons)
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
			g.emitSound(SoundWeaponPop)
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
		// Z80 place_key_pieces at $94B6: each piece goes to a DIFFERENT
		// room from the 3-byte set. Piece 0 → set[0], 1 → set[1], 2 → set[2].
		e.Room = acgRoomTable[acgSet][i]
		e.X = int(k[3])
		e.Y = int(k[4])
		e.Attr = k[5]
		e.Graphic = k[0]
	}

	// --- Coloured keys: each randomised to one of 8 rooms ---
	// Z80 set_key_positions at $98D2
	greenRooms := [8]byte{0x05, 0x06, 0x07, 0x6D, 0x25, 0x24, 0x23, 0x22}
	redRooms := [8]byte{0x17, 0x13, 0x09, 0x0D, 0x89, 0x87, 0x80, 0x85}
	cyanRooms := [8]byte{0x53, 0x8F, 0x41, 0x94, 0x33, 0x91, 0x39, 0x4C}

	// Z80 set_key_positions at $98D2: each key uses an independent random
	// index into its room table. Mummy room = red key room ($98EF).
	greenIdx := int(g.nextRand()) & 0x07
	redIdx := int(g.nextRand()) & 0x07
	cyanIdx := int(g.nextRand()) & 0x07

	greenRoom := greenRooms[greenIdx]
	redRoom := redRooms[redIdx]
	cyanRoom := cyanRooms[cyanIdx]

	// Save red key room for Mummy placement
	g.mummyRoom = redRoom

	colourKeySpawns := []struct {
		init data.EntityInit
		room byte
	}{
		{data.GreenKeyInit, greenRoom},
		{data.RedKeyInit, redRoom},
		{data.CyanKeyInit, cyanRoom},
	}
	for _, ck := range colourKeySpawns {
		e := g.entities.Spawn()
		if e == nil {
			break
		}
		e.Type = entity.TypeKey
		e.Room = ck.room
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

	// --- Boss creatures (5 unique monsters) ---
	bosses := []struct {
		kind    byte
		graphic byte
		room    byte
		x, y    byte
		attr    byte
	}{
		{entity.BossMummy, 0x70, g.mummyRoom, 0x50, 0x50, 0x47},
		{entity.BossDracula, 0x7C, 0x6D, 0x50, 0x50, 0x44},
		{entity.BossDevil, 0x78, 0x43, 0x50, 0x50, 0x43},
		{entity.BossFrankenstein, 0x74, 0x55, 0x50, 0x50, 0x42},
		{entity.BossHunchback, 0x9C, 0x56, 0x58, 0x38, 0x42},
	}
	for _, b := range bosses {
		e := g.entities.Spawn()
		if e == nil {
			break
		}
		e.Type = entity.TypeBoss
		e.Room = b.room
		e.X = int(b.x)
		e.Y = int(b.y)
		e.Attr = b.attr
		e.Graphic = b.graphic
		e.Timer = b.kind
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
		// Z80 $8C7D: ADD A,C / JR C,$8C84 — checks carry before compare.
		// Must check overflow before the byte wraps.
		newEnergy := uint16(g.energy) + 64
		if newEnergy > uint16(InitialEnergy) {
			newEnergy = uint16(InitialEnergy)
		}
		g.energy = byte(newEnergy)
		e.Active = false
		g.hudDirty = true
		g.emitSound(SoundFoodEaten)
	})
}

// replenishFood respawns eaten food items over time.
// Z80 replenish_food at $9924: when 9-bit counter = 0 (~every 512 frames),
// finds the next empty food slot not in the player's room and respawns it
// with a random food graphic ($50-$57).
func (g *GameEnv) replenishFood() {
	// 9-bit counter check: low 9 bits of frame counter must be 0
	if g.frame&0x1FF != 0 {
		return
	}

	// Round-robin through food entities
	foodCount := len(data.FoodInit)
	if foodCount == 0 {
		return
	}

	// Try to find an eaten food item to replenish
	for tries := 0; tries < foodCount; tries++ {
		g.foodReplenIdx = (g.foodReplenIdx + 1) % foodCount
		idx := g.foodReplenIdx

		// Find the entity for this food item
		entityIdx := -1
		foodNum := 0
		for i := range g.entities.Entities {
			e := &g.entities.Entities[i]
			if e.Type == entity.TypeFood {
				if foodNum == idx {
					entityIdx = i
					break
				}
				foodNum++
			} else if !e.Active {
				// Check if this was a food item (by matching init data room)
				fi := data.FoodInit[idx]
				if byte(e.Room) == fi[1] && !e.Active {
					entityIdx = i
					break
				}
			}
		}

		// Simpler approach: scan for inactive food-position matches
		fi := data.FoodInit[idx]
		found := false
		for i := range g.entities.Entities {
			e := &g.entities.Entities[i]
			if !e.Active && e.Room == fi[1] && e.X == int(fi[3]) && e.Y == int(fi[4]) {
				// This food slot is empty and not in player's room
				if e.Room != g.room {
					// Respawn with random food graphic
					e.Active = true
					e.Type = entity.TypeFood
					e.Graphic = 0x50 + byte(g.nextRand()&0x07)
					e.Attr = fi[5]
					found = true
				}
				break
			}
		}
		_ = entityIdx
		if found {
			return
		}
	}
}

// checkMushroomPoison drains 1 energy per frame while touching a mushroom.
// Z80 h_mushroom at $988B: continuous drain, colour cycles every 4 frames
// through table [$42,$43,$46,$43] (red, magenta, yellow, magenta).
func (g *GameEnv) checkMushroomPoison() {
	px := int(g.playerX)
	py := int(g.playerY)
	const touchDist = 12

	mushroomColours := [4]byte{0x42, 0x43, 0x46, 0x43}

	g.entities.ForEachInRoom(g.room, func(e *entity.Entity) {
		// Mushrooms use handler graphic $A0-$A1 (entity types in handler_table)
		// In our entity system they're spawned as TypeCreature with specific graphics
		// Actually, mushrooms are spawning entities — check if this is a mushroom
		// by graphic range. Mushroom graphics are around $A0.
		// For now, check entity Graphic >= 0xA0 and Type is creature-like
		if e.Graphic < 0xA0 || e.Graphic > 0xA1 {
			return
		}
		if abs(px-e.X) >= touchDist || abs(py-e.Y) >= touchDist {
			return
		}

		// Colour cycle every 4 frames
		e.Attr = mushroomColours[(g.frame>>2)&0x03]

		// Drain 1 energy per frame
		if !g.immunity && g.energy > 0 {
			g.energy--
			g.hudDirty = true
			if g.energy == 0 {
				g.playerDeath()
			}
		}
	})
}

// checkColourKeyPickup auto-picks up colour keys (graphic $81) on contact.
// Colour keys (red, green, cyan, yellow) are collected by walking over them.
func (g *GameEnv) checkColourKeyPickup() {
	px := int(g.playerX)
	py := int(g.playerY)
	const touchDist = 12

	g.entities.ForEachInRoom(g.room, func(e *entity.Entity) {
		if e.Type != entity.TypeKey || e.Graphic != 0x81 {
			return
		}
		if abs(px-e.X) >= touchDist || abs(py-e.Y) >= touchDist {
			return
		}
		// Colour key name from attr: $42=red, $44=green, $45=cyan, $46=yellow
		name := "KEY"
		switch e.Attr {
		case 0x42:
			name = "RED"
		case 0x44:
			name = "GREEN"
		case 0x45:
			name = "CYAN"
		case 0x46:
			name = "YELLOW"
		}
		newItem := InvSlot{
			Occupied: true,
			ItemType: e.Graphic,
			Attr:     e.Attr,
			Name:     name,
		}
		// Deactivate picked-up entity FIRST — frees a pool slot so
		// dropAndInsert's Spawn() will always succeed.
		e.Active = false
		slot := g.findFreeSlot()
		if slot >= 0 {
			g.inventory[slot] = newItem
		} else {
			g.dropAndInsert(newItem)
		}
		g.hudDirty = true
		g.emitSound(SoundItemPickup)
		g.pickupBlock = 25 // ~0.5s cooldown matching Z80 $5E1F two-phase reset
	})
}

// checkPickup handles ACG key/collectible pickup on Enter key press.
// Z80 h_pickup_item at $92F5: checks pickup key ($5E20), then touch ($90FB).
// If no item is in range, Z80 $93E3 drops the oldest inventory item (manual drop).
func (g *GameEnv) checkPickup(act action.Action) {
	if act&action.Pickup == 0 {
		return
	}

	px := int(g.playerX)
	py := int(g.playerY)
	const pickupDist = 16

	pickedUp := false
	g.entities.ForEachInRoom(g.room, func(e *entity.Entity) {
		if pickedUp { return } // only pick up one item per press
		if e.Type != entity.TypeKey && e.Type != entity.TypeCollectible {
			return
		}
		// Skip colour keys — handled by checkColourKeyPickup (auto-pickup)
		if e.Type == entity.TypeKey && e.Graphic == 0x81 {
			return
		}
		if abs(px-e.X) >= pickupDist || abs(py-e.Y) >= pickupDist {
			return
		}

		switch e.Type {
		case entity.TypeCollectible:
			e.Active = false
			if collectibleNeedsInventory(e.Graphic) {
				newItem := InvSlot{
					Occupied: true,
					ItemType: e.Graphic,
					Attr:     e.Attr,
					Name:     collectibleName(e.Graphic),
				}
				slot := g.findFreeSlot()
				if slot >= 0 {
					g.inventory[slot] = newItem
				} else {
					g.dropAndInsert(newItem)
				}
			}
			g.score += 100
			pickedUp = true

		case entity.TypeKey:
			e.Active = false
			newItem := InvSlot{
				Occupied: true,
				ItemType: e.Graphic,
				Attr:     e.Attr,
				Name:     keyName(e.Graphic),
			}
			slot := g.findFreeSlot()
			if slot >= 0 {
				g.inventory[slot] = newItem
			} else {
				g.dropAndInsert(newItem)
			}
			g.emitSound(SoundItemPickup)
			pickedUp = true
		}
		g.pickupBlock = 25
		g.hudDirty = true
	})

	// Z80 $93E3: if Enter pressed but nothing picked up, manually drop
	// the oldest inventory item. This allows rearranging ACG key order.
	if !pickedUp {
		g.manualDrop()
	}
}

// manualDrop drops the oldest inventory item on the floor when the player
// presses Enter with nothing to pick up. Z80 $93E3: drop slot 3, shift
// slots 1→2 and 2→3, clear slot 1.
func (g *GameEnv) manualDrop() {
	// Find the oldest occupied slot (slot 2 first, then 1, then 0)
	dropSlot := -1
	for i := 2; i >= 0; i-- {
		if g.inventory[i].Occupied {
			dropSlot = i
			break
		}
	}
	if dropSlot < 0 {
		return // nothing to drop
	}

	dropped := g.inventory[dropSlot]
	g.inventory[dropSlot] = InvSlot{}

	// Spawn dropped item on the floor
	e := g.entities.Spawn()
	if e != nil {
		e.Type = entity.TypeKey
		if collectibleNeedsInventory(dropped.ItemType) {
			e.Type = entity.TypeCollectible
		}
		e.Room = g.room
		e.X = int(g.playerX)
		e.Y = int(g.playerY)
		e.Graphic = dropped.ItemType
		e.Attr = dropped.Attr
		g.emitSound(SoundItemDrop)
	}

	// Shift remaining items: compact toward slot 0
	// Z80 shifts slots 1→2, 2→3 and clears slot 1
	compact := [3]InvSlot{}
	ci := 0
	for i := 0; i < 3; i++ {
		if g.inventory[i].Occupied {
			compact[ci] = g.inventory[i]
			ci++
		}
	}
	g.inventory = compact
	g.hudDirty = true
	g.pickupBlock = 25
}

// dropAndInsert drops the oldest item (slot 2), shifts slots 0→1, 1→2,
// and inserts the new item into slot 0. Matches Z80 flow:
//   $9358 drop_item: drops slot 3 (our slot 2)
//   $934C shift_inventory: shifts slots 1+2 → 2+3 (our 0+1 → 1+2)
//   $9326 add_inventory: adds to slot 1 (our slot 0)
// dropAndInsert drops the oldest item (slot 2) as a floor entity, shifts
// slots 0→1, 1→2, and inserts the new item at slot 0.
// Callers must deactivate the picked-up entity BEFORE calling this to
// guarantee a free pool slot for the dropped item.
func (g *GameEnv) dropAndInsert(newItem InvSlot) {
	// Drop the oldest item (slot 2) as a floor entity
	if g.inventory[2].Occupied {
		dropped := g.inventory[2]
		e := g.entities.Spawn()
		if e == nil {
			return // shouldn't happen — caller freed a slot first
		}
		// Determine entity type based on what was stored
		e.Type = entity.TypeKey
		if collectibleNeedsInventory(dropped.ItemType) {
			e.Type = entity.TypeCollectible
		}
		e.Room = g.room
		e.X = int(g.playerX)
		e.Y = int(g.playerY)
		e.Graphic = dropped.ItemType
		e.Attr = dropped.Attr
		g.emitSound(SoundItemDrop)
	}

	// Shift: slot 0 → 1, slot 1 → 2
	g.inventory[2] = g.inventory[1]
	g.inventory[1] = g.inventory[0]

	// Insert new item at slot 0
	g.inventory[0] = newItem
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

// collectibleNeedsInventory returns true for collectibles that have gameplay
// effects when carried (crucifix repels Dracula, spanner kills Frankenstein,
// leaf attracts Mummy).
func collectibleNeedsInventory(graphic byte) bool {
	switch graphic {
	case 0x8A, 0x8B, 0x80: // crucifix, spanner, leaf
		return true
	}
	return false
}

// collectibleName returns the inventory name for special collectibles.
func collectibleName(graphic byte) string {
	switch graphic {
	case 0x8A:
		return "CRUCIX"
	case 0x8B:
		return "SPANNER"
	case 0x80:
		return "LEAF"
	}
	return "ITEM"
}

// checkSecretPassage checks if the player is overlapping a character-specific
// secret passage decoration and triggers room transition if so.
// Z80: h_clock ($942F) = Knight, h_bookcase ($9428) = Wizard, h_barrel ($9421) = Serf.
func (g *GameEnv) checkSecretPassage() {
	if g.doorTimer > 0 {
		return
	}

	// Map character class to passage entity type
	var passageType byte
	switch g.character {
	case data.Knight:
		passageType = 0x10 // clock
	case data.Wizard:
		passageType = 0x17 // bookcase
	case data.Serf:
		passageType = 0x1A // barrel
	default:
		return
	}

	px := int(g.playerX)
	py := int(g.playerY)
	ra := data.RoomAttrs[g.room]
	style := data.RoomStyles[ra.Style]
	rw := int(style.Width)
	rh := int(style.Height)

	entities := data.GenRoomEntityData[int(g.room)]
	for _, pair := range entities {
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

			ex := int(e[3])
			ey := int(e[4])

			// Use same wall-edge detection as doors. Z80 h_clock/h_bookcase/
			// h_barrel call $91F2 (door exit handler) which uses $90CC
			// (check_exit) — player must be at wall edge aligned with passage.
			const align = 24
			onTop := ey < roomCentreY-rh
			onBottom := ey > roomCentreY+rh
			onLeft := ex < roomCentreX-rw
			onRight := ex > roomCentreX+rw

			matched := false
			if onTop && py <= roomCentreY-rh+4 {
				matched = abs(px-ex) < align
			} else if onBottom && py >= roomCentreY+rh-4 {
				matched = abs(px-ex) < align
			} else if onLeft && px <= roomCentreX-rw+4 {
				matched = abs(py-ey) < align
			} else if onRight && px >= roomCentreX+rw-4 {
				matched = abs(py-ey) < align
			} else {
				// Interior passage — use proximity
				matched = abs(px-ex) < 16 && abs(py-ey) < 16
			}

			if !matched {
				continue
			}

			// Get destination from the other side
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

// checkTrapDoor checks if the player is standing on an open trap door.
// Z80 h_trap_open at $91C5: player falls to linked room.
func (g *GameEnv) checkTrapDoor() {
	if g.doorTimer > 0 {
		return
	}

	px := int(g.playerX)
	py := int(g.playerY)
	const trapDist = 12

	entities := data.GenRoomEntityData[int(g.room)]
	for ei, pair := range entities {
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
			// Only check trap door entities ($18/$19)
			if e[0] != 0x18 && e[0] != 0x19 {
				continue
			}
			// Check runtime state — trap must be open ($19) to fall through
			key := uint32(g.room)<<16 | uint32(ei)
			rt, ok := g.doorTypes[key]
			if !ok {
				rt = e[0]
			}
			if rt != 0x19 {
				continue // closed trap — safe
			}

			ex := int(e[3])
			ey := int(e[4])
			if abs(px-ex) >= trapDist || abs(py-ey) >= trapDist {
				continue
			}

			// Player falls through — get destination from other side
			var dest [8]byte
			if side == 0 {
				copy(dest[:], pair[8:16])
			} else {
				copy(dest[:], pair[0:8])
			}

			destRoom := dest[1]
			destX := int(dest[3])
			destY := int(dest[4])

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

			// Start falling tunnel animation (Z80: 128 frames at $9731)
			g.state = StateFalling
			g.emitSound(SoundTrapFall)
			g.fallTimer = 30 // match trap fall sound length (30 steps × 20ms = 600ms @ 50fps)
			g.fallDestRoom = destRoom
			g.fallDestX = byte(destX)
			g.fallDestY = byte(destY)
			g.weaponActive = false
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

// hasACGKeysInOrder checks if all 3 ACG key pieces are in the correct
// inventory slot order. Z80 h_acg_exit at $961B:
//   slot 0 graphic = $8C (ACG key part 1)
//   slot 1 graphic = $8D (ACG key part 2)
//   slot 2 graphic = $8E (ACG key part 3)
func (g *GameEnv) hasACGKeysInOrder() bool {
	return g.inventory[0].Occupied && g.inventory[0].ItemType == ItemACGKey1 &&
		g.inventory[1].Occupied && g.inventory[1].ItemType == ItemACGKey2 &&
		g.inventory[2].Occupied && g.inventory[2].ItemType == ItemACGKey3
}

// checkACGExit checks if the player is touching the ACG exit decoration
// (type $24) in the current room, and if so, whether they have all 3 ACG
// key pieces in the correct inventory order.
func (g *GameEnv) checkACGExit() {
	px := int(g.playerX)
	py := int(g.playerY)
	const touchDist = 12

	entities := data.GenRoomEntityData[int(g.room)]
	for _, pair := range entities {
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
			if e[0] != 0x24 { // ACG exit decoration type
				continue
			}
			ex := int(e[3])
			ey := int(e[4])
			if abs(px-ex) >= touchDist || abs(py-ey) >= touchDist {
				continue
			}
			// Player is touching the ACG exit — check keys in order
			if g.hasACGKeysInOrder() {
				g.state = StateWin
				g.frame = 0
				g.score += 5000
				return
			}
		}
	}
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

		// Check if this door is closed using runtime door type.
		// Closed doors (bit 0 = 0) block passage.
		if d.Type == 0x01 || d.Type == 0x02 {
			entities := data.GenRoomEntityData[int(g.room)]
			blocked := false
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
					if !g.isDoorOpenRuntime(g.room, ei) {
						blocked = true
					}
					break
				}
			}
			if blocked {
				continue
			}
		}

		// Locked door check: types $08-$0F require matching colour key.
		// But if the door has been permanently opened (runtime type $02),
		// skip the key check — it's already unlocked.
		if d.Type >= 0x08 && d.Type <= 0x0F {
			// Check if already permanently opened
			alreadyOpen := false
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
				if e[0] >= 0x08 && e[0] <= 0x0F &&
					int(e[3]) == int(d.X) && int(e[4]) == int(d.Y) {
					rt := g.getDoorType(g.room, ei)
					if rt == 0x02 {
						alreadyOpen = true
					}
					break
				}
			}
			if !alreadyOpen {
				// Z80 check_key_colour at $9222: door type & $03 = colour index.
				keyAttrs := [4]byte{0x42, 0x44, 0x45, 0x46}
				requiredAttr := keyAttrs[d.Type&0x03]
				found := false
				for _, slot := range g.inventory {
					if slot.Occupied && slot.ItemType == 0x81 && slot.Attr == requiredAttr {
						found = true
						break
					}
				}
				if !found {
					continue // locked — no matching key
				}
				// Z80 h_door_locked at $9244: permanently unlock
				g.permanentlyOpenDoor(d)
			}
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
		g.emitSound(SoundRoomEntry)
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
	g.paintEntityAttr(int(g.playerX), int(g.playerY), sprData, 0x47)
	// Also need to restore room colour around the player's previous position
	// but for now just paint the sprite's cells
}

// paintEntityAttr paints an entity sprite's attribute into the buffer.
// In authentic mode this fills the cells the sprite covers (matching Z80
// set_entity_attrs at $A00E). In per-pixel mode it stamps attributes only
// at the pixels the sprite bitmap actually sets, so the sprite does not
// clobber background attributes around its mask.
//
// `spr` is the entity sprite blob: byte 0 = height, then 2 bytes per row,
// top-to-bottom, drawn upward from y. All entity sprites in Atic Atac use
// this 2-byte-wide format.
func (g *GameEnv) paintEntityAttr(x, y int, spr []byte, attr byte) {
	g.buf.StampSpriteAttr(x, y, spr, attr)
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
			g.paintEntityAttr(e.X, e.Y, spr, e.Attr)

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
					g.paintEntityAttr(e.X, e.Y, spr, e.Attr)
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
					g.paintEntityAttr(e.X, e.Y, spr, e.Attr)
				}
			}

		case entity.TypeBoss:
			// Boss sprites: 4-frame animation from graphic base
			// Z80: base + (frame_counter & $03)
			animFrame := byte(e.Frame>>2) & 0x03
			graphicID := e.Graphic + animFrame
			flatIdx := int(graphicID) - 1
			group := flatIdx / 4
			frame := flatIdx % 4
			if group < len(data.GenSpriteTable) {
				addr := data.GenSpriteTable[group][frame]
				if spr := data.GenMenuIcons[addr]; spr != nil {
					g.buf.DrawSpriteXOR(e.X, e.Y, spr)
					g.paintEntityAttr(e.X, e.Y, spr, e.Attr)
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

		// Door state rendering: check runtime type for managed doors.
		// The runtime doorTypes map overrides the original entity type.
		// Z80 set_door_type at $9260 modifies entity byte 0 directly.
		rt := g.getDoorType(g.room, ei)
		if rt != 0 {
			// Runtime type overrides original — use it for rendering.
			if rt == 0x18 || rt == 0x19 {
				// Trap door: $18=closed (gfx 23), $19=open (gfx 24)
				gfxIdx = int(rt) - 1
			} else if rt == 0x02 || (rt >= 0x20 && rt&0x01 != 0) {
				// Open door: horseshoe arch sprite
				if typeID >= 0x08 && typeID <= 0x0F {
					gfxIdx = 1 // type $02 - 1
				}
			} else if rt >= 0x20 && rt&0x01 == 0 {
				// Closed door: solid door sprite
				gfxIdx = 31
			}
		} else if typeID == 0x01 || typeID == 0x02 {
			// Unmanaged regular doors default to open
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
				buf.SetCellAttr(cellCol, cellRow, av)
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
		// Sword: 8 directional frames. Z80 set_sword_dir at $82C3:
		// 0=down, 1=down-right, 2=right, 3=up-left, 4=up,
		// 5=up-right, 6=left, 7=down-left
		dir := byte(0) // default: down
		if g.weaponDY < 0 {
			dir = 4 // up
		} else if g.weaponDY > 0 {
			dir = 0 // down
		} else {
			// Y=0: pure horizontal
			if g.weaponDX > 0 {
				dir = 2 // right
			} else if g.weaponDX < 0 {
				dir = 6 // left
			}
			graphicID = 0x38 + dir
			weaponAttr = 0x46
			break
		}
		// Add X component for diagonals
		if g.weaponDX > 0 {
			dir++ // right component
		} else if g.weaponDX < 0 {
			dir-- // left component (wraps via &7)
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
	g.paintEntityAttr(g.weaponX, g.weaponY, spr, weaponAttr)
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
	// Lives colour: red if immunity, cyan if infinite lives, white normally
	livesAttr := byte(0x47) // bright white
	if g.immunity {
		livesAttr = 0x42 // bright red
	} else if g.infiniteLives {
		livesAttr = 0x45 // bright cyan
	}
	g.buf.FillAttrArea(25, 15, 6, 3, livesAttr)

	// Time digits (row 8, Y=64) — custom game font from $BF4C
	cs := &data.GenCharset
	g.buf.DrawStringFrom(200, 64, formatClockShort(g.clockM, g.clockS), cs)

	// Score digits (row 10, Y=80)
	g.buf.DrawStringFrom(200, 80, formatBCD(g.score), cs)

	// Chicken energy bar (rows 11-14, Y=90-119, 30 pixel rows).
	// Z80 $8B8A: draws ChickenEmpty (bones) for depleted portion at top,
	// then ChickenFull (meat) for remaining health at bottom.
	// Sprite data stored bottom-to-top: array[0] = bottom row of sprite.
	chickenRows := int(g.energy) * 30 / int(InitialEnergy)
	if chickenRows > 30 {
		chickenRows = 30
	}
	emptyRows := 30 - chickenRows
	// Draw bones (top of chicken area): need TOP rows of ChickenEmpty,
	// which are at the END of the array (bottom-to-top storage).
	if emptyRows > 0 {
		g.buf.DrawSpriteWideOR(200, 89+emptyRows, 6, emptyRows,
			data.ChickenEmpty[(30-emptyRows)*6:])
	}
	// Draw meat (bottom of chicken area): need BOTTOM rows of ChickenFull,
	// which are at the START of the array (bottom-to-top storage).
	if chickenRows > 0 {
		g.buf.DrawSpriteWideOR(200, 119, 6, chickenRows,
			data.ChickenFull[:chickenRows*6])
	}

	// Lives (rows 15-17, Y=120-143) — up to 3 character sprites
	for i := byte(0); i < g.lives && i < 3; i++ {
		lx := 200 + int(i)*16
		sprites := data.CharacterSprites(g.character)
		// Z80 draw_lives at $A2CE: uses graphic $01/$11/$21 = LEFT-facing sprite
		g.buf.DrawSpriteXOR(lx, 139, sprites[data.DirLeft][0])
	}

	// Inventory slots (rows 3-5, Y=24-44)
	// Z80 draw_inventory at $A13B: first item at (200, 44), +16px each.
	// $A185 clears 2×20 pixel area before drawing each item.
	for i, slot := range g.inventory {
		ix := 200 + i*16
		if !slot.Occupied {
			continue
		}
		flatIdx := int(slot.ItemType) - 1
		group := flatIdx / 4
		frame := flatIdx % 4
		if group >= 0 && group < len(data.GenSpriteTable) {
			addr := data.GenSpriteTable[group][frame]
			if spr := data.GenMenuIcons[addr]; spr != nil {
				g.buf.DrawSpriteXOR(ix, 44, spr) // Y=44 from Z80 $2CC8
				attr := slot.Attr
				if attr == 0 {
					attr = 0x47 // default bright white
				}
				g.paintEntityAttr(ix, 44, spr, attr)
			}
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
	// '#' maps to a colon glyph in the custom game font (GenCharset[3]).
	// The Z80 TIME label uses '#' as the separator character.
	return string([]byte{
		'0' + m/10, '0' + m%10, '#',
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
