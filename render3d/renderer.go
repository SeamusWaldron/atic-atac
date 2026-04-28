package render3d

import (
	"fmt"
	"math"

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

	// Weapon state (active = false if no weapon in flight)
	WeaponActive  bool
	WeaponX       int
	WeaponY       int
	WeaponGraphic byte
	WeaponAttr    byte

	// DoorType returns the runtime door state for (room, entityIdx).
	// Used to pick open vs closed door sprites; nil-safe.
	DoorType func(room byte, entityIdx int) byte
}

// Renderer is the 3D rendering engine.
type Renderer struct {
	raster     *Raster
	wallCache  *WallCache
	camera     Camera
	rgbaOut    []byte
	debugFrame int
	// ShowMarkers draws coloured debug markers at wall centres, corners, and
	// the floor/ceiling 3 units ahead. Off by default; flip to re-enable.
	ShowMarkers bool
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
	r.camera.Y = 0.65 // eye height (below wall midpoint of 0.75)
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
	if r.ShowMarkers {
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
	// Edges are clipped against the near plane so nearby walls don't vanish.
	walls := r.wallCache.Walls[s.Room]
	for i := range walls {
		q := &walls[i]

		// Transform all 4 vertices to camera space
		var cs [4]Vec3
		for j := 0; j < 4; j++ {
			cs[j] = r.camera.WorldToCamera(q.Verts[j])
		}

		// Check if ALL vertices are behind near plane — skip entirely
		allBehind := true
		for j := 0; j < 4; j++ {
			if cs[j].Z > r.camera.Near {
				allBehind = false
				break
			}
		}
		if allBehind {
			continue
		}

		// Project visible vertices; clip those behind near plane
		var sv [4]ScreenVert
		var visible [4]bool
		for j := 0; j < 4; j++ {
			if cs[j].Z > r.camera.Near {
				sx, sy, depth, _ := r.camera.Project(cs[j], w, h)
				sv[j] = ScreenVert{float32(sx), float32(sy), depth}
				visible[j] = true
			}
		}

		// Fill quad only if all vertices visible (simple case)
		if visible[0] && visible[1] && visible[2] && visible[3] {
			r.raster.FillQuad(sv[0], sv[1], sv[2], sv[3], q.Color)
		}

		// Draw edges with near-plane clipping
		for j := 0; j < 4; j++ {
			k := (j + 1) % 4
			r.drawClippedEdge(cs[j], cs[k], q.EdgeColor, w, h)
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

	// Render weapon (axe/fireball/sword) as a billboard
	if s.WeaponActive {
		fakeEntity := entity.Entity{
			Active:  true,
			X:       s.WeaponX,
			Y:       s.WeaponY,
			Graphic: s.WeaponGraphic,
			Attr:    s.WeaponAttr,
		}
		RenderSpriteBillboard(r.raster, &r.camera, &fakeEntity, w, h)
	}

	// Render ALL room decorations (doors, arches, shields, torches, etc.)
	if r.debugFrame%250 == 2 {
		fmt.Println("  Decorations:")
	}
	r.renderDecorations(s, w, h)

	// Render floor decorations (trap doors)
	r.renderFloorDecorations(s, w, h)

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

	// Get wall surface positions from the inner boundary points.
	// style.Width/Height are collision bounds, not wall positions — use the
	// actual inner-boundary point min/max instead.
	ra := data.RoomAttrs[s.Room]
	style := data.RoomStyles[ra.Style]
	innerStart := len(style.Points) / 2
	wallWestPx, wallEastPx := 255, 0
	wallNorthPx, wallSouthPx := 255, 0
	for i := innerStart; i < len(style.Points); i++ {
		p := style.Points[i]
		px, py := int(p.X), int(p.Y)
		if px < wallWestPx {
			wallWestPx = px
		}
		if px > wallEastPx {
			wallEastPx = px
		}
		if py < wallNorthPx {
			wallNorthPx = py
		}
		if py > wallSouthPx {
			wallSouthPx = py
		}
	}

	entities := data.GenRoomEntityData[int(s.Room)]
	for ei, pair := range entities {
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

		// Skip floor-only decorations (trap doors). They shouldn't render as
		// wall decorations. TODO: render them flat on the floor.
		if typeID == 0x18 || typeID == 0x19 {
			continue
		}

		// Look up decoration sprite data
		gfxIdx := typeID - 1
		if gfxIdx < 0 || gfxIdx >= 39 {
			continue
		}

		// Apply runtime door state (mirrors engine.drawDecorations logic).
		// Without this, closed doors render as the open arch sprite.
		if s.DoorType != nil {
			rt := s.DoorType(s.Room, ei)
			if rt != 0 {
				if rt == 0x18 || rt == 0x19 {
					gfxIdx = int(rt) - 1
				} else if rt == 0x02 || (rt >= 0x20 && rt&0x01 != 0) {
					if typeID >= 0x08 && typeID <= 0x0F {
						gfxIdx = 1
					}
				} else if rt >= 0x20 && rt&0x01 == 0 {
					gfxIdx = 31
				}
			}
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

		// Back-face culling: skip decorations on walls that face away from the camera.
		// Wall normals point INTO the room (away from the wall surface):
		//   N wall: normal = +Z, visible when cos(yaw) < 0 (camera looks toward -Z, north)
		//   S wall: normal = -Z, visible when cos(yaw) > 0 (camera looks toward +Z, south)
		//   E wall: normal = -X, visible when sin(yaw) < 0 (camera looks toward +X, east)
		//   W wall: normal = +X, visible when sin(yaw) > 0 (camera looks toward -X, west)
		// Forward F = (-sin yaw, 0, cos yaw); render if F·normal < 0.
		camS := float32(math.Sin(float64(r.camera.Yaw)))
		camC := float32(math.Cos(float64(r.camera.Yaw)))
		visible := false
		switch wall {
		case WallNorth:
			visible = camC < 0
		case WallSouth:
			visible = camC > 0
		case WallEast:
			visible = camS < 0
		case WallWest:
			visible = camS > 0
		default:
			visible = true
		}
		if !visible {
			continue
		}

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

		if r.debugFrame%250 == 2 {
			wallName := "?"
			switch wall {
			case WallNorth:
				wallName = "N"
			case WallSouth:
				wallName = "S"
			case WallWest:
				wallName = "W"
			case WallEast:
				wallName = "E"
			}
			fmt.Printf("    deco type=0x%02X pos=(%d,%d)→(%d,%d) wall=%s mode=%d\n",
				typeID, x, y, drawX, drawY, wallName, mode)
		}

		mount := decorationMount(typeID)
		RenderWallDecoration(r.raster, &r.camera, drawX, drawY, wall, mount, sprData, attrData, roomAttr, w, h)
	}
}

// drawClippedEdge draws a line between two camera-space points, clipping
// against the near plane. If one endpoint is behind the camera, the line
// is clipped to the near plane intersection.
func (r *Renderer) drawClippedEdge(a, b Vec3, color byte, screenW, screenH int) {
	nearZ := r.camera.Near

	aFront := a.Z > nearZ
	bFront := b.Z > nearZ

	if !aFront && !bFront {
		return // both behind
	}

	// Clip to near plane if needed
	ca, cb := a, b
	if !aFront {
		// Clip a to near plane
		t := (nearZ - a.Z) / (b.Z - a.Z)
		ca = Vec3{a.X + t*(b.X-a.X), a.Y + t*(b.Y-a.Y), nearZ + 0.001}
	}
	if !bFront {
		// Clip b to near plane
		t := (nearZ - b.Z) / (a.Z - b.Z)
		cb = Vec3{b.X + t*(a.X-b.X), b.Y + t*(a.Y-b.Y), nearZ + 0.001}
	}

	sx0, sy0, d0, v0 := r.camera.Project(ca, screenW, screenH)
	sx1, sy1, d1, v1 := r.camera.Project(cb, screenW, screenH)
	if !v0 || !v1 {
		return
	}

	r.raster.DrawLine(sx0, sy0, d0, sx1, sy1, d1, color)
}

// decorationMount returns how a decoration type is positioned vertically.
// Floor-mounted: doors, suits of armour, ACG door (full wall height, anchored at floor)
// Wall-hung: portraits, shields, torches, small decorations (centred mid-wall)
func decorationMount(typeID int) MountType {
	switch typeID {
	// Door types (0x01-0x0F): floor-mounted
	case 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F:
		return MountFloor
	// Suit of armour, ACG door, ACG exit
	case 0x1E, 0x24:
		return MountFloor
	}
	// Everything else (portraits, shields, torches, ghosts, food, etc.) hangs on wall
	return MountWallHung
}

// renderFloorDecorations draws decorations that lie flat on the floor
// (trap doors). Each decoration is rendered as a small horizontal quad
// at floor level (Y=0).
func (r *Renderer) renderFloorDecorations(s RenderState, w, h int) {
	roomAttr := data.RoomAttrs[s.Room].Colour
	entities := data.GenRoomEntityData[int(s.Room)]
	for _, pair := range entities {
		var e [8]byte
		if pair[1] == s.Room {
			copy(e[:], pair[0:8])
		} else if pair[9] == s.Room {
			copy(e[:], pair[8:16])
		} else {
			continue
		}
		typeID := int(e[0])
		// Only trap doors for now
		if typeID != 0x18 && typeID != 0x19 {
			continue
		}
		x := int(e[3])
		y := int(e[4])
		gfxIdx := typeID - 1
		sprData, ok := data.GenDecoSprites[gfxIdx]
		if !ok || len(sprData) < 2 {
			continue
		}
		widthBytes := int(sprData[0])
		spriteH := int(sprData[1])
		widthPx := widthBytes * 8

		// Build a flat quad on the floor centred at (x+widthPx/2, y-spriteH/2)
		// (entity y is bottom of displayed sprite in 2D top-down).
		centreX := x + widthPx/2
		centreY := y - spriteH/2
		halfW := float32(widthPx) / 2 / coordScale
		halfD := float32(spriteH) / 2 / coordScale
		basePos := PixelToWorld(centreX, centreY)

		// Floor quad corners (Y=0 for all four)
		corners := [4]Vec3{
			{basePos.X - halfW, 0.01, basePos.Z + halfD}, // BL (south-west)
			{basePos.X + halfW, 0.01, basePos.Z + halfD}, // BR (south-east)
			{basePos.X + halfW, 0.01, basePos.Z - halfD}, // TR (north-east)
			{basePos.X - halfW, 0.01, basePos.Z - halfD}, // TL (north-west)
		}

		// Project corners
		var pv [4]projVert
		allVis := true
		for i := 0; i < 4; i++ {
			cs := r.camera.WorldToCamera(corners[i])
			if cs.Z <= r.camera.Near {
				allVis = false
				break
			}
			sx, sy, depth, vis := r.camera.Project(cs, w, h)
			if !vis {
				allVis = false
				break
			}
			pv[i] = projVert{sx, sy, depth, 1.0 / depth}
		}
		if !allVis {
			continue
		}

		// Texture-map the trap door sprite onto the floor quad.
		// Corner UV mapping (matches quadUV expectations):
		//   c0=BL(u=0,v=1), c1=BR(u=1,v=1), c2=TR(u=1,v=0), c3=TL(u=0,v=0)
		sprPixels := sprData[2:]
		attrs := data.GenDecoAttrs[gfxIdx] // may be nil
		attrW, attrH := 0, 0
		if attrs != nil && len(attrs) >= 2 {
			attrW = int(attrs[0])
			attrH = int(attrs[1])
		}

		// Screen bounding box for the projected quad
		minX, maxX := pv[0].sx, pv[0].sx
		minY, maxY := pv[0].sy, pv[0].sy
		for i := 1; i < 4; i++ {
			if pv[i].sx < minX {
				minX = pv[i].sx
			}
			if pv[i].sx > maxX {
				maxX = pv[i].sx
			}
			if pv[i].sy < minY {
				minY = pv[i].sy
			}
			if pv[i].sy > maxY {
				maxY = pv[i].sy
			}
		}
		if minX < 0 {
			minX = 0
		}
		if minY < 0 {
			minY = 0
		}
		if maxX >= w {
			maxX = w - 1
		}
		if maxY >= h {
			maxY = h - 1
		}

		// Open trap doors render as a dark hole rather than the cross-hatch
		// sprite — matches the visual cue that you can fall through.
		openHole := typeID == 0x19

		for sy := minY; sy <= maxY; sy++ {
			for sx := minX; sx <= maxX; sx++ {
				u, v, depth, inside := quadUV(float32(sx)+0.5, float32(sy)+0.5, pv)
				if !inside {
					continue
				}
				if openHole {
					r.raster.setPixel(sx, sy, depth-0.005, 0) // black hole
					continue
				}
				sprCol := int(u * float32(widthPx))
				sprRow := int(v * float32(spriteH))
				if sprCol < 0 {
					sprCol = 0
				}
				if sprRow < 0 {
					sprRow = 0
				}
				if sprCol >= widthPx {
					sprCol = widthPx - 1
				}
				if sprRow >= spriteH {
					sprRow = spriteH - 1
				}
				byteIdx := sprCol / 8
				bitIdx := uint(7 - sprCol%8)
				if sprPixels[sprRow*widthBytes+byteIdx]&(1<<bitIdx) == 0 {
					continue
				}
				cellCol := sprCol / 8
				cellRow := sprRow / 8
				colorIdx := attrColorForCell(attrs, attrW, attrH, cellCol, cellRow, widthBytes, roomAttr)
				r.raster.setPixel(sx, sy, depth-0.005, colorIdx)
			}
		}
	}
}
