# 3D View Mode — Implementation Plan

## Overview

Add a first-person 3D rendering mode to Atic Atac that preserves the ZX Spectrum
aesthetic. The player can toggle between the original top-down view and a 3D
first-person view. A settings preference controls the default view on game start.

Rollback tag: `pre-3d-mode`

## Architecture

The engine is already headless — `GameEnv.Step()` produces game state, and
rendering is a separate concern in `game/game.go` + `screen/`. The 3D renderer
is a parallel rendering path that reads the same game state.

```
engine.GameEnv.Step(action) → StepResult
                                  │
                     ┌────────────┴────────────┐
                     ▼                          ▼
            screen.RenderToRGBA()       render3d.Renderer.Render()
            (top-down 2D)               (first-person 3D)
                     │                          │
                     └────────────┬─────────────┘
                                  ▼
                        game.Game.Draw() → Ebitengine
```

### Rendering approach: Software rasterizer at 256x192

- Render to a 256x192 pixel buffer with a Z-buffer
- Use only the 16-colour ZX Spectrum palette
- Low resolution gives chunky pixels when scaled up (3x)
- Flat-shaded wall polygons with bright-coloured edges
- Black floor/ceiling
- Billboard sprites for entities

This matches the aesthetic in the Reddit reference (flat colours, wireframe
edges, no textures, no lighting).

## Phase 1: Engine Exports

**Goal:** Expose game state needed by the 3D renderer without modifying engine logic.

### Changes to `engine/engine.go`:
- Add `PlayerX() byte` — returns playerX
- Add `PlayerY() byte` — returns playerY
- Add `PlayerDir() int` — returns playerDir
- Add `PlayerMoving() bool` — returns moving
- Add `Inventory() [3]InvSlot` — returns inventory copy
- Add `RoomDoors() map[byte][]data.RoomDoor` — returns roomDoors
- Add `Frame() uint32` — returns frame counter

### Changes to `engine/observation.go`:
- Add `PlayerX, PlayerY byte` to StepResult
- Add `PlayerDir int` to StepResult
- Populate in `Step()` return

No engine logic changes. Only new accessor methods and StepResult fields.

## Phase 2: 3D Geometry Pipeline

**Goal:** Convert 2D room definitions into 3D wall geometry.

### New package: `render3d/`

#### `render3d/geom.go` — 3D geometry types
```go
type Vec3 struct{ X, Y, Z float32 }
type Quad struct {
    Verts    [4]Vec3     // 4 corners (CCW winding)
    Color    byte        // ZX Spectrum palette index (fill)
    EdgeColor byte       // palette index (edge lines)
}
```

#### `render3d/room.go` — Room style → 3D walls
- `BuildRoomWalls(style data.RoomStyle, attr data.RoomAttr) []Quad`
- For each line segment in the room style, extrude into a vertical wall quad
- 2D coords (0-187 pixel range) → 3D coords:
  - X_3d = (point.X - roomCentreX) / scale
  - Z_3d = (point.Y - roomCentreY) / scale
  - Y_3d = 0 (floor) to wallHeight (ceiling)
- Pre-compute wall geometry for all 13 room styles on init
- Wall colour = room attribute ink colour
- Edge colour = bright variant of ink colour

#### `render3d/camera.go` — First-person camera
```go
type Camera struct {
    X, Y, Z float32     // position in 3D world
    Yaw     float32     // rotation around Y axis (radians)
    FOV     float32     // horizontal field of view
    Near    float32     // near clip
    Far     float32     // far clip
}
```
- `(c *Camera) Project(v Vec3) (screenX, screenY int, depth float32, visible bool)`
- Maps player direction (4 cardinal dirs) to yaw angle
- Smooth interpolation between yaw targets for direction changes

## Phase 3: Software 3D Renderer

**Goal:** Rasterize 3D walls into a 256x192 RGBA buffer using ZX Spectrum palette.

#### `render3d/raster.go` — Triangle rasterizer with Z-buffer
```go
type Raster struct {
    Width, Height int
    ColorBuf      []byte    // palette index per pixel
    ZBuf          []float32 // depth per pixel
}
```
- `(r *Raster) Clear()` — fill colour with black, Z with +inf
- `(r *Raster) FillTriangle(v0, v1, v2 [3]float32, color byte)` — scanline fill
  - v0/v1/v2 are (screenX, screenY, depth) from projection
  - Z-interpolation per scanline for correct depth
