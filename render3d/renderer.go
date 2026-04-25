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
	Score     uint32
	Lives     byte
	Energy    byte
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
		rgbaOut:   make([]byte, screen.ScreenWidthPx*screen.ScreenHeightPx*4),
	}
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

// Render produces a 256x192 RGBA frame from the given game state.
func (r *Renderer) Render(s RenderState) []byte {
	r.raster.Clear()

	// Update camera position from player
	pos := PixelToWorld(int(s.PlayerX), int(s.PlayerY))
	r.camera.TargetX = pos.X
	r.camera.TargetZ = pos.Z
	r.camera.Y = 0.8 // eye height
	r.camera.TargetYaw = DirToYaw(s.PlayerDir)
	r.camera.Update()

	w := r.raster.Width
	h := r.raster.Height

	// Render walls for current room
	walls := r.wallCache.Walls[s.Room]
	for i := range walls {
		q := &walls[i]

		// Back-face culling: skip quads facing away from camera
		camToQuad := q.Verts[0].Sub(Vec3{r.camera.X, r.camera.Y, r.camera.Z})
		normal := q.Normal()
		if normal.Dot(camToQuad) > 0 {
			continue
		}

		// Project all 4 vertices
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
			// Simple near-plane clipping: skip quads with any vertex behind camera.
			// This is sufficient for room-scale geometry.
			continue
		}

		// Fill the quad
		r.raster.FillQuad(sv[0], sv[1], sv[2], sv[3], q.Color)

		// Draw edges (wireframe overlay)
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

	// Render doors
	r.renderDoors(s, w, h)

	// Draw HUD
	r.renderHUD(s)

	// Convert to RGBA
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

// renderHUD draws score, lives, and energy as a 2D overlay.
func (r *Renderer) renderHUD(s RenderState) {
	w := r.raster.Width

	// Energy bar at bottom of screen
	barY := r.raster.Height - 4
	barMaxW := 64
	energyW := int(s.Energy) * barMaxW / 240
	barStartX := w/2 - barMaxW/2

	for y := barY; y < barY+3; y++ {
		for x := barStartX; x < barStartX+barMaxW; x++ {
			if x < 0 || x >= w || y < 0 || y >= r.raster.Height {
				continue
			}
			idx := y*w + x
			if x-barStartX < energyW {
				r.raster.ColorBuf[idx] = 12 // bright green
			} else {
				r.raster.ColorBuf[idx] = 2 // red
			}
			r.raster.ZBuf[idx] = 0 // always on top
		}
	}

	// Lives indicators (small blocks at bottom-left)
	for i := 0; i < int(s.Lives); i++ {
		for y := barY; y < barY+3; y++ {
			for x := 4 + i*6; x < 4+i*6+4; x++ {
				if x >= 0 && x < w && y >= 0 && y < r.raster.Height {
					idx := y*w + x
					r.raster.ColorBuf[idx] = 14 // bright yellow
					r.raster.ZBuf[idx] = 0
				}
			}
		}
	}
}
