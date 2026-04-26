package render3d

import (
	"github.com/seamuswaldron/aticatac/data"
	"github.com/seamuswaldron/aticatac/entity"
)

// projVert holds a projected vertex with screen coords, depth, and inverse depth.
type projVert struct {
	sx, sy int
	depth  float32
	invZ   float32 // 1/depth for perspective-correct interpolation
}

// SpriteLookup returns the raw sprite data for an entity's current graphic.
// Returns nil if no sprite data is available.
// Sprite format: first byte = height, then 2 bytes per row (16 pixels wide).
func SpriteLookup(e *entity.Entity) []byte {
	graphicID := int(e.Graphic)
	if graphicID == 0 {
		return nil
	}
	// Graphic IDs are 1-based: flatIdx = graphicID - 1
	flatIdx := graphicID - 1
	group := flatIdx / 4
	frame := flatIdx % 4
	if group < 0 || group >= len(data.GenSpriteTable) {
		return nil
	}
	addr := data.GenSpriteTable[group][frame]
	if addr == 0 {
		return nil
	}
	return data.GenMenuIcons[addr]
}

// RenderSpriteBillboard draws an entity's sprite as a camera-facing billboard.
// The sprite is drawn pixel-by-pixel into the raster with Z-testing.
func RenderSpriteBillboard(r *Raster, cam *Camera, e *entity.Entity, screenW, screenH int) {
	sprData := SpriteLookup(e)

	// Entity world position — offset to sprite centre
	// Engine position is bottom-left of sprite; centre it horizontally
	pos := PixelToWorld(e.X+8, e.Y)

	// Entity colour
	ink := e.Attr & 0x07
	bright := (e.Attr >> 6) & 0x01
	colorIdx := ink + bright*8
	if colorIdx == 0 {
		colorIdx = 7 // default white if black
	}

	// Transform to camera space
	cs := cam.WorldToCamera(pos)
	if cs.Z <= cam.Near {
		return
	}

	if sprData == nil {
		// No sprite data — draw a simple coloured block
		renderBlockBillboard(r, cam, cs, colorIdx, screenW, screenH)
		return
	}

	// Sprite dimensions
	sprHeight := int(sprData[0])
	sprWidthPx := 16 // always 16 pixels wide (2 bytes per row)

	// Billboard size in world units — scale to roughly match game proportions
	worldW := float32(sprWidthPx) / coordScale
	worldH := float32(sprHeight) / coordScale

	// Billboard centre Y — standard Y-up (positive = up from floor)
	centreY := worldH / 2

	// Project two corners of the billboard to get screen extent
	c0 := Vec3{cs.X - worldW/2, centreY - worldH/2, cs.Z}
	c1 := Vec3{cs.X + worldW/2, centreY + worldH/2, cs.Z}

	px0, py0, _, vis0 := cam.Project(c0, screenW, screenH)
	px1, py1, _, vis1 := cam.Project(c1, screenW, screenH)
	if !vis0 || !vis1 {
		return
	}

	// Ensure correct screen ordering
	if px0 > px1 {
		px0, px1 = px1, px0
	}
	if py0 > py1 {
		py0, py1 = py1, py0
	}
	sx0, sy0 := px0, py0

	screenSprW := px1 - px0
	screenSprH := py1 - py0
	if screenSprW <= 0 || screenSprH <= 0 {
		return
	}

	// Draw sprite pixel-by-pixel, sampling from the original bitmap
	// Sprite row 0 = head (top), drawn at the BOTTOM of the screen rect
	// because higher world Y (top of billboard) → smaller screen Y (top of screen)
	// and spy=0 starts at sy0 (top of screen), so we invert the row mapping.
	for py := 0; py < screenSprH; py++ {
		sprRow := (screenSprH - 1 - py) * sprHeight / screenSprH
		if sprRow >= sprHeight {
			sprRow = sprHeight - 1
		}

		// Get the 2 bytes for this sprite row
		rowOff := 1 + sprRow*2
		if rowOff+1 >= len(sprData) {
			continue
		}
		hi := sprData[rowOff]
		lo := sprData[rowOff+1]

		for px := 0; px < screenSprW; px++ {
			// Map screen X back to sprite column (0-15)
			sprCol := px * sprWidthPx / screenSprW
			if sprCol >= sprWidthPx {
				sprCol = sprWidthPx - 1
			}

			// Check if this pixel is set in the sprite bitmap
			var set bool
			if sprCol < 8 {
				set = hi&(0x80>>uint(sprCol)) != 0
			} else {
				set = lo&(0x80>>uint(sprCol-8)) != 0
			}

			if set {
				screenX := sx0 + px
				screenY := sy0 + py
				// Bias slightly forward so sprites aren't occluded by walls at same Z
				r.setPixel(screenX, screenY, cs.Z-0.005, colorIdx)
			}
		}
	}
}

