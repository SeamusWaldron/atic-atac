# Pattern: Optional Per-Pixel Colour Attributes for Spectrum Conversions

A reusable recipe for adding a runtime-switchable "no colour clash" mode to a
Go-based ZX Spectrum game conversion, without touching art, palette, BRIGHT,
FLASH, or game logic. Proven in the `atic atac` conversion — this document is
the generalised form, ready to drop into your other Spectrum ports.

## What it does

The ZX Spectrum stores one ink/paper/bright/flash attribute byte per 8×8
character cell, which is the cause of colour clash: whenever a sprite and a
background share a cell, they have to share a single ink+paper pair. This
pattern widens the attribute buffer to one byte per pixel and routes sprite
attribute writes through a mask-aware primitive so sprites only affect the
pixels they actually occupy. A global `Mode` flag selects between authentic
cell behaviour and the new per-pixel behaviour; the toggle is live and the
next redraw converges the display.

Because the per-pixel buffer is a strict superset of the cell buffer,
authentic mode is byte-identical to the pre-change output. That makes the
migration safe to ship in stages and easy to regression-test with screenshot
diffs.

## Non-goals

- No new art, no remastering, no true-colour palettes.
- No change to game logic, entity behaviour, or room layouts.
- No change to save formats, ROM data, or extracted assets.
- No per-sprite palette overrides beyond what the attribute byte already
  encodes. BRIGHT and FLASH keep their original semantics and gain per-pixel
  granularity for free.

## Starting point assumptions

This pattern assumes your conversion already has:

1. A `screen.Buffer` (or similar) with separate `Pixels` and `Attrs` arrays,
   the `Attrs` array being one byte per 8×8 cell (768 bytes for a full
   Spectrum screen).
2. A renderer that walks the cell grid, reads one attribute per cell, and
   emits RGBA by picking ink or paper per pixel bit.
3. Sprite blit helpers that are separate from attribute writes: the pixel
   blit draws the 1-bit mask, and a follow-up call sets a cell attribute.
4. Background fills that also go through cell-level attribute writes.

Atic Atac matched all four; so will any conversion that models the
Spectrum's display memory faithfully.

## The design

### 1. Mode selector

Add an enum and a package-global in your `screen` package:

```go
type ColourMode int

const (
    ColourModeAuthentic ColourMode = iota // one attr per 8×8 cell (default)
    ColourModePerPixel                    // one attr per pixel
)

var Mode = ColourModeAuthentic
```

Global rather than per-buffer: every attribute writer and the renderer need
to agree on the interpretation, and switching mid-frame isn't a supported use
case. Switching *between* frames is fine — the next full redraw converges.

### 2. Widen the attribute buffer

```go
const AttrSize = ScreenWidthPx * ScreenHeightPx // was 768

type Buffer struct {
    Pixels [DisplaySize]byte
    Attrs  [AttrSize]byte
}

func AttrPixelAddr(x, y int) int { return y*ScreenWidthPx + x }
```

Memory cost is ~48 KB per buffer. Irrelevant on any host that can run Go +
Ebiten; flag it only if you're targeting embedded.

Delete any standalone `AttrAddr(x, y)` helper that returned a *cell* index —
callers would silently get wrong offsets into the new wider array. Fail
loudly at compile time instead.

### 3. Two attribute-write primitives

All attribute writes funnel through two helpers. The rest of the game never
touches `Attrs` directly.

```go
// SetCellAttr writes one 8×8 cell's worth of attribute (64 pixels).
// Used by background fills and any caller that currently writes
// buf.Attrs[row*Cols+col] = attr.
func (b *Buffer) SetCellAttr(col, row int, attr byte)

// FillAttrArea fills a rectangular region of cells. Reimplemented on top
// of SetCellAttr / a pixel-rect helper. Mode-independent: both modes
// stamp every pixel in the rect.
func (b *Buffer) FillAttrArea(col, row, w, h int, attr byte)

// StampSpriteAttr writes a sprite's attribute into the buffer. In
// authentic mode it fills the cells the sprite covers (matching the
// original ROM rule for set_entity_attrs). In per-pixel mode it stamps
// only at the pixels where the sprite bitmap has a set bit, so the
// sprite does not clobber surrounding background attributes.
func (b *Buffer) StampSpriteAttr(x, y int, spr []byte, attr byte)
```

