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
	Doors     map[byte][]data.RoomDoor
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
	r.camera.Y = 0.5 // eye height (below wall midpoint for more visible wall)
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

	// Render room decorations (door arches, shields, torches, etc.)
	r.renderDecorations(s, w, h)

	// Render doors
	r.renderDoors(s, w, h)

	// Convert to RGBA (HUD is drawn by the 2D renderer, not here)
	r.raster.ToRGBA(r.rgbaOut)
	return r.rgbaOut
}

// renderDoors draws doors as coloured markers on walls.
func (r *Renderer) renderDoors(s RenderState, w, h int) {
	doors := s.Doors[s.Room]
	for _, d := range doors {
		pos := PixelToWorld(int(d.X), int(d.Y))

		// Door colour based on type
		var colorIdx byte
		switch data.DoorType(d.Type) {
		case data.DoorRed, data.DoorRedC:
			colorIdx = 10 // bright red
		case data.DoorGreen, data.DoorGreenC:
			colorIdx = 12 // bright green
		case data.DoorCyan, data.DoorCyanC:
			colorIdx = 13 // bright cyan
		case data.DoorYellow:
			colorIdx = 14 // bright yellow
		case data.DoorBlue:
			colorIdx = 9 // bright blue
		default:
			colorIdx = 15 // bright white
		}

		// Draw as a small billboard
		halfW := float32(0.25)
		halfH := float32(0.6)

		cs := r.camera.WorldToCamera(pos)
		if cs.Z <= r.camera.Near {
			continue
		}

		corners := [4]Vec3{
			{cs.X - halfW, -halfH, cs.Z},
			{cs.X + halfW, -halfH, cs.Z},
			{cs.X + halfW, halfH, cs.Z},
			{cs.X - halfW, halfH, cs.Z},
		}

		var sv [4]ScreenVert
		allVis := true
		for j := 0; j < 4; j++ {
			sx, sy, depth, vis := r.camera.Project(corners[j], w, h)
			if !vis {
				allVis = false
				break
			}
			sv[j] = ScreenVert{float32(sx), float32(sy), depth}
		}
		if !allVis {
			continue
		}

		// Draw door outline
		for j := 0; j < 4; j++ {
			k := (j + 1) % 4
			r.raster.DrawLine(
				int(sv[j].X), int(sv[j].Y), sv[j].Depth,
				int(sv[k].X), int(sv[k].Y), sv[k].Depth,
				colorIdx,
			)
		}
	}
}

// renderDecorations draws room decorations (door arches, shields, torches, etc.) as billboards.
func (r *Renderer) renderDecorations(s RenderState, w, h int) {
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
		attr := e[5]

		// Skip door types (0x01-0x0F) — rendered by renderDoors
		if typeID >= 0x01 && typeID <= 0x0F {
			continue
		}
		// Skip invalid types
		if typeID <= 0 || typeID > 0x26 {
			continue
		}

		// Look up decoration sprite data
		gfxIdx := typeID - 1
		if gfxIdx < 0 || gfxIdx >= 39 {
			continue
		}
		sprData, ok := data.GenDecoSprites[gfxIdx]
		if !ok || len(sprData) < 2 {
			continue
		}

		RenderDecoBillboard(r.raster, &r.camera, x, y, sprData, attr, w, h)
	}
}