- `(r *Raster) DrawLine3D(v0, v1 [3]float32, color byte)` — Bresenham with Z-test
- `(r *Raster) ToRGBA(out []byte)` — convert palette indices → RGBA using Spectrum palette

#### `render3d/renderer.go` — Orchestration
```go
type Renderer struct {
    raster    Raster
    wallCache map[byte][]Quad  // room style → pre-built walls
    camera    Camera
    rgbaOut   []byte           // 256*192*4 output
}
```
- `(r *Renderer) Render(env RenderState) []byte`
  - RenderState contains: room, playerX/Y/dir, entities, doors, room attrs
  - Clear Z-buffer
  - Transform + clip + rasterize wall quads for current room
  - Draw quad edges on top (wireframe look)
  - Draw entity billboards
  - Draw HUD overlay
  - Convert to RGBA
  - Return pixel buffer

## Phase 4: Entity Billboards + Doors

**Goal:** Render game entities as camera-facing sprites in 3D space.

#### `render3d/billboard.go`
- `RenderBillboard(r *Raster, cam *Camera, worldPos Vec3, spriteData []byte, attr byte)`
- Convert entity position (room-local pixel coords) to 3D world position
- Project sprite corners to screen space (always face camera)
- Draw sprite pixel-by-pixel using Z-buffer:
  - For each row of the sprite bitmap
  - For each set bit, write the ink colour at interpolated depth
  - Unset bits are transparent (skip)
- Sprite data format: first byte = height, then 2-byte-wide rows

#### Door rendering
- Doors are rendered as coloured rectangles on the wall nearest their position
- Door colour matches door type (red, green, cyan, yellow, blue)
- Open doors rendered differently (archway outline vs solid)

## Phase 5: Integration + Settings

**Goal:** Wire the 3D renderer into the game with toggle and settings.

### Changes to `game/game.go`:
- Add `viewMode3D bool` field to Game struct
- Add `renderer3d *render3d.Renderer` field
- In `Draw()`:
  ```go
  if g.viewMode3D && g.eng.State() == engine.StatePlaying {
      pixels := g.renderer3d.Render(...)
      copy(g.pixels, pixels)
  } else {
      screen.RenderToRGBA(g.result.Buffer, g.pixels)
  }
  ```
- In `Update()`: Tab key toggles viewMode3D
- 3D mode only active during StatePlaying (menu/death/win use 2D)

### Changes to `game/menu.go`:
- Add `View3D bool` to MenuState (persists across games)
- Add "VIEW MODE" option to settings menu (index 5, after COLOUR CLASH)
  - Displays "TOP DOWN" or "3D"
  - Toggle sets `g.viewMode3D` on game start
- Add "3D" / "TOP DOWN" label

### Changes to `engine/constants.go`:
- No changes needed — view mode is purely a rendering concern

## Phase 6: Polish + Documentation

**Goal:** Refine visuals and update project documentation.

### Visual polish:
- HUD overlay in 3D mode (energy bar, lives, score, inventory drawn as 2D overlay)
- Room transition: brief black flash when changing rooms (matches original)
- Smooth camera yaw interpolation (lerp over 4-6 frames on direction change)
- Flash attribute support in 3D mode (swap ink/paper every 16 frames)

### Documentation:
- Update `docs/thesis/` with 3D mode architecture notes
- Document the rendering pipeline, coordinate mapping, and design decisions

## File manifest

New files:
```
render3d/
  geom.go       — Vec3, Quad, matrix types
  room.go       — 2D room style → 3D wall geometry
  camera.go     — first-person camera + projection
  raster.go     — software triangle rasterizer + Z-buffer
  billboard.go  — entity billboard rendering
  renderer.go   — orchestration + public API
docs/3d-mode-implementation-plan.md  (this file)
docs/thesis/3d-mode.md
```

Modified files:
```
engine/engine.go       — new accessor methods
engine/observation.go  — extended StepResult
game/game.go           — toggle + 3D Draw path
game/menu.go           — VIEW MODE setting
```

## Constraints

- ZX Spectrum palette only (16 colours)
- Render at 256x192, scale 3x (matches existing behaviour)
- No textures, no lighting — flat shading + wireframe edges
- Game logic unchanged — 3D is purely a rendering concern
- 2D mode must remain fully functional and be the default
- Performance: must maintain 50 TPS at 256x192 (trivial for software raster)
