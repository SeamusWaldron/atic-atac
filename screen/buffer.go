package screen

// ZX Spectrum display dimensions.
const (
	ScreenWidthPx  = 256
	ScreenHeightPx = 192
	ScreenCols     = 32 // character columns (256/8)
	ScreenRows     = 24 // character rows (192/8)

	DisplaySize = 6144                          // pixel data: 256*192/8
	AttrSize    = ScreenWidthPx * ScreenHeightPx // one attribute byte per pixel
)

// ColourMode controls whether attributes are interpreted per-cell (authentic
// ZX Spectrum colour clash) or per-pixel (clash-free).
type ColourMode int

const (
	// ColourModeAuthentic replicates ZX Spectrum behaviour: one attribute
	// per 8×8 cell. Writes via FillAttrArea / SetCellAttr / StampSpriteAttr
	// fill every pixel of the affected cell with the same attribute, so the
	// per-pixel attribute array holds a cell-uniform image.
	ColourModeAuthentic ColourMode = iota

	// ColourModePerPixel gives each pixel its own attribute. Background
	// fills still stamp whole cells; sprites only stamp attributes at the
	// pixels their bitmap actually covers, so they no longer clobber the
	// surrounding background colours.
	ColourModePerPixel
)

// Mode is the global colour-attribute mode. Switching between frames is
// supported: the next full redraw converges the display to the new mode.
var Mode = ColourModeAuthentic

// Buffer represents the ZX Spectrum display memory.
// Pixel data uses the original interleaved layout.
// Attrs holds one attribute byte per pixel (row-major, y*256+x). In authentic
// mode every pixel in a cell carries the same attribute; in per-pixel mode
// each pixel is independent.
type Buffer struct {
	Pixels [DisplaySize]byte
	Attrs  [AttrSize]byte
}

// AttrPixelAddr returns the attribute index for pixel coordinate (x, y).
func AttrPixelAddr(x, y int) int {
	return y*ScreenWidthPx + x
}

// yTable maps pixel Y (0-191) to the byte offset within Pixels.
// Each entry gives the offset of the leftmost byte of that pixel row.
var yTable [ScreenHeightPx]uint16

func init() {
	for y := 0; y < ScreenHeightPx; y++ {
		// ZX Spectrum display address for pixel row y, column 0:
		// High byte: 010TT PPP  (T = third 0-2, P = pixel row within char 0-7)
		// Low byte:  CCCCC 000  (C = character row within third 0-7, but low 3 bits = column = 0)
		//
		// third  = y / 64          (bits 7-6 of y)
		// charRow = (y / 8) % 8    (bits 5-3 of y)
		// pixRow  = y % 8          (bits 2-0 of y)
		//
		// offset = third*0x800 + pixRow*0x100 + charRow*0x20
		third := (y >> 6) & 0x03
		charRow := (y >> 3) & 0x07
		pixRow := y & 0x07
		yTable[y] = uint16(third*0x800 + pixRow*0x100 + charRow*0x20)
	}
}

// PixelAddr returns the byte offset in Pixels for pixel coordinate (x, y).
func PixelAddr(x, y int) uint16 {
	return yTable[y] + uint16(x>>3)
}

// SetPixel sets a single pixel at (x, y) using OR mode.
func (b *Buffer) SetPixel(x, y int) {
	if x < 0 || x >= ScreenWidthPx || y < 0 || y >= ScreenHeightPx {
		return
	}
	addr := PixelAddr(x, y)
	bit := byte(0x80) >> uint(x&7)
	b.Pixels[addr] |= bit
}

// XORPixel toggles a single pixel at (x, y) using XOR mode.
func (b *Buffer) XORPixel(x, y int) {
	if x < 0 || x >= ScreenWidthPx || y < 0 || y >= ScreenHeightPx {
		return
	}
	addr := PixelAddr(x, y)
	bit := byte(0x80) >> uint(x&7)
	b.Pixels[addr] ^= bit
}

// ClearPixel clears a single pixel at (x, y).
func (b *Buffer) ClearPixel(x, y int) {
	if x < 0 || x >= ScreenWidthPx || y < 0 || y >= ScreenHeightPx {
		return
	}
	addr := PixelAddr(x, y)
	bit := byte(0x80) >> uint(x&7)
	b.Pixels[addr] &^= bit
}

// ClearPixels zeroes all pixel data.
func (b *Buffer) ClearPixels() {
	for i := range b.Pixels {
		b.Pixels[i] = 0
	}
}

// ClearAttrs zeroes all attribute data.
func (b *Buffer) ClearAttrs() {
	for i := range b.Attrs {
		b.Attrs[i] = 0
	}
}

