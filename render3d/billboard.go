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

	// Project top-left and bottom-right of the billboard to get screen extent
	topLeft := Vec3{cs.X - worldW/2, centreY + worldH/2, cs.Z}
	botRight := Vec3{cs.X + worldW/2, centreY - worldH/2, cs.Z}

	sx0, sy0, _, vis0 := cam.Project(topLeft, screenW, screenH)
	sx1, sy1, _, vis1 := cam.Project(botRight, screenW, screenH)
	if !vis0 || !vis1 {
		return
	}

	// Screen rectangle for the billboard
	screenSprW := sx1 - sx0
	screenSprH := sy1 - sy0
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