`StampSpriteAttr` needs the sprite bitmap in whatever format your blitters
consume. In Atic Atac all entity sprites are 2-byte-wide with a height byte
first, drawn upward from `y`; the per-pixel branch iterates rows, reads the
two mask bytes, and stamps the attribute at `(x+bit, y-row)` for every set
bit. Adapt the decode to your game's sprite format — the mask logic is the
same shape regardless.

**Critical authentic-mode behaviour:** `StampSpriteAttr` in authentic mode
must reproduce your game's existing cell-write rule *exactly*. Atic Atac
uses the Z80 `set_entity_attrs` rule (2 cells wide, 3 if the sprite
straddles a byte boundary; height computed by `((h>>2)+1)>>1` etc). Your
game will have its own rule — preserve it verbatim, and verify with a
screenshot diff in authentic mode before touching anything else.

### 4. Renderer: attribute lookup per pixel

Move the attribute read inside the pixel loop:

```go
for y := 0; y < ScreenHeightPx; y++ {
    rowBase := yTable[y]
    attrRowBase := y * ScreenWidthPx
    for charCol := 0; charCol < ScreenCols; charCol++ {
        pixByte := buf.Pixels[rowBase+uint16(charCol)]
        for bit := 0; bit < 8; bit++ {
            x := charCol*8 + bit
            attr := buf.Attrs[attrRowBase+x]
            // decode ink/paper/bright/flash unchanged
            // emit RGBA from Palette[ink] or Palette[paper]
        }
    }
}
```

The cell-nested loop structure goes away. BRIGHT and FLASH handling are
unchanged — they're per-attribute-byte, so in per-pixel mode they
automatically gain per-pixel granularity. Flashing items flash only on
their own pixels, which is strictly more correct than the original.

Performance cost: ~49k attribute reads per frame instead of ~768. At 50 Hz
that's negligible on any modern host. Don't micro-optimise.

### 5. Convert callers

Audit every attribute-write site in the codebase. Classify each one:

- **Direct `buf.Attrs[...] = x` writes** → replace with `SetCellAttr(col,
  row, x)`. These are usually menus, HUD, debug overlays — all cell-aligned
  writes that just need to work in both modes.
- **Background tiles, room fills, decorations** → keep using
  `FillAttrArea` / `SetAttrGrid`. No caller changes.
- **Sprite attribute writes** (player, enemies, items, weapons,
  animations) → route through `StampSpriteAttr`, passing the same sprite
  blob the pixel blit already receives.

In Atic Atac, the sprite audit found 11 call sites to a single
`paintEntityAttr` helper (player walk, death shrink, spawn grow, creatures,
explosions, sparkles, weapon, HUD inventory, HUD lives). Refactoring
`paintEntityAttr` to take the sprite blob and delegate to `StampSpriteAttr`
meant the call-site changes were mechanical: replace `(x, y, 2,
int(spr[0]), attr)` with `(x, y, spr, attr)`.

If your game lacks a central helper, add one — it's one function per sprite
blit style (2-byte entity, N-byte decoration, etc) and it's worth it for
the grep-ability alone.

### 6. Expose the toggle

Add a setting wherever your game's options menu lives. In Atic Atac this
was a fifth row in the settings sub-menu:

```go
type MenuState struct {
    // ...
    ColourClash bool // true = authentic, false = per-pixel
}

func NewMenuState() MenuState {
    return MenuState{ColourClash: true}
}

// In the settings toggle handler:
case 4:
    ms.ColourClash = !ms.ColourClash
    if ms.ColourClash {
        screen.Mode = screen.ColourModeAuthentic
    } else {
        screen.Mode = screen.ColourModePerPixel
    }
```

Name it "COLOUR CLASH" and default it to ON. That way a cold-started copy
of the game looks exactly like the original, and the no-clash mode is an
opt-in the player discovers.

## Sprite erasure and animation