// Clear zeroes both pixels and attributes.
func (b *Buffer) Clear() {
	b.ClearPixels()
	b.ClearAttrs()
}

// FillAttrArea fills a rectangular attribute area with the given attribute byte.
// (col, row) is the top-left character cell, (w, h) is size in character cells.
// Mode-independent: both authentic and per-pixel mode stamp every pixel in
// the affected cells, since this call is used for background regions.
func (b *Buffer) FillAttrArea(col, row, w, h int, attr byte) {
	x0 := col * 8
	y0 := row * 8
	x1 := x0 + w*8
	y1 := y0 + h*8
	b.fillAttrPixels(x0, y0, x1, y1, attr)
}

// fillAttrPixels stamps `attr` into every pixel in [x0,x1) × [y0,y1),
// clipping to the screen.
func (b *Buffer) fillAttrPixels(x0, y0, x1, y1 int, attr byte) {
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > ScreenWidthPx {
		x1 = ScreenWidthPx
	}
	if y1 > ScreenHeightPx {
		y1 = ScreenHeightPx
	}
	for y := y0; y < y1; y++ {
		base := y * ScreenWidthPx
		for x := x0; x < x1; x++ {
			b.Attrs[base+x] = attr
		}
	}
}

// SetCellAttr sets the attribute for one 8×8 character cell. Works in both
// modes because the underlying storage is per-pixel and a cell write simply
// stamps all 64 pixels.
func (b *Buffer) SetCellAttr(col, row int, attr byte) {
	if col < 0 || col >= ScreenCols || row < 0 || row >= ScreenRows {
		return
	}
	x0 := col * 8
	y0 := row * 8
	for y := y0; y < y0+8; y++ {
		base := y * ScreenWidthPx
		for x := x0; x < x0+8; x++ {
			b.Attrs[base+x] = attr
		}
	}
}

// StampSpriteAttr writes the given attribute into the display's attribute
// buffer at the pixel positions touched by a 2-byte-wide entity sprite. The
// sprite data format matches DrawSpriteXOR/DrawSpriteOR: first byte is the
// height in rows, followed by 2 bytes per row (top row first) and drawn
// upward from (x, y).
//
// In authentic mode the function rounds to cells and stamps whole 8×8
// blocks, reproducing the original cell-based attribute writes (matching
// the Z80 set_entity_attrs routine). In per-pixel mode it stamps only at
// the pixel positions where the sprite bitmap has a set bit, so the sprite
// does not clobber background attributes around its mask.
func (b *Buffer) StampSpriteAttr(x, y int, data []byte, attr byte) {
	if attr == 0 || len(data) < 1 {
		return
	}
	height := int(data[0])
	if len(data) < 1+height*2 {
		return
	}

	if Mode == ColourModeAuthentic {
		// Width: 2 cells if byte-aligned, 3 if the sprite straddles a
		// cell boundary, matching the Z80 $5E10 width-bytes rule.
		actualWidth := 2
		if x&7 != 0 {
			actualWidth = 3
		}
		// Height: Z80 formula from set_entity_attrs at $A02E.
		attrH := ((height >> 2) + 1) >> 1
		attrH = (attrH & 0x1F) + 1

		startCol := x >> 3
		startRow := y >> 3
		for r := 0; r < attrH; r++ {
			for c := 0; c < actualWidth; c++ {
				b.SetCellAttr(startCol+c, startRow-r, attr)
			}
		}
		return
	}

	// Per-pixel mode: stamp attr only where the sprite bitmap is set.
	// Sprite draws upward from y (pixel row 0 sits at y, row 1 at y-1, …).
	for row := 0; row < height; row++ {
		py := y - row
		if py < 0 || py >= ScreenHeightPx {
			continue
		}
		b1 := data[1+row*2]
		b2 := data[1+row*2+1]
		base := py * ScreenWidthPx
		for bit := 0; bit < 8; bit++ {
			if b1&(0x80>>uint(bit)) != 0 {
				px := x + bit
				if px >= 0 && px < ScreenWidthPx {
					b.Attrs[base+px] = attr
				}
			}
			if b2&(0x80>>uint(bit)) != 0 {
				px := x + 8 + bit
				if px >= 0 && px < ScreenWidthPx {
					b.Attrs[base+px] = attr
				}
			}
		}
	}
}

