package render3d

import (
	"image/color"
	"math"

	"github.com/seamuswaldron/aticatac/screen"
)

// PlayAreaW is the width of the playable area (left portion of the screen).
// The right 64 pixels (columns 24-31) are the HUD panel.
const PlayAreaW = 192

// Raster is a software rasterizer with a Z-buffer.
// Renders at PlayAreaW x 192 (the play area only, not the HUD panel).
type Raster struct {
	Width, Height int
	ColorBuf      []byte    // palette index per pixel (0-15)
	ZBuf          []float32 // depth per pixel
}

// NewRaster creates a raster buffer at 192x192 (play area dimensions).
func NewRaster() *Raster {
	w, h := PlayAreaW, screen.ScreenHeightPx
	return &Raster{
		Width:    w,
		Height:   h,
		ColorBuf: make([]byte, w*h),
		ZBuf:     make([]float32, w*h),
	}
}

// Clear fills the colour buffer with black and the Z-buffer with max depth.
func (r *Raster) Clear() {
	for i := range r.ColorBuf {
		r.ColorBuf[i] = 0 // black
	}
	for i := range r.ZBuf {
		r.ZBuf[i] = math.MaxFloat32
	}
}

// setPixel writes a pixel if it passes the depth test.
func (r *Raster) setPixel(x, y int, z float32, color byte) {
	if x < 0 || x >= r.Width || y < 0 || y >= r.Height {
		return
	}
	idx := y*r.Width + x
	if z < r.ZBuf[idx] {
		r.ZBuf[idx] = z
		r.ColorBuf[idx] = color
	}
}

// ScreenVert holds a projected screen-space vertex with depth.
type ScreenVert struct {
	X, Y  float32
	Depth float32
}

// FillTriangle rasterizes a filled triangle using scanline conversion with Z-interpolation.
func (r *Raster) FillTriangle(v0, v1, v2 ScreenVert, color byte) {
	// Sort vertices by Y (top to bottom)
	if v0.Y > v1.Y {
		v0, v1 = v1, v0
	}
	if v0.Y > v2.Y {
		v0, v2 = v2, v0
	}
	if v1.Y > v2.Y {
		v1, v2 = v2, v1
	}

	// Degenerate triangle check
	totalH := v2.Y - v0.Y
	if totalH < 0.5 {
		return
	}

	// Scanline rasterization
	yStart := int(math.Ceil(float64(v0.Y)))
	yEnd := int(math.Floor(float64(v2.Y)))
	if yStart < 0 {
		yStart = 0
	}
	if yEnd >= r.Height {
		yEnd = r.Height - 1
	}

	for y := yStart; y <= yEnd; y++ {
		fy := float32(y) + 0.5

		// Compute x-intersections with edges
		// Long edge: v0 → v2 (always spans full height)
		tLong := (fy - v0.Y) / totalH
		xLong := v0.X + tLong*(v2.X-v0.X)
		zLong := v0.Depth + tLong*(v2.Depth-v0.Depth)

		var xShort, zShort float32
		if fy < v1.Y {
			// Upper half: v0 → v1
			h := v1.Y - v0.Y
			if h < 0.5 {
				continue
			}
			t := (fy - v0.Y) / h
			xShort = v0.X + t*(v1.X-v0.X)
			zShort = v0.Depth + t*(v1.Depth-v0.Depth)
		} else {
			// Lower half: v1 → v2
			h := v2.Y - v1.Y
			if h < 0.5 {
				continue
			}
			t := (fy - v1.Y) / h
			xShort = v1.X + t*(v2.X-v1.X)
			zShort = v1.Depth + t*(v2.Depth-v1.Depth)
		}

		// Ensure left-to-right
		xLeft, xRight := xLong, xShort
		zLeft, zRight := zLong, zShort
		if xLeft > xRight {
			xLeft, xRight = xRight, xLeft
			zLeft, zRight = zRight, zLeft
		}

		xStart := int(math.Ceil(float64(xLeft)))
		xEnd := int(math.Floor(float64(xRight)))
		if xStart < 0 {
			xStart = 0
		}
		if xEnd >= r.Width {
			xEnd = r.Width - 1
		}

		span := xRight - xLeft
		for x := xStart; x <= xEnd; x++ {
			var z float32
			if span < 0.5 {
				z = (zLeft + zRight) * 0.5
			} else {
				t := (float32(x) - xLeft) / span
				z = zLeft + t*(zRight-zLeft)
			}
			r.setPixel(x, y, z, color)
		}
	}
}

// FillQuad rasterizes a quad as two triangles.
func (r *Raster) FillQuad(v0, v1, v2, v3 ScreenVert, color byte) {
	r.FillTriangle(v0, v1, v2, color)
	r.FillTriangle(v0, v2, v3, color)
}

// DrawLine draws a line between two screen-space points with Z-testing.
func (r *Raster) DrawLine(x0, y0 int, z0 float32, x1, y1 int, z1 float32, color byte) {
	dx := x1 - x0
	dy := y1 - y0
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}

	steps := dx
	if dy > steps {
		steps = dy
	}
	if steps == 0 {
		r.setPixel(x0, y0, z0, color)
		return
	}

	for i := 0; i <= steps; i++ {
		t := float32(i) / float32(steps)
		x := x0 + int(t*float32(x1-x0))
		y := y0 + int(t*float32(y1-y0))
		z := z0 + t*(z1-z0)
		r.setPixel(x, y, z-0.001, color) // slight bias so edges draw on top of fills
	}
}

// ToRGBA converts the palette-indexed colour buffer into RGBA bytes.
// No vertical flip — the projection handles Y-up→Y-down via -ndcY.
func (r *Raster) ToRGBA(out []byte) {
	for i, ci := range r.ColorBuf {
		var c color.RGBA
		if int(ci) < len(screen.Palette) {
			c = screen.Palette[ci]
		}
		off := i * 4
		out[off] = c.R
		out[off+1] = c.G
		out[off+2] = c.B
		out[off+3] = c.A
	}
}