// WallDir indicates which wall a decoration is mounted on.
type WallDir int

const (
	WallNone  WallDir = iota
	WallNorth         // facing +Z (into room from top)
	WallSouth         // facing -Z (into room from bottom)
	WallWest          // facing +X (into room from left)
	WallEast          // facing -X (into room from right)
)

// WallFromMode returns the wall direction based on the decoration's rotation
// mode (bits 7-5 of the entity flags byte) and position.
func WallFromMode(mode int, px, py int) WallDir {
	switch mode {
	case 0, 1: // normal / h-flip → N/S wall
		if py < roomCentreY {
			return WallNorth
		}
		return WallSouth
	case 4, 5: // 180° → N/S wall (flipped)
		if py > roomCentreY {
			return WallSouth
		}
		return WallNorth
	case 2, 3: // 90° rotation → E/W wall
		if px > roomCentreX {
			return WallEast
		}
		return WallWest
	case 6, 7: // 270° rotation → E/W wall
		if px < roomCentreX {
			return WallWest
		}
		return WallEast
	}
	return WallNone
}

// RenderWallDecoration draws a decoration projected onto its wall surface.
// Uses inverse mapping: projects the 4 corners of the sprite quad, then
// for each screen pixel in the bounding box, computes UV to sample the sprite.
// This eliminates shredding gaps that forward projection causes.
func RenderWallDecoration(r *Raster, cam *Camera, px, py int, wall WallDir, sprData []byte, attrData []byte, roomAttr byte, screenW, screenH int) {
	if len(sprData) < 2 {
		return
	}
	widthBytes := int(sprData[0])
	height := int(sprData[1])
	widthPx := widthBytes * 8
	if widthBytes == 0 || height == 0 || len(sprData) < 2+widthBytes*height {
		return
	}
	sprPixels := sprData[2:]

	// Build per-cell colour lookup
	var attrW, attrH int
	var attrs []byte
	if len(attrData) >= 2 {
		attrW = int(attrData[0])
		attrH = int(attrData[1])
		if len(attrData) >= 2+attrW*attrH {
			attrs = attrData[2:]
		}
	}
	heightCells := (height + 7) / 8

	// Entity (px, py) is the bottom-left of the displayed sprite.
	// Centre offset depends on wall direction:
	//   N/S walls: sprite spans X (along wall), so offset px by widthPx/2
	//   E/W walls: sprite spans Y (along wall = Z in 3D), so offset py by widthPx/2 upward
	var basePos Vec3
	switch wall {
	case WallEast, WallWest:
		// px is the wall surface, py is bottom of displayed sprite.
		// Displayed Y extent for rotated sprite is widthPx; centre is py - widthPx/2.
		basePos = PixelToWorld(px, py-widthPx/2)
	default:
		// N/S walls (or default): px is left edge, centre is px + widthPx/2
		basePos = PixelToWorld(px+widthPx/2, py)
	}
	halfW := float32(widthPx) / 2 / coordScale
	topY := float32(height) / coordScale // top of sprite (standard Y-up)

	// Build the 4 world-space corners of the sprite quad:
	// c0=bottom-left, c1=bottom-right, c2=top-right, c3=top-left
	var corners [4]Vec3
	switch wall {
	case WallNorth:
		corners = [4]Vec3{
			{basePos.X - halfW, 0, basePos.Z},
			{basePos.X + halfW, 0, basePos.Z},
			{basePos.X + halfW, topY, basePos.Z},
			{basePos.X - halfW, topY, basePos.Z},
		}
	case WallSouth:
		corners = [4]Vec3{
			{basePos.X + halfW, 0, basePos.Z},
			{basePos.X - halfW, 0, basePos.Z},
			{basePos.X - halfW, topY, basePos.Z},
			{basePos.X + halfW, topY, basePos.Z},
		}
	case WallWest:
		// Player faces -X. Camera left = +Z (south). u=0 should be on camera left.
		corners = [4]Vec3{
			{basePos.X, 0, basePos.Z + halfW},     // c0 (u=0) at +Z (south = camera left)
			{basePos.X, 0, basePos.Z - halfW},     // c1 (u=1) at -Z (north = camera right)
			{basePos.X, topY, basePos.Z - halfW},
			{basePos.X, topY, basePos.Z + halfW},
		}
	case WallEast:
		// Player faces +X. Camera left = -Z (north). u=0 should be on camera left.
		corners = [4]Vec3{
			{basePos.X, 0, basePos.Z - halfW},     // c0 (u=0) at -Z (north = camera left)
			{basePos.X, 0, basePos.Z + halfW},     // c1 (u=1) at +Z (south = camera right)
			{basePos.X, topY, basePos.Z + halfW},
			{basePos.X, topY, basePos.Z - halfW},
		}
	default:
		// Camera-facing fallback
		corners = [4]Vec3{
			{basePos.X - halfW, 0, basePos.Z},
			{basePos.X + halfW, 0, basePos.Z},
			{basePos.X + halfW, topY, basePos.Z},
			{basePos.X - halfW, topY, basePos.Z},
		}
	}

	// Project all 4 corners to camera space (need both screen coords and depth)
	var pv [4]projVert
	allVis := true
	for i := 0; i < 4; i++ {
		cs := cam.WorldToCamera(corners[i])
		sx, sy, depth, vis := cam.Project(cs, screenW, screenH)
		if !vis {
			allVis = false
			break
		}
		pv[i] = projVert{sx, sy, depth, 1.0 / depth}
	}
	if !allVis {
		return
	}

	// Compute screen bounding box
	minX, maxX := pv[0].sx, pv[0].sx
	minY, maxY := pv[0].sy, pv[0].sy
	for i := 1; i < 4; i++ {
		if pv[i].sx < minX { minX = pv[i].sx }
		if pv[i].sx > maxX { maxX = pv[i].sx }
		if pv[i].sy < minY { minY = pv[i].sy }
		if pv[i].sy > maxY { maxY = pv[i].sy }
	}
	if minX < 0 { minX = 0 }
	if minY < 0 { minY = 0 }
	if maxX >= screenW { maxX = screenW - 1 }
	if maxY >= screenH { maxY = screenH - 1 }

	// For each screen pixel, compute perspective-correct UV using bilinear
	// interpolation on the projected quad.
	// UV mapping: u=0..1 left-to-right, v=0..1 top-to-bottom
	// Corners: c0=BL(u=0,v=1), c1=BR(u=1,v=1), c2=TR(u=1,v=0), c3=TL(u=0,v=0)
	for sy := minY; sy <= maxY; sy++ {
		for sx := minX; sx <= maxX; sx++ {
			// Compute UV via perspective-correct interpolation
			u, v, depth, inside := quadUV(float32(sx)+0.5, float32(sy)+0.5, pv)
			if !inside {
				continue
			}

			// Map UV to sprite pixel.
			// ZX Spectrum sprites are stored bottom-up: row 0 = bottom of sprite.
			// v=0 is ceiling (top of screen), so invert to get correct row.
			sprCol := int(u * float32(widthPx))
			sprRow := height - 1 - int(v*float32(height))
			if sprCol < 0 { sprCol = 0 }
			if sprRow < 0 { sprRow = 0 }
			if sprCol >= widthPx { sprCol = widthPx - 1 }
			if sprRow >= height { sprRow = height - 1 }

			// Check sprite pixel
			byteIdx := sprCol / 8
			bitIdx := uint(7 - sprCol%8)
			if sprPixels[sprRow*widthBytes+byteIdx]&(1<<bitIdx) == 0 {
				continue
			}

			// Per-cell colour
			cellCol := sprCol / 8
			cellRow := sprRow / 8
			attrRow := heightCells - 1 - cellRow
			colorIdx := attrColorForCell(attrs, attrW, attrH, cellCol, attrRow, widthBytes, roomAttr)

			// Bias decoration depth slightly forward so it wins depth test
			// against the wall fill at the same surface
			r.setPixel(sx, sy, depth-0.005, colorIdx)
		}
	}
}

