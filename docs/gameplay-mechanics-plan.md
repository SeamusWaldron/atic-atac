# Gameplay Mechanics Implementation Plan

## Context
Core rendering, decorations, colours, creature spawning, weapon sprites, item sprites, and player animations are working. This plan covers the remaining gameplay mechanics needed for a complete game.

Base commit: `31e972b` (or latest good state)
Reference: `aticatac.skool` Z80 disassembly

---

## Phase 1: Food Auto-Consumption
**Effort: Small | Dependencies: None**

Currently food goes to inventory on Enter key. In the original, food is auto-consumed on CONTACT — no key press needed.

### Tasks
- [ ] Remove food from inventory pickup path
- [ ] In `checkCreaturePlayerCollision` or a new `checkFoodCollision`, detect player proximity to TypeFood entities
- [ ] On contact: add $40 (64) energy, cap at $F0 (240), remove food entity, play no sound (audio not implemented)
- [ ] Verify: food items disappear when player walks over them, energy increases

### Z80 Reference
- `h_food` at $8C63: auto-consumption, adds $40 to energy
- Touch detection via $90FB
- Energy cap at $F0

---

## Phase 2: Furniture Collision
**Effort: Medium | Dependencies: None**

Players can currently walk through tables, barrels, bookcases. The original blocks movement through decorations.

### Tasks
- [ ] In `movePlayer`, after calculating newX/newY, check against decoration bounding boxes in current room
- [ ] For each decoration entity in `GenRoomEntityData[room]`, compute its bounding box from sprite width/height and position
- [ ] If player's new position overlaps a decoration's bounding box, block that axis of movement (same independent X/Y check as wall collision)
- [ ] Only block for solid decoration types (tables, barrels, bookcases, suits of armour — NOT doors, NOT shields on walls)
- [ ] Verify: player cannot walk through table in room 06, cannot walk through barrels

### Z80 Reference
- `chk_decor_move` at $900A: checks each decoration in room
- Width/height encoded in decoration attribute bytes
- Independent X and Y blocking (bits $10 and $20 in ix+$02)

---

## Phase 3: Door Cycling (Open/Close)
**Effort: Medium | Dependencies: Phase 2 (furniture collision pattern)**

Doors currently stay in their initial state. The original has doors that randomly toggle between open and closed.

### Tasks
- [ ] Add a door timer (matching $5E2E, initial value $5E = 94 frames)
- [ ] Each frame, decrement timer. When timer reaches 0:
  - Pick a random door pair in the current room
  - Toggle its state (open ↔ closed)
  - Reset timer to 94 frames
- [ ] Door state affects: which sprite is drawn (open horseshoe vs closed door) AND whether the player can pass through
- [ ] Closed doors block player movement (no door exit transition)
- [ ] Open doors allow passage (existing door exit system)
- [ ] Store door states in a map (room+door_index → open/closed)
- [ ] Randomise initial door states at game start (Z80 `randomise_doors` at $94F5: ~56% of paired doors toggled)
- [ ] Verify: doors visibly change between horseshoe (open) and solid (closed) graphics, player blocked by closed doors

### Z80 Reference
- `h_door_open` at $915F / `h_door_closed` at $917D
- Timer: $5E2E, reset to $5E (94 frames)
- `randomise_doors` at $94F5: uses ROM data as pseudo-random source
- `set_door_type` at $9260: updates both linked door entities

---

## Phase 4: Locked Door Key Checking
**Effort: Medium | Dependencies: Phase 3**

Locked doors (types $08-$0F) require matching colour key in inventory.

### Tasks
- [ ] When player tries to exit through a locked door (checkDoorExit finds a door with type $08-$0F):
  - Extract colour from door type: `type & $03` → 0=red($42), 1=green($44), 2=cyan($45), 3=yellow($46)
  - Search inventory for a key with matching colour attribute
  - If found: open the door (change type to open), allow transition, remove key from inventory
  - If not found: block transition (player stays in room)
- [ ] Key colour table from Z80 at $925C: [$42, $44, $45, $46]
- [ ] Verify: player cannot pass through coloured locked doors without matching key, key is consumed on use

### Z80 Reference
- `h_door_locked` at $9244 / `h_cave_locked` at $9252
- `check_key_colour` at $9222: extracts colour, searches inventory
- Key attrs table at $925C

---

## Phase 5: Secret Passages
**Effort: Medium | Dependencies: Phase 4 (door transition pattern)**

Each character class can pass through a specific decoration type as a shortcut between rooms.

### Tasks
- [ ] Knight passes through CLOCKS (decoration type that uses handler $942F)
- [ ] Wizard passes through BOOKCASES (handler $9428)
- [ ] Serf passes through BARRELS (handler $9421)
- [ ] When player walks into a secret passage decoration:
  - Check if current character class matches the passage type
  - If match: trigger room transition to the linked room (same as door exit)
  - If no match: block movement (treat as solid furniture)
