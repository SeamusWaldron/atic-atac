# 3D View Mode — Architecture and Design

## Motivation

Inspired by a community project recreating Atic Atac in 3D (r/zxspectrum), this
feature adds a first-person 3D rendering mode that preserves the ZX Spectrum
aesthetic: 16-colour palette, flat shading, wireframe edges, chunky pixels.

The original top-down view remains the default. Players can toggle between views
with the Tab key or set a default in the settings menu.

## Architecture

The 3D renderer is a pure rendering concern — it reads game state produced by the
headless engine and produces a 256x192 RGBA frame. No game logic was modified.

```
engine.GameEnv.Step(action)
        │
        ├─ StepResult (buffer, score, lives, energy, room, player pos/dir)
        │
        ├──► screen.RenderToRGBA()     [2D path — original top-down view]
        │
        └──► render3d.Renderer.Render() [3D path — first-person view]
                │
                └──► 256×192 RGBA output
```

### Key design decisions

1. **Software rasterizer, not Ebitengine triangles.** Rendering at 256x192 with
   a Z-buffer gives us complete control over the palette (only 16 ZX Spectrum
   colours), eliminates depth-sorting complexity, and produces chunky pixels
   naturally when the image is scaled 3x.

2. **Room geometry from existing data.** The 13 room styles are defined as
   vertex lists + line connectivity in `data/roomstyles.go`. Each line segment
   is extruded into a vertical wall quad. No new geometry data was needed.

3. **Pre-computed wall cache.** Wall geometry for all 150 rooms is computed at
   startup and cached. Per-frame work is limited to camera transforms, back-face
   culling, projection, and rasterization.

4. **Camera smoothing.** Position and yaw are interpolated (30% and 25% lerp per
   frame respectively) for smooth movement. On room transitions, the camera
   snaps instantly to avoid lerping across rooms.

## Coordinate mapping

```
2D (room-local pixels)          3D (world units)
─────────────────────          ────────────────
X: 0-187 (left→right)    →    X: (px - 96) / 48
Y: 0-187 (top→bottom)    →    Z: (py - 96) / 48
                               Y: 0 (floor) to 2.0 (ceiling)
```

Player direction (DirLeft=0, DirRight=1, DirUp=2, DirDown=3) maps to yaw:
- Left → π/2 (looking -X)
- Right → -π/2 (looking +X)
- Up → π (looking -Z)
- Down → 0 (looking +Z)

## Rendering pipeline per frame

1. **Clear** — colour buffer to black (palette index 0), Z-buffer to +∞
2. **Camera update** — lerp position/yaw toward targets, recompute sin/cos
3. **Wall rendering** — for each wall quad in current room:
   - Back-face cull (dot product of normal with camera→quad vector)
   - Transform 4 vertices: world → camera space → screen space
   - Skip if any vertex behind near plane (simple clip)
   - Fill quad as 2 triangles with Z-interpolated scanlines
   - Draw edges with bright palette variant
4. **Entity billboards** — for each active entity in room:
   - Look up sprite data (GenSpriteTable → GenMenuIcons)
   - Project billboard centre to screen
   - Draw sprite pixel-by-pixel, sampling original bitmap
   - Fallback to coloured block if no sprite data
5. **Door markers** — draw door outlines at door positions with type-based colour
6. **HUD overlay** — energy bar, lives indicators (drawn at Z=0, always on top)
7. **Palette → RGBA** — convert 8-bit palette indices to RGBA using Spectrum palette

## Package structure

```
render3d/
  geom.go       — Vec3, Quad types + vector math
  camera.go     — first-person camera with projection + smoothing
  room.go       — room style → 3D wall quads + WallCache
  raster.go     — software rasterizer (scanline fill, Z-buffer, Bresenham lines)
  billboard.go  — sprite billboard rendering (reads original sprite data)
  renderer.go   — orchestration: Render() → RGBA output
```

## Integration points

- `game/game.go` — Tab key toggles `viewMode3D`; Draw() switches rendering path
- `game/menu.go` — "3D VIEW" setting in settings menu; `View3D` bool in MenuState
- `engine/engine.go` — new accessor methods (PlayerX/Y/Dir, Inv, RoomDoorsMap, Frame)
- `engine/observation.go` — StepResult extended with PlayerX/Y/Dir

## ZX Spectrum aesthetic constraints

- Only 16 colours used (8 normal + 8 bright from Spectrum palette)
- Walls filled with room's ink colour; edges in bright variant
- No textures, no lighting, no gradient — flat shading only
- Render at native 256×192, scale 3x — matches original display
- Black background (no skybox, no floor texture)
- Entity sprites rendered pixel-for-pixel from original bitmap data

## Room style coverage

| Style | Shape | 3D appearance |
|-------|-------|--------------|
| 0 | Plain square | Box room |
| 1 | Cave square | Irregular cavern (64 vertices) |
| 2 | Octagonal | Octagonal chamber |
| 3 | Wide rectangle | Wide corridor |
| 4 | Tall rectangle | Narrow corridor |
| 5-8 | Stairs (4 orientations) | Stepped corridor walls |
| 9 | Wide cave | Wide irregular cavern |
| 10 | Tall cave | Tall irregular cavern |
| 11 | Final room | Two parallel walls |
| 12 | Trapdoor tunnel | Concentric squares (12 nested) |

## Performance

At 256×192 (49,152 pixels), the software rasterizer handles typical rooms
(10-60 wall quads + handful of billboards) well within the 20ms frame budget.
The Z-buffer is 192KB (49K × 4 bytes) — trivial for modern hardware.

## Future work

- Textured walls using Spectrum attribute patterns
- Smooth walking camera bob
- Transition effects (door opening animation)
- Floor/ceiling rendering with room colour
- Minimap overlay in 3D mode