// quadUV computes perspective-correct UV coordinates for a point inside a projected quad.
// Uses two-triangle decomposition with barycentric coordinates.
// pv[0]=BL(0,1), pv[1]=BR(1,1), pv[2]=TR(1,0), pv[3]=TL(0,0)
func quadUV(px, py float32, pv [4]projVert) (u, v, depth float32, inside bool) {
	// Try triangle 0: TL(3), BL(0), BR(1) — u/v: (0,0),(0,1),(1,1)
	if u, v, depth, ok := triUV(px, py,
		float32(pv[3].sx), float32(pv[3].sy), 0, 0, pv[3].invZ,
		float32(pv[0].sx), float32(pv[0].sy), 0, 1, pv[0].invZ,
		float32(pv[1].sx), float32(pv[1].sy), 1, 1, pv[1].invZ,
	); ok {
		return u, v, depth, true
	}
	// Try triangle 1: TL(3), BR(1), TR(2) — u/v: (0,0),(1,1),(1,0)
	return triUV(px, py,
		float32(pv[3].sx), float32(pv[3].sy), 0, 0, pv[3].invZ,
		float32(pv[1].sx), float32(pv[1].sy), 1, 1, pv[1].invZ,
		float32(pv[2].sx), float32(pv[2].sy), 1, 0, pv[2].invZ,
	)
}