// DrawSpriteXOR draws a 2-byte-wide sprite at pixel position (x, y) using XOR mode.
// (x, y) is the top-left of the sprite. Sprite data: first byte = height,
// then 2 bytes per pixel row, top-to-bottom. Handles sub-byte pixel shifting
// when x is not byte-aligned (spills into a 3rd byte).
func (b *Buffer) DrawSpriteXOR(x, y int, data []byte) {
	if len(data) < 1 {
		return
	}
	height := int(data[0])
	if len(data) < 1+height*2 {
		return
	}
	col := x >> 3
	shift := uint(x & 7) // sub-byte pixel offset

	for row := 0; row < height; row++ {
		py := y - row // draw UPWARD from y (entity Y = bottom of sprite)
		if py < 0 || py >= ScreenHeightPx {
			continue
		}
		addr := int(yTable[py]) + col
		b1 := data[1+row*2]
		b2 := data[1+row*2+1]

		if shift == 0 {
			// Byte-aligned: 2 bytes
			if addr >= 0 && addr < DisplaySize {
				b.Pixels[addr] ^= b1
			}
			if addr+1 >= 0 && addr+1 < DisplaySize {
				b.Pixels[addr+1] ^= b2
			}
		} else {
			// Shifted: spills across 3 bytes
			s0 := b1 >> shift
			s1 := (b1 << (8 - shift)) | (b2 >> shift)
			s2 := b2 << (8 - shift)
			if addr >= 0 && addr < DisplaySize {
				b.Pixels[addr] ^= s0
			}
			if addr+1 >= 0 && addr+1 < DisplaySize {
				b.Pixels[addr+1] ^= s1
			}
			if addr+2 >= 0 && addr+2 < DisplaySize {
				b.Pixels[addr+2] ^= s2
			}
		}
	}
}

// DrawSpriteOR draws a 2-byte-wide sprite at pixel position (x, y) using OR mode.
// Same layout as DrawSpriteXOR but uses OR instead of XOR.
func (b *Buffer) DrawSpriteOR(x, y int, data []byte) {
	if len(data) < 1 {
		return
	}
	height := int(data[0])
	if len(data) < 1+height*2 {
		return
	}
	col := x >> 3
	shift := uint(x & 7)

	for row := 0; row < height; row++ {
		py := y - row
		if py < 0 || py >= ScreenHeightPx {
			continue
		}
		addr := int(yTable[py]) + col
		b1 := data[1+row*2]
		b2 := data[1+row*2+1]

		if shift == 0 {
			if addr >= 0 && addr < DisplaySize {
				b.Pixels[addr] |= b1
			}
			if addr+1 >= 0 && addr+1 < DisplaySize {
				b.Pixels[addr+1] |= b2
			}
		} else {
			s0 := b1 >> shift
			s1 := (b1 << (8 - shift)) | (b2 >> shift)
			s2 := b2 << (8 - shift)
			if addr >= 0 && addr < DisplaySize {
				b.Pixels[addr] |= s0
			}
			if addr+1 >= 0 && addr+1 < DisplaySize {
				b.Pixels[addr+1] |= s1
			}
			if addr+2 >= 0 && addr+2 < DisplaySize {
				b.Pixels[addr+2] |= s2
			}
		}
	}
}

// DrawSpriteWideOR draws an N-byte-wide sprite at pixel position (x, y) using OR mode.
// Data format: widthBytes × height bytes of pixel data (no header — caller provides width and height).
// Draws UPWARD from y (entity Y = bottom of sprite), matching the 2-byte sprites.
func (b *Buffer) DrawSpriteWideOR(x, y, widthBytes, height int, data []byte) {
	if len(data) < widthBytes*height {
		return
	}
	col := x >> 3
	shift := uint(x & 7)

	for row := 0; row < height; row++ {
		py := y - row
		if py < 0 || py >= ScreenHeightPx {
			continue
		}
		base := int(yTable[py]) + col
		rowData := data[row*widthBytes : row*widthBytes+widthBytes]

		if shift == 0 {
			for c := 0; c < widthBytes; c++ {
				a := base + c
				if a >= 0 && a < DisplaySize {
					b.Pixels[a] |= rowData[c]
				}
			}
		} else {
			a := base
			if a >= 0 && a < DisplaySize {
				b.Pixels[a] |= rowData[0] >> shift
			}
			for c := 1; c < widthBytes; c++ {
				a = base + c
				if a >= 0 && a < DisplaySize {
					b.Pixels[a] |= (rowData[c-1] << (8 - shift)) | (rowData[c] >> shift)
				}
			}
			a = base + widthBytes
			if a >= 0 && a < DisplaySize {
				b.Pixels[a] |= rowData[widthBytes-1] << (8 - shift)
			}
		}
	}
}

