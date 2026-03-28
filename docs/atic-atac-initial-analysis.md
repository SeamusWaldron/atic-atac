# Atic Atac — Initial Source Analysis

## Source Format

Three source files available:
- `aticatac.skool` — SkoolKit format, 14,056 lines, 1,432 labels (primary reference)
- `aticatac.ctl` — SkoolKit control file, 4,583 lines
- `original/aticatac.asm` — Plain Z80 ASM, 12,041 lines (alternative reference)

Disassembled by Simon Owen ("obo"). Both the `.skool` and `.asm` files are fully annotated.

## Game Overview

Atic Atac (1983) by Tim and Chris Stamper (Ultimate Play the Game / Ashby Computers & Graphics) for the ZX Spectrum.

**Gameplay:** Top-down adventure game. The player explores a haunted castle with interconnected rooms, collecting keys and items to find the ACG (Ashby Computers & Graphics) key to escape. Three playable characters (Wizard, Knight, Serf), each with a unique weapon and secret passages.

**Key mechanics:**
- Room-based map (not scrolling — each room is a distinct screen)
- Door navigation between rooms (4 possible exits per room)
- Inventory system (carry up to 3 items)
- Energy system (health depletes over time and from enemies)
- Food items restore energy (chicken leg that cooks over time)
- Multiple key colours to unlock specific doors
- Creature AI (different enemy types per room)
- Character-specific secret passages
- Score system (BCD)
- Clock (hours/minutes/seconds — time limit)

## Memory Layout ($5E00-$5E56: Game Variables)

### Core State
| Address | Label | Description |
|---|---|---|
| $5E00 | menu_selection | Current menu choice |
| $5E03 | last_FRAMES | Last frame counter value |
| $5E05 | rand8 | Random number (low 8 bits) |
| $5E14 | game_flags | Bit 0: room content drawn |
| $5E1A | room_attr | Current room attribute colour |
| $5E1B | room_ptr | Pointer to current room data |
| $5E1D | room_width | Room width |
| $5E1E | room_height | Room height |

### Player State
| Address | Label | Description |
|---|---|---|
| $5E21 | lives | Lives remaining |
| $5E28 | player_energy | Current energy (health bar) |
| $5E2A | score_bcd | 3-byte BCD score |
| $5E2D | in_doorway | Currently in a doorway |
| $5E2E | door_timer | Door transition timer |
| $5E2F | walk_counter | Walk animation counter |

### Inventory ($5E30-$5E3B)
3 inventory slots, 4 bytes each:
| Offset | Description |
|---|---|
| 0 | Item graphic ID |
| 1 | Item room number |
| 2 | Item X position |
| 3 | Item Y position |

### Clock
| Address | Label | Description |
|---|---|---|
| $5E3D | clock_hours | Hours |
| $5E3E | clock_minutes | Minutes |
| $5E3F | clock_seconds | Seconds |

### Room Tracking
| Address | Label | Description |
|---|---|---|
| $5E40 | visited_rooms | Bitfield of visited rooms (20 bytes) |
| $5E54 | visited_percent | Percentage of castle explored |

## Entity System

Entities are 8 bytes each, stored in a block from $EAA8. The main loop iterates through all entities, checking if each is in the current room, and dispatching to the appropriate handler.

### Entity Structure (8 bytes)
| Offset | Description |
|---|---|
| 0 | Handler/type ID |
| 1 | Room number |
| 2 | X position |
| 3 | Y position |
| 4 | Graphic ID |
| 5 | Colour attribute |
| 6-7 | Type-specific data |

### Entity List Ranges
- $EAA8-$EE5F: Regular entities (creatures, items)
- $EE60: End marker
- $EEE0: Linked entity pairs (doors, etc.)

## Room System

### Room Table ($6C17, label `room_table`)
An array of 2-byte pointers to individual room definitions. Each room definition contains:
- Room dimensions and attribute colour
- Door positions and destinations
- Decoration/furniture sprites and positions
- Platform/wall layout data

### Door System
Doors are defined as linked pairs with source and destination room numbers. The game has an extensive door network connecting ~100+ rooms across multiple floors (basement, ground, first floor, attic).

Door definitions include:
- Source room number
- Destination room number
- Door type (normal, coloured key required, secret passage)
- X/Y position in source room
- X/Y position in destination room

## Handler/Dispatch System

The main loop uses TWO handler tables:
- `handler_table` ($7EE6): 166 entries (83 word pairs) — primary entity handlers
- `handler_table2` ($802A): Secondary handler table

Each entity's byte 0 is used as an index into the handler table. The handler routine performs the entity's per-frame update (movement, animation, collision, etc.).