- [ ] Identify which decoration entity types are passages from handler_table2:
  - Type $10: clock/exit handler → Knight passage
  - Type $17: bookcase handler → Wizard passage
  - Type $1A: barrel handler → Serf passage
- [ ] Use the linked entity pair to find the destination room (same as doors — XOR $08 trick)
- [ ] Verify: Knight can walk through clocks but not bookcases/barrels, etc.

### Z80 Reference
- `h_barrel` at $9421: checks if player graphic base is $21 (Serf)
- `h_bookcase` at $9428: checks if base is $11 (Wizard)
- `h_clock` at $942F: checks if base is $01 (Knight)
- Common: subtract base from player graphic, if < $10 then allowed

---

## Phase 6: Boss Creatures
**Effort: Large | Dependencies: Phase 1 (food/energy), Phase 2 (collision)**

5 unique boss monsters with individual AI behaviours.

### Tasks

#### 6a: Mummy
- [ ] Spawn in randomised red key room (from Phase key randomisation)
- [ ] AI: hunts Leaf item → moves toward leaf position in same room
- [ ] When reaching leaf: moves leaf to room $6B
- [ ] Secondary: if no leaf, patrols around red key position
- [ ] Anger mechanic: if player takes red key, Mummy chases player directly
- [ ] 8 damage per touch
- [ ] 4-frame animation, 1/4 speed
- [ ] Graphic base $70

#### 6b: Dracula
- [ ] Spawn in fixed room from init data
- [ ] AI: chases player in same room
- [ ] If player has crucifix ($8A in inventory): Dracula RUNS AWAY (inverts velocity)
- [ ] Room hopping: every 50 frames, teleports to random square room (style < 3)
- [ ] Avoids teleporting to player's room
- [ ] 8 damage per touch
- [ ] Graphic base $7C

#### 6c: Devil
- [ ] Spawn from init data
- [ ] AI: always chases player position directly
- [ ] 8 damage per touch
- [ ] Graphic base $78

#### 6d: Frankenstein
- [ ] Spawn from init data
- [ ] AI: chases player
- [ ] If player has spanner ($8B in inventory): Frankenstein dies instantly, awards 1000 points
- [ ] 8 damage per touch (if spanner not held)
- [ ] Graphic base $74

#### 6e: Hunchback
- [ ] Spawn from init data
- [ ] AI: attracted to 8 specific floor items (red key, green key, yellow key, bell, spanner, mirror, crucifix, leaf)
- [ ] When reaching item: REMOVES it from floor (clears entity)
- [ ] 16 damage per touch (double other bosses!)
- [ ] Stationary when no target item nearby
- [ ] Graphic base $9C

- [ ] Verify: each boss has correct behaviour, damage values, and item interactions

### Z80 Reference
- Mummy: $8862, Dracula: $8906, Devil: $89ED, Frankenstein: $8988, Hunchback: $8AFF
- Boss init data: $640D-$644D (16 bytes each)
- Damage routines: `damage_8` at $8A1E, `damage_16` at $8A15

---

## Phase 7: Trap Doors
**Effort: Medium | Dependencies: Phase 5 (passage pattern)**

Trap doors toggle between open and closed. Player falls through open traps.

### Tasks
- [ ] Identify trap door entity types from handler_table2 (types $18/$19)
- [ ] Trap doors toggle state based on game timer
- [ ] Open trap: player walks over → transitions to linked room (falling effect)
- [ ] Closed trap: acts as solid decoration (blocks or allows walking over)
- [ ] Visual: open trap shows hole graphic, closed shows floor graphic
- [ ] Verify: player falls through open traps, closed traps are safe

### Z80 Reference
- `h_trap_closed` at $91BC / `h_trap_open` at $91C5
- Toggle: XOR state bit, redraw graphic
- Sound: $A46E on toggle

---

## Phase 8: Item Drop & Inventory Polish
**Effort: Small | Dependencies: Phase 4**

### Tasks
- [ ] When inventory is full (3 items) and player picks up new item: automatically drop the last item at player position
- [ ] Dropped item becomes a floor entity in the current room at player X/Y
- [ ] Inventory shifts: new item goes to slot 1, others shift to 2 and 3
- [ ] Verify: picking up 4th item drops the 3rd, dropped item visible on floor

### Z80 Reference
- `drop_item` at $9358: places item at player position with Y offset $80
- Inventory management: $5E30-$5E3F

---

## Verification Protocol (after each phase)

1. `go vet ./...` — no warnings
2. `go build .` — clean compilation
3. Manual play-test of the specific mechanic
4. Screenshot comparison with original game where possible
5. Check that no previously-working mechanics have regressed
