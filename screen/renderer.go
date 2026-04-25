package screen

import "image/color"

// ZX Spectrum colour palette (normal + bright).
var Palette = [16]color.RGBA{
	// Normal colours (BRIGHT off)
	{0, 0, 0, 255},       // 0: black
	{0, 0, 205, 255},     // 1: blue
	{205, 0, 0, 255},     // 2: red
	{205, 0, 205, 255},   // 3: magenta
	{0, 205, 0, 255},     // 4: green
	{0, 205, 205, 255},   // 5: cyan
	{205, 205, 0, 255},   // 6: yellow
	{205, 205, 205, 255}, // 7: white

	// Bright colours
	{0, 0, 0, 255},       // 8: black (bright)
	{0, 0, 255, 255},     // 9: blue (bright)
	{255, 0, 0, 255},     // 10: red (bright)
	{255, 0, 255, 255},   // 11: magenta (bright)
	{0, 255, 0, 255},     // 12: green (bright)
	{0, 255, 255, 255},   // 13: cyan (bright)
	{255, 255, 0, 255},   // 14: yellow (bright)
	{255, 255, 255, 255}, // 15: white (bright)
}

// FlashCounter tracks the global flash state for FLASH attribute.
var FlashCounter int

// RenderToRGBA converts the ZX Spectrum buffer into a flat RGBA byte slice.
// Output is 256*192*4 bytes (RGBA for each pixel).
// Handles the FLASH attribute (bit 7): swaps INK/PAPER every 16 frames.
//
// Attributes are read once per pixel. In authentic mode every pixel in an
// 8×8 cell carries the same attribute byte, so the result is bit-identical
// to the original cell-based Spectrum display. In per-pixel mode, sprites
// and backgrounds each own their own pixels' attributes and there is no
// colour clash.
func RenderToRGBA(buf *Buffer, out []byte) {
	FlashCounter++
	flashSwap := (FlashCounter / 16) % 2

	for y := 0; y < ScreenHeightPx; y++ {
		rowBase := yTable[y]
		attrRowBase := y * ScreenWidthPx
		for charCol := 0; charCol < ScreenCols; charCol++ {
			pixByte := buf.Pixels[rowBase+uint16(charCol)]
			for bit := 0; bit < 8; bit++ {
				x := charCol*8 + bit
				attr := buf.Attrs[attrRowBase+x]

				ink := attr & 0x07
				paper := (attr >> 3) & 0x07
				if attr&0x40 != 0 {
					ink += 8
					paper += 8
				}
				if attr&0x80 != 0 && flashSwap != 0 {
					ink, paper = paper, ink
				}

				off := (y*ScreenWidthPx + x) * 4
				var c color.RGBA
				if pixByte&(0x80>>uint(bit)) != 0 {
					c = Palette[ink]
				} else {
					c = Palette[paper]
				}
				out[off] = c.R
				out[off+1] = c.G
				out[off+2] = c.B
				out[off+3] = c.A
			}
		}
	}
}
