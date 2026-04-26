package render3d

import (
	"fmt"

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
	raster     *Raster
	wallCache  *WallCache
	camera     Camera
	rgbaOut    []byte
	debugFrame int
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
	r.camera.Y = 0.75 // eye height (midpoint of 1.5-unit walls)
	r.camera.Update()

	if r.debugFrame%50 == 0 {
		fmt.Printf("3D: player=(%d,%d) cam=(%.2f,%.2f,%.2f) yaw=%.2f targetYaw=%.2f room=%d\n",
			s.PlayerX, s.PlayerY, r.camera.X, r.camera.Y, r.camera.Z, r.camera.Yaw, r.camera.TargetYaw, s.Room)
	}
	r.debugFrame++

	w := r.raster.Width
	h := r.raster.Height

	// DEBUG: Markers for walls, corners, floor and ceiling
	// Wall centres: Green=SOUTH Red=NORTH Cyan=WEST Yellow=EAST
	// Corners: White dots where walls meet
	// Floor/Ceiling: Magenta=floor, Bright white=ceiling (3 units ahead)
	{
		ra := data.RoomAttrs[s.Room]
		style := data.RoomStyles[ra.Style]
		rw := float32(style.Width)
		rh := float32(style.Height)

		type marker struct { name string; x,y,z float32; color byte }
		markers := []marker{
			// Wall centres at mid-height
			{"SOUTH", float32(roomCentreX), 0.75, float32(roomCentreY) + rh, 4},       // green
			{"NORTH", float32(roomCentreX), 0.75, float32(roomCentreY) - rh, 2},       // red
			{"WEST", float32(roomCentreX) - rw, 0.75, float32(roomCentreY), 5},        // cyan
			{"EAST", float32(roomCentreX) + rw, 0.75, float32(roomCentreY), 6},        // yellow
			// Corners at mid-height (where walls meet)
			{"NW", float32(roomCentreX) - rw, 0.75, float32(roomCentreY) - rh, 15},    // bright white
			{"NE", float32(roomCentreX) + rw, 0.75, float32(roomCentreY) - rh, 15},
			{"SW", float32(roomCentreX) - rw, 0.75, float32(roomCentreY) + rh, 15},
			{"SE", float32(roomCentreX) + rw, 0.75, float32(roomCentreY) + rh, 15},
		}

		for _, m := range markers {
			wpos := PixelToWorld(int(m.x), int(m.z))
			wpos.Y = m.y
			cs := r.camera.WorldToCamera(wpos)
			sx, sy, _, vis := r.camera.Project(cs, w, h)
			if vis {
				sz := 3
				if m.color == 15 { sz = 2 } // smaller for corners
				for dy := -sz; dy <= sz; dy++ {
					for dx := -sz; dx <= sz; dx++ {
						r.raster.setPixel(sx+dx, sy+dy, 0.01, m.color)
					}
				}
			}
			if r.debugFrame%250 == 1 && vis {
				fmt.Printf("  %s: pixel=(%.0f,%.0f) screen=(%d,%d)\n", m.name, m.x, m.z, sx, sy)
			}
		}

		// Floor and ceiling markers 3 units directly ahead
		fwdX := r.camera.X + 3*r.camera.sinYaw
		fwdZ := r.camera.Z + 3*r.camera.cosYaw
		// Floor (Y=0)
		csF := r.camera.WorldToCamera(Vec3{fwdX, 0, fwdZ})
		sxF, syF, _, visF := r.camera.Project(csF, w, h)
		if visF {
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					r.raster.setPixel(sxF+dx, syF+dy, 0.01, 3) // magenta = floor
				}
			}
		}
		// Ceiling (Y=wallHeight)
		csC := r.camera.WorldToCamera(Vec3{fwdX, wallHeight, fwdZ})
		sxC, syC, _, visC := r.camera.Project(csC, w, h)
		if visC {
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					r.raster.setPixel(sxC+dx, syC+dy, 0.01, 15) // bright white = ceiling
				}
			}
		}
		if r.debugFrame%250 == 1 {
			if visF {
				fmt.Printf("  FLOOR ahead: screen=(%d,%d)\n", sxF, syF)
			}
			if visC {
				fmt.Printf("  CEILING ahead: screen=(%d,%d)\n", sxC, syC)
			}
		}
	}

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
	if r.debugFrame%250 == 2 {
		fmt.Println("  Decorations:")
	}
	r.renderDecorations(s, w, h)

	// Convert to RGBA (HUD is drawn by the 2D renderer, not here)
	r.raster.ToRGBA(r.rgbaOut)
	return r.rgbaOut
}

// renderDecorations draws ALL room decorations projected onto walls.
// Colour is extracted from GenDecoAttrs (the attribute painting data).
// Decoration depth is snapped to the wall surface so bases align with
// the wall wireframe floor line.
func (r *Renderer) renderDecorations(s RenderState, w, h int) {
	roomAttr := data.RoomAttrs[s.Room].Colour

	// Get wall boundary positions from room style
	ra := data.RoomAttrs[s.Room]
	style := data.RoomStyles[ra.Style]
	wallNorthPx := int(roomCentreY) - int(style.Height) // pixel Y of north wall
	wallSouthPx := int(roomCentreY) + int(style.Height)
	wallWestPx := int(roomCentreX) - int(style.Width)
	wallEastPx := int(roomCentreX) + int(style.Width)

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

		// Snap decoration depth to the wall surface so its floor line
		// aligns with the wall wireframe
		drawX, drawY := x, y
		switch wall {
		case WallNorth:
			drawY = wallNorthPx
		case WallSouth:
			drawY = wallSouthPx
		case WallWest:
			drawX = wallWestPx
		case WallEast:
			drawX = wallEastPx
		}
		RenderWallDecoration(r.raster, &r.camera, drawX, drawY, wall, sprData, attrData, roomAttr, w, h)
	}
}