// triUV computes perspective-correct UV for point (px,py) inside triangle (ax,ay)-(bx,by)-(cx,cy).
func triUV(px, py, ax, ay, au, av, ainvz, bx, by, bu, bv, binvz, cx, cy, cu, cv, cinvz float32) (u, v, depth float32, inside bool) {
	// Barycentric coordinates
	denom := (by-cy)*(ax-cx) + (cx-bx)*(ay-cy)
	if denom == 0 {
		return 0, 0, 0, false
	}
	inv := 1.0 / denom
	w0 := ((by-cy)*(px-cx) + (cx-bx)*(py-cy)) * inv
	w1 := ((cy-ay)*(px-cx) + (ax-cx)*(py-cy)) * inv
	w2 := 1.0 - w0 - w1

	// Handle reversed winding (caused by -ndcY projection flipping screen Y).
	// When winding is reversed, all barycentric coords are negative.
	// Negate them to get correct interpolation.
	if denom < 0 {
		w0, w1, w2 = -w0, -w1, -w2
	}

	if w0 < -0.001 || w1 < -0.001 || w2 < -0.001 {
		return 0, 0, 0, false
	}

	// Perspective-correct interpolation
	invZ := w0*ainvz + w1*binvz + w2*cinvz
	if invZ <= 0 {
		return 0, 0, 0, false
	}
	z := 1.0 / invZ

	u = (w0*au*ainvz + w1*bu*binvz + w2*cu*cinvz) * z
	v = (w0*av*ainvz + w1*bv*binvz + w2*cv*cinvz) * z

	return u, v, z, true
}

// attrColorForCell returns the palette index for a given cell position.
func attrColorForCell(attrs []byte, attrW, attrH, cellCol, cellRow, spriteCellW int, roomAttr byte) byte {
	if attrs == nil || attrW == 0 || attrH == 0 {
		// No attr data — use room colour
		ink := roomAttr & 0x07
		bright := (roomAttr >> 6) & 0x01
		return ink + bright*8
	}

	if cellCol >= attrW || cellRow >= attrH || cellCol < 0 || cellRow < 0 {
		ink := roomAttr & 0x07
		bright := (roomAttr >> 6) & 0x01
		return ink + bright*8
	}

	a := attrs[cellRow*attrW+cellCol]
	if a == 0x00 {
		return 7 // transparent → default white
	}
	if a == 0xFF {
		a = roomAttr
	}
	ink := a & 0x07
	bright := (a >> 6) & 0x01
	return ink + bright*8
}

// renderBlockBillboard draws a simple coloured square when no sprite data exists.
func renderBlockBillboard(r *Raster, cam *Camera, cs Vec3, colorIdx byte, screenW, screenH int) {
	halfSize := float32(0.3)
	corners := [4]Vec3{
		{cs.X - halfSize, 0.2, cs.Z},
		{cs.X + halfSize, 0.2, cs.Z},
		{cs.X + halfSize, 0.8, cs.Z},
		{cs.X - halfSize, 0.8, cs.Z},
	}

	var sv [4]ScreenVert
	for j := 0; j < 4; j++ {
		sx, sy, depth, vis := cam.Project(corners[j], screenW, screenH)
		if !vis {
			return
		}
		sv[j] = ScreenVert{float32(sx), float32(sy), depth}
	}

	r.FillQuad(sv[0], sv[1], sv[2], sv[3], colorIdx)
}
