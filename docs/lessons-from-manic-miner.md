# Lessons from Manic Miner — Applied to Jetpac

These are hard-won lessons from building a Go replication of Manic Miner from Z80 assembly. Every one of these was learned through bugs, rewrites, or wasted effort.

## Critical Rules

### 1. NEVER guess mechanics — read the original code
Every time we guessed (jump height, HUD layout, air bar behaviour, music timing, frame rate), we got it wrong. The Z80 source is the ONLY source of truth. Even when the user provides reference screenshots, you need the assembly to understand WHY things work the way they do.

### 2. Use the original coordinate system EXACTLY
Manic Miner stores Willy's Y position as y*2 (doubled). We initially used pixel Y and applied the doubled deltas, causing jumps to be double height and landing checks to fail at non-aligned positions. This caused 6 cascading bugs. Use whatever bizarre coordinate system the original uses — don't "simplify" it.

### 3. Replicate the original buffer/memory layout
The ZX Spectrum has a non-linear display memory layout (interleaved thirds) and a separate attribute byte system. Game logic depends on attribute comparisons for collision detection. We replicated the dual buffer system exactly, and converted to modern rendering only at the final display boundary.

### 4. Decode display addresses from the assembly
HUD positions, sprite locations, air bar placement — all are specified as absolute addresses in the assembly. Decode them properly using the Spectrum address format (`010TTRRR CCCXXXXX`). Don't estimate pixel positions.

### 5. Audio is harder than gameplay — expect 6+ iterations
Our audio went through: wrong frequencies (ultrasonic clicks), wrong tempo (frame-driven vs stream-driven), wrong articulation (sustained vs staccato), wrong latency (Ebitengine's 500ms buffer), wrong pitch (frequency multiplier instead of tempo), music hanging on toggle. Final solution: bypass Ebitengine audio entirely, use oto directly with a 4096-byte buffer.

### 6. Separate engine from renderer from day one
Build a headless `GameEnv` with `Step(action) → observation` API before adding graphics. This enables testing, AI training, and clean architecture. The Ebitengine wrapper should be a thin layer that reads keyboard → Action, calls Step, renders the returned buffers.

### 7. Frame rate is determined by CPU execution time, not vsync
The Spectrum has no vsync in gameplay (interrupts are disabled). The frame rate equals 3,500,000 / total_T_states_per_loop. Count the T-states of the main loop to determine the correct FPS. For Manic Miner this was ~15 FPS with music, ~17 FPS without.

### 8. Test with a human player continuously
Every major bug was caught by the user playing, not by automated tests. After every few commits, have someone play and report what looks/sounds/feels wrong.

### 9. The draw order matters for collision detection
Manic Miner draws Willy BEFORE guardians. Guardian blend mode (AND check) detects overlap with Willy's already-drawn pixels. If drawn in the wrong order, collisions are never detected.

### 10. Feature flags > code modification for cheats
Implement POKEs as boolean flags checked at the appropriate point in the game logic. Much cleaner than modifying code, and they can be toggled through a settings UI.

## Common Pitfalls

| Pitfall | What happened | How to avoid |
|---|---|---|
| Wrong sprite draw mode | Willy drawn with collision-detecting blend; disappeared on floors | Three draw modes: Overwrite (portals), Blend (guardians, collision), OR (player) |
| Frame-driven audio | Music one note per game frame = too slow | Audio stream manages its own note timing internally |
| Per-sample mutex locking | 44,100 lock/unlock per second killed performance | Lock once per Read() call, not per sample |
| Shared counter for music + animation | Changing music speed also changed lives animation speed | Separate counters for audio and visual animation |
| Death animation too long | 32 frames at 16 FPS = 2 seconds; original is 0.12 seconds | Count T-states: 415,000 / 3,500,000 = 0.12s |
| Air bar moved with depleted area | Background was recalculated each frame based on current air | Background is FIXED (set once), only the gauge pixels change |
| Title screen corrupted | Raw Spectrum display data copied to linear buffer | Write SpectrumDisplayToLinear() conversion function |
| Settings remnants on screen | Switching states didn't clear the display | Always clear display (Fill black) before rendering each state |

## Architecture Template

```
game-name/
├── main.go                  # Ebitengine entry point
├── Makefile                 # CGO_CFLAGS for macOS warnings
├── action/action.go         # Pure input type (leaf package, no deps)
├── engine/
│   ├── engine.go            # Headless GameEnv with Step/Reset
│   ├── observation.go       # State snapshot struct
│   ├── constants.go         # Game-logic constants
│   └── engine_test.go       # Determinism + basic tests
├── entity/                  # Game objects (player, enemies, items)
├── screen/
│   ├── buffer.go            # ZX Spectrum buffer layout + YTable
│   ├── draw.go              # Tile → buffer rendering
│   ├── renderer.go          # Buffer → Ebitengine image (attribute colours)
│   ├── sprites.go           # Sprite drawing with collision detection
│   └── text.go              # ZX Spectrum ROM font
├── audio/audio.go           # Direct oto with small buffer (~12ms)
├── data/                    # Extracted sprite/level/music data
├── config/config.go         # Persistent settings + high scores (JSON)
├── input/input.go           # Keyboard → Action with control schemes
├── game/
│   ├── game.go              # Ebitengine wrapper (thin)
│   ├── settings.go          # Settings screen
│   ├── highscore.go         # High score table + name entry
│   ├── help.go              # Help/instructions screen
│   └── cheat.go             # Cheat codes
└── docs/                    # Analysis, plans, journal
```

## Audio Architecture (use from day one)

```go
// Use oto directly, NOT Ebitengine audio.
import "github.com/ebitengine/oto/v3"

ctx, ready, _ := oto.NewContext(&oto.NewContextOptions{
    SampleRate:   44100,
    ChannelCount: 2,
    Format:       oto.FormatFloat32LE,
    BufferSize:   20 * time.Millisecond,
})
<-ready
player := ctx.NewPlayer(stream)
player.SetBufferSize(4096) // Critical: default is 256KB = 740ms latency!
player.Play()
```

The stream's Read() method generates square waves. Lock once per Read() call, not per sample. For music, manage note timing internally in the stream (don't rely on game frame rate).

## Data Extraction

Write a Python script to extract binary data from the assembly source. Verify byte counts match expected sizes. The SkoolKit format (.skool) is different from standard Z80 ASM — it uses `defb`, `defw`, `defs` instead of `DB`, `DW`, `DS`.
