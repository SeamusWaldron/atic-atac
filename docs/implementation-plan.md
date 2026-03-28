# Atic Atac Go Implementation Plan

## Technology Stack
Same as Manic Miner and Jetpac:
- **Language:** Go
- **Graphics:** Ebitengine (ebiten/v2)
- **Audio:** oto/v3 directly (NOT Ebitengine audio)
- **Persistence:** JSON config (~/.aticatac/config.json)

## Package Structure

```
atic-atac/
├── main.go
├── Makefile
├── action/
│   └── action.go            # Action{Up, Down, Left, Right, Fire, Pickup, Enter, Escape}
├── engine/
│   ├── engine.go            # Headless GameEnv with Step/Reset
│   ├── observation.go       # Full state snapshot
│   ├── constants.go
│   └── engine_test.go
├── entity/
│   ├── player.go            # Player: movement, energy, weapon, death
│   ├── creature.go          # Enemies with handler dispatch
│   ├── item.go              # Keys, collectibles, food
│   ├── weapon.go            # Fireball/sword/axe projectiles
│   ├── door.go              # Door navigation + key checks
│   └── explosion.go         # Death/hit animations
├── room/
│   ├── room.go              # Room struct + rendering
│   ├── map.go               # Room connectivity, door network
│   └── data.go              # All room definitions extracted from ASM
├── screen/
│   ├── buffer.go            # ZX Spectrum buffer system
│   ├── renderer.go          # Buffer → Ebitengine image
│   ├── sprites.go           # Variable-size sprite drawing
│   └── text.go              # ZX Spectrum ROM font
├── audio/
│   └── audio.go             # oto direct, SFX
├── data/
│   ├── sprites.go           # All sprite data
│   ├── characters.go        # Character class definitions
│   └── items.go             # Key/item init data
├── config/
│   └── config.go            # Settings, high scores
├── input/
│   └── input.go             # Keyboard → Action
├── game/
│   ├── game.go              # Ebitengine wrapper
│   ├── menu.go              # Main menu (character select)
│   ├── hud.go               # Energy bar, inventory, score, clock, lives
│   ├── settings.go
│   ├── highscore.go
│   └── help.go
└── docs/
```

## Implementation Phases

### Phase 1: Core Rendering + Room System
- ZX Spectrum buffer system (reuse from Manic Miner)
- Room data extraction (room_table pointers → room definitions)
- Room rendering (frame, decorations, platforms/walls)
- Room navigation (draw room on entry)
- Door rendering

### Phase 2: Player Movement
- 8-directional movement (top-down, not platformer)
- Walking animation (walk_counter)
- Collision with room walls/furniture
- Door entry/exit (in_doorway, door_timer)
- Character class selection (Wizard/Knight/Serf)

### Phase 3: Entity System
- Entity data structure (8 bytes each)
- Handler dispatch via handler_table
- Entity room filtering (only process entities in current room)
- Entity rendering (lookup_graphic → draw sprite)

### Phase 4: Creatures
- Creature spawning and AI
- Multiple creature types
- Collision with player (energy drain)
- Creature delay system (game speed stabilisation)

### Phase 5: Combat
- Weapon firing (character-specific: fireball/sword/axe)
- Projectile movement and collision
- Enemy death (explosion animation)
- Score for kills

### Phase 6: Items & Inventory
- Item pickup (3 inventory slots)
- Item drop
- Key system (green/red/cyan/yellow + ACG key pieces)
- Door unlocking (check inventory for matching key)
- Food system (chicken cooking stages, energy restore)
- Collectibles (score bonus)

### Phase 7: Game Progression
- Energy decay over time
- Clock (hours/minutes/seconds)
- Lives system
- Room visit tracking (visited_percent)
- ACG key assembly (3 pieces) → escape → win
- Secret passages (character-specific)
- Score and high score

### Phase 8: Audio
- Movement sounds
- Weapon fire sounds
- Enemy explosion sounds
- Pickup sounds
- Door sounds
- Death sound
- Victory sound

### Phase 9: Menu & Polish
- Character selection menu
- Title screen
- Game over screen
- Settings, high scores, help (reuse from Manic Miner patterns)

## Key Technical Challenges

1. **Room system**: ~100+ rooms with interconnecting doors. Need an efficient data structure for the room map and door network. Each room has unique decorations and layout.

2. **Entity handler dispatch**: 166+ handler entries in a table. Need to map these to Go functions. Could use a function table or switch statement.

3. **Top-down collision**: Unlike Manic Miner's cell-based checks, Atic Atac needs pixel-level collision in 2D (not just below feet).

4. **Inventory UI**: Need to display 3 inventory slots with item graphics, plus handle pickup/drop mechanics.

5. **Energy system**: Continuous energy drain + damage from enemies + food restoration. Visual energy bar (chicken leg that depletes).

6. **Map complexity**: Non-linear room map with coloured key gates. Need to track which keys the player has and which doors they can open.

7. **Data extraction**: 14,000+ lines of SkoolKit format. Room definitions, sprite data, handler tables — much more data than Manic Miner's 20 caverns.

## Risk Mitigation (from Manic Miner lessons)

- Start with room rendering (get something on screen fast)
- Player movement second (most bug-prone area)
- Use the original coordinate system exactly
- Don't guess room layouts — extract from the data tables
- Audio last (use oto from day one, don't touch Ebitengine audio)
- Test with human player after every major feature
- Commit frequently
