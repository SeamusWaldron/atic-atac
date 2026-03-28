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
// data contains height as first byte, then 2 bytes per pixel row.
// The sprite is drawn upward from (x, y) matching the original Z80 code.
func (b *Buffer) DrawSpriteXOR(x, y int, data []byte) {
	if len(data) < 1 {
		return
	}
	height := int(data[0])
	if len(data) < 1+height*2 {
		return
	}
	col := x >> 3
	for row := 0; row < height; row++ {
		py := y - row
		if py < 0 || py >= ScreenHeightPx {
			continue
		}
		addr := yTable[py] + uint16(col)
		b1 := data[1+row*2]
		b2 := data[1+row*2+1]
		if int(addr) < DisplaySize {
			b.Pixels[addr] ^= b1
		}
		if int(addr)+1 < DisplaySize {
			b.Pixels[addr+1] ^= b2
		}
	}
}

// DrawSpriteOR draws a 2-byte-wide sprite at pixel position (x, y) using OR mode.
func (b *Buffer) DrawSpriteOR(x, y int, data []byte) {
	if len(data) < 1 {
		return
	}
	height := int(data[0])
	if len(data) < 1+height*2 {
		return
	}
	col := x >> 3
	for row := 0; row < height; row++ {
		py := y - row
		if py < 0 || py >= ScreenHeightPx {
			continue
		}
		addr := yTable[py] + uint16(col)
		b1 := data[1+row*2]
		b2 := data[1+row*2+1]
		if int(addr) < DisplaySize {
			b.Pixels[addr] |= b1
		}
		if int(addr)+1 < DisplaySize {
			b.Pixels[addr+1] |= b2
		}
	}
}

// DrawSprite4OR draws a 4-byte-wide sprite at pixel position (x, y) using OR mode.
// data format: [width_bytes, height, ...pixel data...]
func (b *Buffer) DrawSprite4OR(x, y int, data []byte) {
	if len(data) < 2 {
		return
	}
	width := int(data[0])
	height := int(data[1])
	if len(data) < 2+height*width {
		return
	}
	col := x >> 3
	for row := 0; row < height; row++ {
		py := y - row
		if py < 0 || py >= ScreenHeightPx {
			continue
		}
		addr := yTable[py] + uint16(col)
		for c := 0; c < width; c++ {
			a := int(addr) + c
			if a < DisplaySize {
				b.Pixels[a] |= data[2+row*width+c]
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