### Key Handler Addresses
| Address | Description |
|---|---|
| $807A | Creature delay (keeps game speed stable) |
| $80D2 | Generic creature handler |
| $81DB | Weapon projectile handler |
| $81F0 | Item pickup handler |
| $82F1 | Food/consumable handler |
| $8301 | Door handler |
| $845F | Specific creature type |
| $8DC4 | Another creature handler |
| $8E26 | Inactive creature |
| $93E3 | Special entity |

## Graphics System

### Sprite Lookup
`lookup_graphic` at $7E91 resolves a graphic ID to sprite data. Graphics are stored as variable-sized blocks with width, height, and pixel data.

### Room Drawing
`draw_room` at $7E23 draws the current room:
1. Looks up room data from `room_table`
2. Draws room frame/border (`draw_room_frame` at $A240)
3. Draws room decorations via `decor_loop`
4. Draws doors
5. Draws entities in the room

### Play Area
The play area is cleared via `clear_play_area` ($8093) which writes zeros to the display file from $4000, covering 24 columns × 192 rows.

## Character Classes

Three playable characters, selectable at game start:
1. **Wizard** — shoots fireballs, can use magic passages
2. **Knight** — throws swords, can use armour passages
3. **Serf** — uses axes, can use servant passages

Each character has:
- Unique weapon sprite and behaviour
- Unique secret passage type (character-specific shortcuts)
- Same base stats (energy, speed)

## Item System

### Key Items
| Label | Graphic | Room | Colour | Purpose |
|---|---|---|---|---|
| acg_key_init | $8C-$8E | Various | $46 | 3-piece key to escape castle |
| green_key_init | $81 | $05 | $44 | Opens green doors |
| red_key_init | $81 | $17 | $42 | Opens red doors |
| cyan_key_init | $81 | $53 | $45 | Opens cyan doors |
| yellow_key | $81 | $66 | $46 | Opens yellow doors |

### Collectible Items
Leaf, crucifix, spanner, wine, coin — each with a fixed room and position. Collecting these increases score.

### Food System
Food items (chicken legs) appear at random. The `chicken_level` variable tracks the cooking state — food goes from raw to cooked to burnt. The `food_ptr` tracks the current food spawn.

## Main Loop Architecture

```
main_loop ($7DC3):
  1. Reset stack pointer to $5E00
  2. Enable interrupts
  3. Clear creature counter
  4. If room not drawn yet → draw linked entities (doors) → draw room
  5. Frame tick check (SYSVAR_FRAMES vs last_FRAMES)
  6. For each entity ($EAA8 to $EE60, 8 bytes each):
     a. Check if entity is in current room
     b. If yes → dispatch to handler via handler_table
     c. Handler returns to loop_return
  7. Process linked entity pairs ($EEE0+, 16 bytes each)
  8. Draw room (if needed)
  9. Process player input and movement
  10. Loop back to step 5
```

The frame rate is tied to SYSVAR_FRAMES (50 Hz PAL interrupt), similar to Jetpac.

## Key Differences from Manic Miner and Jetpac

| Aspect | Manic Miner | Jetpac | Atic Atac |
|---|---|---|---|
| View | Side-on platformer | Side-on shooter | Top-down adventure |
| Movement | Tile-based, gravity | Free-form flying | 8-directional walking |
| Levels | 20 fixed caverns | Procedural platforms | ~100+ room map |
| Screen | Single screen | Single screen, wrapping | Room-based transitions |
| Combat | Avoid enemies | Laser beams | Character-specific weapons |
| Items | Collect all → portal | Fuel → rocket launch | Keys → doors → escape |
| Inventory | None | Carry one item | 3 slots |
| Health | Binary (alive/dead) | Lives only | Energy bar + lives |
| Score | ASCII digits | 3-byte BCD | 3-byte BCD |
| Time | Air supply per level | None | Clock (time limit) |
| Characters | Willy only | Jetman only | 3 classes |
| Map | Linear (cavern 1-20) | Sequential planets | Non-linear interconnected |

## Complexity Assessment

Atic Atac is significantly more complex than both Manic Miner and Jetpac:
- **~1,432 labels** vs Manic Miner's ~200 and Jetpac's ~326
- **14,056 lines** vs Manic Miner's ~12,000 and Jetpac's ~6,000
- **Room-based map** with ~100+ rooms, doors, keys, secret passages
- **Entity system** with handler dispatch tables (166+ handlers)
- **Inventory system** with pickup/drop mechanics
- **Energy/health** system with food and decay
- **Three character classes** with different abilities
- **Clock/time limit** adding urgency

Estimated implementation effort: 2-3x Manic Miner.
