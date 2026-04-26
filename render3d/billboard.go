package render3d

import (
	"github.com/seamuswaldron/aticatac/data"
	"github.com/seamuswaldron/aticatac/entity"
)

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

	// Entity world position
	pos := PixelToWorld(e.X, e.Y)

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

	// Billboard centre Y — sprites are drawn from bottom-up, centre at mid-height
	centreY := worldH / 2

	// Project two corners of the billboard to get screen extent
	c0 := Vec3{cs.X - worldW/2, centreY + worldH/2, cs.Z}
	c1 := Vec3{cs.X + worldW/2, centreY - worldH/2, cs.Z}

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
	for py := 0; py < screenSprH; py++ {
		// Map screen Y back to sprite row
		sprRow := py * sprHeight / screenSprH
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
				r.setPixel(screenX, screenY, cs.Z, colorIdx)
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
// Each sprite pixel is individually projected through the camera from its
// world position on the wall, giving correct perspective automatically.
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
	pixels := sprData[2:]

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

	// Base position in world coordinates
	basePos := PixelToWorld(px, py)

	// For each sprite pixel, compute its world position on the wall and project
	for row := 0; row < height; row++ {
		for col := 0; col < widthPx; col++ {
			byteIdx := col / 8
			bitIdx := uint(7 - col%8)
			if pixels[row*widthBytes+byteIdx]&(1<<bitIdx) == 0 {
				continue
			}

			// Compute world position of this pixel on the wall.
			// Sprite row 0 = top, height-1 = bottom.
			// In world: Y goes from wallHeight (top) to 0 (floor).
			// The decoration's base Y position is at the sprite's bottom.
			worldY := float32(height-row) / coordScale

			// Horizontal offset: sprite column relative to base position
			halfW := float32(widthPx) / 2
			colOffset := (float32(col) - halfW) / coordScale

			var worldPt Vec3
			switch wall {
			case WallNorth:
				// On N wall: sprite spans in X, wall is at base Z
				worldPt = Vec3{basePos.X + colOffset, worldY, basePos.Z}
			case WallSouth:
				// On S wall: sprite spans in X (mirrored), wall at base Z
				worldPt = Vec3{basePos.X - colOffset, worldY, basePos.Z}
			case WallWest:
				// On W wall: sprite spans in Z, wall at base X
				worldPt = Vec3{basePos.X, worldY, basePos.Z + colOffset}
			case WallEast:
				// On E wall: sprite spans in Z (mirrored), wall at base X
				worldPt = Vec3{basePos.X, worldY, basePos.Z - colOffset}
			default:
				// Fallback: camera-facing at base position
				worldPt = Vec3{basePos.X + colOffset, worldY, basePos.Z}
			}

			cs := cam.WorldToCamera(worldPt)
			if cs.Z <= cam.Near {
				continue
			}
			sx, sy, depth, vis := cam.Project(cs, screenW, screenH)
			if !vis {
				continue
			}

			// Per-cell colour
			cellCol := col / 8
			cellRow := row / 8
			attrRow := heightCells - 1 - cellRow
			colorIdx := attrColorForCell(attrs, attrW, attrH, cellCol, attrRow, widthBytes, roomAttr)

			r.setPixel(sx, sy, depth, colorIdx)
		}
	}
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
		{cs.X - halfSize, 0.5, cs.Z},
		{cs.X + halfSize, 0.5, cs.Z},
		{cs.X + halfSize, 1.1, cs.Z},
		{cs.X - halfSize, 1.1, cs.Z},
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