The pattern relies on one invariant: **background tiles must restamp their
full attribute rect each redraw.** Because `FillAttrArea` in both modes
stamps every pixel in the rect, a background redraw naturally erases stale
sprite attributes from the previous frame — no explicit erase pass needed.

If your game has any XOR-only sprite erasure path that *doesn't* redraw the
background beneath (i.e., it just XORs the sprite pixels off), that path
leaves stale attributes in per-pixel mode. The fix is to redraw the
background; Atic Atac already does this on room entry and every frame
during animation, so no change was needed. Audit your game's erasure
strategy before shipping.

## Migration plan

Each step leaves the game shippable. Steps 1–5 are invisible to the
player; only step 6 introduces a user-visible change, gated by the toggle.

1. **Widen `Attrs`** and add `ColourMode` + `Mode`. Nothing else changes.
   Game runs identically.
2. **Add `SetCellAttr` and `StampSpriteAttr`.** Reimplement `FillAttrArea`
   / `SetAttrGrid` in terms of pixel-rect fills. Byte-identical output in
   authentic mode.
3. **Rewrite the renderer loop** for per-pixel attribute lookup. Still
   byte-identical because every pixel in a cell shares its attribute.
4. **Convert direct `Attrs` writes** across the codebase to `SetCellAttr`.
5. **Convert sprite attribute writes** to `StampSpriteAttr`. Still
   byte-identical in authentic mode because `StampSpriteAttr` in that mode
   reproduces the original cell rule.
6. **Expose the toggle** in the settings menu and wire it to `screen.Mode`.

Regression test after each step with a screenshot diff against a pre-change
baseline. If step 3 or 5 drifts, the sprite audit missed a site or the
authentic-mode rule inside `StampSpriteAttr` doesn't match your game's
original cell formula.

## Portable checklist

When porting this pattern to another Spectrum conversion, work through:

- [ ] Add `ColourMode` enum + global `Mode` to the `screen` package.
- [ ] Widen `Attrs` to `ScreenWidthPx * ScreenHeightPx` bytes.
- [ ] Delete any cell-indexed `AttrAddr` helper to fail loudly.
- [ ] Add `SetCellAttr(col, row, attr)` helper.
- [ ] Add `StampSpriteAttr(x, y, spr, attr)` with your game's sprite
      format and your game's authentic-mode cell rule.
- [ ] Reimplement `FillAttrArea` / `SetAttrGrid` as pixel-rect fills.
- [ ] Move the renderer's attribute lookup inside the pixel loop.
- [ ] Grep for direct `buf.Attrs[...] =` writes and convert to
      `SetCellAttr`.
- [ ] Grep for your sprite-attribute helper (in Atic Atac, this was
      `paintEntityAttr`) and convert to `StampSpriteAttr`.
- [ ] Audit every call site of that helper — pass the sprite blob through.
- [ ] Confirm your erasure strategy redraws backgrounds rather than
      XOR-erasing in isolation.
- [ ] Add a `COLOUR CLASH` toggle to the settings menu, default ON.
- [ ] Screenshot-diff against baseline in authentic mode; toggle and
      eyeball in per-pixel mode.

The sprite audit is the only per-game work of any size; everything else is
mechanical.

## Known caveats

- **Sprite Z-order on overlaps.** When two sprites overlap in per-pixel
  mode, whichever draws last owns the shared pixels' attributes. In cell
  mode this is invisible because the cell only holds one value anyway.
  Expect one or two spots where draw order matters; fix by reordering the
  draw calls, not by hacking the attribute writes.
- **Cell-aligned HUD/text.** Score, lives, inventory text, etc. are
  usually cell-aligned and write their attributes via `SetCellAttr`, so
  they remain visually identical in both modes.
- **Mid-frame mode switches are unsupported.** The toggle is designed to
  flip between frames. If the user toggles mid-frame the display may show
  one stale frame; a full redraw corrects it immediately.
- **Colour clash is part of the art direction.** The original artists
  designed around clash, and some rooms rely on it for visual framing.
  Per-pixel mode is strictly more accurate to the *sprite* colours but
  occasionally looks flatter or less intentional. That's inherent to the
  feature, not a bug — offer it as an option, don't force it.
