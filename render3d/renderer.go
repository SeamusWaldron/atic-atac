package render3d

import (
	"github.com/seamuswaldron/aticatac/data"
	"github.com/seamuswaldron/aticatac/entity"
	"github.com/seamuswaldron/aticatac/screen"
)

// RenderState holds the game state needed for 3D rendering.
type RenderState struct {
	Room      byte
	PlayerX   byte
	PlayerY   byte
	PlayerDir int
	Entities  *entity.Pool
	Frame     uint32
}

// Renderer is the 3D rendering engine.
type Renderer struct {
	raster    *Raster
	wallCache *WallCache
	camera    Camera
	rgbaOut   []byte
}

// NewRenderer creates a new 3D renderer.
func NewRenderer() *Renderer {
	return &Renderer{
		raster:    NewRaster(),
		wallCache: BuildWallCache(),
		camera:    NewCamera(),
		rgbaOut:   make([]byte, PlayAreaW*screen.ScreenHeightPx*4),
	}
}

// SetCameraYaw sets the camera yaw directly (for 3D input mode).
func (r *Renderer) SetCameraYaw(yaw float32) {
	r.camera.Yaw = yaw
	r.camera.TargetYaw = yaw
}

// CameraYaw returns the current camera yaw.
func (r *Renderer) CameraYaw() float32 {
	return r.camera.Yaw
}

// SnapCamera instantly sets the camera to the player position (for room transitions).
func (r *Renderer) SnapCamera(px, py byte, dir int) {
	pos := PixelToWorld(int(px), int(py))
	r.camera.X = pos.X
	r.camera.Z = pos.Z
	r.camera.TargetX = pos.X
	r.camera.TargetZ = pos.Z
	yaw := DirToYaw(dir)
	r.camera.Yaw = yaw
	r.camera.TargetYaw = yaw
}

// Render produces a 192x192 RGBA frame covering the play area only.
func (r *Renderer) Render(s RenderState) []byte {
	r.raster.Clear()

	// Update camera position from player (yaw is managed by game layer in 3D mode)
	pos := PixelToWorld(int(s.PlayerX), int(s.PlayerY))
	r.camera.TargetX = pos.X
	r.camera.TargetZ = pos.Z
	r.camera.Y = 0.5 // eye height
	r.camera.Update()

	w := r.raster.Width
	h := r.raster.Height

	// Render walls for current room.
	// No back-face culling — line segment winding is arbitrary (from the 2D
	// wireframe data), and the Z-buffer handles occlusion correctly.
	walls := r.wallCache.Walls[s.Room]
	for i := range walls {
		q := &walls[i]

		// Project all 4 vertices; skip if any behind near plane
		var sv [4]ScreenVert
		allVisible := true
		for j := 0; j < 4; j++ {
			cs := r.camera.WorldToCamera(q.Verts[j])
			sx, sy, depth, vis := r.camera.Project(cs, w, h)
			if !vis {
				allVisible = false
				break
			}
			sv[j] = ScreenVert{float32(sx), float32(sy), depth}
		}
		if !allVisible {
			continue
		}

		// Fill the quad with paper (dark) colour — walls are dark surfaces
		r.raster.FillQuad(sv[0], sv[1], sv[2], sv[3], q.Color)

		// Draw edges in bright ink — wireframe structure on dark walls
		for j := 0; j < 4; j++ {
			k := (j + 1) % 4
			r.raster.DrawLine(
				int(sv[j].X), int(sv[j].Y), sv[j].Depth,
				int(sv[k].X), int(sv[k].Y), sv[k].Depth,
				q.EdgeColor,
			)
		}
	}

	// Render entities as billboards
	if s.Entities != nil {
		s.Entities.ForEachInRoom(s.Room, func(e *entity.Entity) {
			if e.Type == entity.TypeNone {
				return
			}
			RenderSpriteBillboard(r.raster, &r.camera, e, w, h)
		})
	}

	// Render ALL room decorations (doors, arches, shields, torches, etc.)
	r.renderDecorations(s, w, h)

	// Convert to RGBA (HUD is drawn by the 2D renderer, not here)
	r.raster.ToRGBA(r.rgbaOut)
	return r.rgbaOut
}

// renderDecorations draws ALL room decorations as billboards.
// This includes door arches, shields, torches, bookcases, barrels, etc.
// Colour is extracted from GenDecoAttrs (the attribute painting data),
// NOT from the entity flags byte which contains rotation/blend mode.
func (r *Renderer) renderDecorations(s RenderState, w, h int) {
	roomAttr := data.RoomAttrs[s.Room].Colour

	entities := data.GenRoomEntityData[int(s.Room)]
	for _, pair := range entities {
		// Find the side matching this room
		var e [8]byte
		if pair[1] == s.Room {
			copy(e[:], pair[0:8])
		} else if pair[9] == s.Room {
			copy(e[:], pair[8:16])
		} else {
			continue
		}

		typeID := int(e[0])
		x := int(e[3])
		y := int(e[4])
		mode := (int(e[5]) >> 5) & 0x07

		if typeID <= 0 || typeID > 0x26 {
			continue
		}

		// Look up decoration sprite data
		gfxIdx := typeID - 1
		if gfxIdx < 0 || gfxIdx >= 39 {
			continue
		}
		// Skip chicken sprites (HUD energy bar, not room decor)
		if gfxIdx == 18 || gfxIdx == 19 {
			continue
		}

		sprData, ok := data.GenDecoSprites[gfxIdx]
		if !ok || len(sprData) < 2 {
			continue
		}

		// Get per-cell attribute data for multi-colour rendering
		attrData := data.GenDecoAttrs[gfxIdx] // nil if not present

		// Determine which wall this decoration is on from its rotation mode
		wall := WallFromMode(mode, x, y)
		RenderWallDecoration(r.raster, &r.camera, x, y, wall, sprData, attrData, roomAttr, w, h)
	}
}

