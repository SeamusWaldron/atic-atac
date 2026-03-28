package screen

// ZX Spectrum display dimensions.
const (
	ScreenWidthPx  = 256
	ScreenHeightPx = 192
	ScreenCols     = 32 // character columns (256/8)
	ScreenRows     = 24 // character rows (192/8)

	DisplaySize = 6144 // pixel data: 256*192/8
	AttrSize    = 768  // attributes: 32*24
)

// Buffer represents the ZX Spectrum display memory.
// Pixel data uses the original interleaved layout.
// Attributes use the standard 32×24 grid.
type Buffer struct {
	Pixels [DisplaySize]byte // $4000-$57FF equivalent
	Attrs  [AttrSize]byte    // $5800-$5AFF equivalent
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

// AttrAddr returns the byte offset in Attrs for pixel coordinate (x, y).
func AttrAddr(x, y int) uint16 {
	col := x >> 3
	row := y >> 3
	return uint16(row*ScreenCols + col)
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
func (b *Buffer) FillAttrArea(col, row, w, h int, attr byte) {
	for r := row; r < row+h && r < ScreenRows; r++ {
		for c := col; c < col+w && c < ScreenCols; c++ {
			b.Attrs[r*ScreenCols+c] = attr
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

		// Clip to play area (columns 0-23, pixel X 0-191)
		maxCol := 24 - col // max bytes we can write before hitting HUD
		if maxCol <= 0 {
			continue
		}
		drawW := widthBytes
		if drawW > maxCol {
			drawW = maxCol
		}

		if shift == 0 {
			for c := 0; c < drawW; c++ {
				a := base + c
				if a >= 0 && a < DisplaySize {
					b.Pixels[a] |= rowData[c]
				}
			}
		} else {
			a := base
			if a >= 0 && a < DisplaySize && col >= 0 {
				b.Pixels[a] |= rowData[0] >> shift
			}
			for c := 1; c < drawW; c++ {
				a = base + c
				if a >= 0 && a < DisplaySize {
					b.Pixels[a] |= (rowData[c-1] << (8 - shift)) | (rowData[c] >> shift)
				}
			}
			if drawW < widthBytes || col+drawW < 24 {
				a = base + drawW
				if a >= 0 && a < DisplaySize && col+drawW < 24 {
					b.Pixels[a] |= rowData[drawW-1] << (8 - shift)
				}
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
func (b *Buffer) SetAttrGrid(col, row int, data []byte, w, h int) {
	for r := 0; r < h; r++ {
		for c := 0; c < w; c++ {
			ar := row + r
			ac := col + c
			if ar >= 0 && ar < ScreenRows && ac >= 0 && ac < ScreenCols {
				b.Attrs[ar*ScreenCols+ac] = data[r*w+c]
			}
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
