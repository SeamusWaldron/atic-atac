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

// RenderToRGBA converts the ZX Spectrum buffer into a flat RGBA byte slice
// suitable for Ebitengine's NewImageFromImage or WritePixels.
// Output is 256*192*4 bytes (RGBA for each pixel).
func RenderToRGBA(buf *Buffer, out []byte) {
	for charRow := 0; charRow < ScreenRows; charRow++ {
		for charCol := 0; charCol < ScreenCols; charCol++ {
			attr := buf.Attrs[charRow*ScreenCols+charCol]
			ink := attr & 0x07
			paper := (attr >> 3) & 0x07
			bright := (attr >> 6) & 0x01
			if bright != 0 {
				ink += 8
				paper += 8
			}
			inkC := Palette[ink]
			paperC := Palette[paper]

			for pixRow := 0; pixRow < 8; pixRow++ {
				y := charRow*8 + pixRow
				addr := yTable[y] + uint16(charCol)
				pixByte := buf.Pixels[addr]

				for bit := 0; bit < 8; bit++ {
					x := charCol*8 + bit
					off := (y*ScreenWidthPx + x) * 4
					if pixByte&(0x80>>uint(bit)) != 0 {
						out[off] = inkC.R
						out[off+1] = inkC.G
						out[off+2] = inkC.B
						out[off+3] = inkC.A
					} else {
						out[off] = paperC.R
						out[off+1] = paperC.G
						out[off+2] = paperC.B
						out[off+3] = paperC.A
					}
				}
			}
		}
	}
}