// DrawSpriteWideOverwrite draws an N-byte-wide sprite using OVERWRITE mode.
// Unlike OR mode, this REPLACES display bytes, clearing any existing pixels.
// Used for door sprites which need to erase frame lines beneath them.
func (b *Buffer) DrawSpriteWideOverwrite(x, y, widthBytes, height int, data []byte) {
	if len(data) < widthBytes*height {
		return
	}
	col := x >> 3
	shift := uint(x & 7)

	for row := 0; row < height; row++ {
		py := y - row
		if py < 0 || py >= ScreenHeightPx {
			continue
		}
		base := int(yTable[py]) + col
		rowData := data[row*widthBytes : row*widthBytes+widthBytes]

		if shift == 0 {
			for c := 0; c < widthBytes; c++ {
				a := base + c
				if a >= 0 && a < DisplaySize {
					b.Pixels[a] = rowData[c] // OVERWRITE, not OR
				}
			}
		} else {
			a := base
			if a >= 0 && a < DisplaySize {
				// Preserve bits outside the sprite, overwrite bits inside
				mask0 := byte(0xFF) << (8 - shift)
				b.Pixels[a] = (b.Pixels[a] & mask0) | (rowData[0] >> shift)
			}
			for c := 1; c < widthBytes; c++ {
				a = base + c
				if a >= 0 && a < DisplaySize {
					b.Pixels[a] = (rowData[c-1] << (8 - shift)) | (rowData[c] >> shift)
				}
			}
			a = base + widthBytes
			if a >= 0 && a < DisplaySize {
				maskEnd := byte(0xFF) >> shift
				b.Pixels[a] = (b.Pixels[a] & maskEnd) | (rowData[widthBytes-1] << (8 - shift))
			}
		}
	}
}

// DrawSpriteWideXOR draws an N-byte-wide sprite using XOR mode.
func (b *Buffer) DrawSpriteWideXOR(x, y, widthBytes, height int, data []byte) {
	if len(data) < widthBytes*height {
		return
	}
	col := x >> 3
	shift := uint(x & 7)

	for row := 0; row < height; row++ {
		py := y - row
		if py < 0 || py >= ScreenHeightPx {
			continue
		}
		base := int(yTable[py]) + col
		rowData := data[row*widthBytes : row*widthBytes+widthBytes]

		if shift == 0 {
			for c := 0; c < widthBytes; c++ {
				a := base + c
				if a >= 0 && a < DisplaySize {
					b.Pixels[a] ^= rowData[c]
				}
			}
		} else {
			a := base
			if a >= 0 && a < DisplaySize {
				b.Pixels[a] ^= rowData[0] >> shift
			}
			for c := 1; c < widthBytes; c++ {
				a = base + c
				if a >= 0 && a < DisplaySize {
					b.Pixels[a] ^= (rowData[c-1] << (8 - shift)) | (rowData[c] >> shift)
				}
			}
			a = base + widthBytes
			if a >= 0 && a < DisplaySize {
				b.Pixels[a] ^= rowData[widthBytes-1] << (8 - shift)
			}
		}
	}
}

// SetAttrGrid writes a rectangular grid of attribute bytes at (col, row) in character cells.
// data is a flat array of w*h attribute bytes, row-major order.
// Each grid cell stamps all 64 of its pixels with the same attribute (both modes).
func (b *Buffer) SetAttrGrid(col, row int, data []byte, w, h int) {
	for r := 0; r < h; r++ {
		for c := 0; c < w; c++ {
			b.SetCellAttr(col+c, row+r, data[r*w+c])
		}
	}
}

// DrawLine draws a line from (x0,y0) to (x1,y1) using Bresenham's algorithm.
// Uses OR mode, matching the original plot_l_h which uses OR (HL).
func (b *Buffer) DrawLine(x0, y0, x1, y1 int) {
	dx := x1 - x0
	dy := y1 - y0
	sx := 1
	sy := 1
	if dx < 0 {
		dx = -dx
		sx = -1
	}
	if dy < 0 {
		dy = -dy
		sy = -1
	}

	var err int
	if dx >= dy {
		err = dx / 2
		for i := 0; i <= dx; i++ {
			b.SetPixel(x0, y0)
			x0 += sx
			err -= dy
			if err < 0 {
				y0 += sy
				err += dx
			}
		}
	} else {
		err = dy / 2
		for i := 0; i <= dy; i++ {
			b.SetPixel(x0, y0)
			y0 += sy
			err -= dx
			if err < 0 {
				x0 += sx
				err += dy
			}
		}
	}
}
