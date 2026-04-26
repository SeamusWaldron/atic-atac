package render3d

import "github.com/seamuswaldron/aticatac/data"

// Room coordinate system:
// Original 2D: X = 0-187 pixels (left-right), Y = 0-187 pixels (top-bottom)
// 3D world:    X = left-right, Y = up, Z = forward (into screen in top-down)
//
// Mapping: 3D_X = (2D_X - centreX) / scale
//          3D_Z = (2D_Y - centreY) / scale
//          3D_Y = 0 (floor) to wallHeight (ceiling)

const (
	roomCentreX = 0x60 // 96 — centre of playable area
	roomCentreY = 0x60 // 96
	coordScale  = 20.0 // pixels per world unit (lower = walls further away)
	wallHeight  = 1.5  // world units tall
)

// PixelToWorld converts a 2D room pixel coordinate to a 3D floor position.
func PixelToWorld(px, py int) Vec3 {
	return Vec3{
		X: float32(px-roomCentreX) / coordScale,
		Y: 0,
		Z: float32(py-roomCentreY) / coordScale,
	}
}

// BuildRoomWalls converts a room style into 3D wall quads.
// Each line segment in the room frame is extruded into a vertical wall.
func BuildRoomWalls(style *data.RoomStyle, attr data.RoomAttr) []Quad {
	ink := attr.Colour & 0x07
	paper := (attr.Colour >> 3) & 0x07
	bright := (attr.Colour >> 6) & 0x01
	// Walls are dark surfaces with bright wireframe edges — matching the
	// original look where rooms are black backgrounds with coloured lines.
	fillIdx := paper + bright*8
	edgeIdx := ink + bright*8

	var quads []Quad
	pts := style.Points

	for _, lg := range style.Lines {
		src := int(lg.Src)
		if src >= len(pts) {
			continue
		}
		sx, sy := float32(pts[src].X), float32(pts[src].Y)

		for _, dst := range lg.Dsts {
			di := int(dst)
			if di >= len(pts) {
				continue
			}
			dx, dy := float32(pts[di].X), float32(pts[di].Y)

			// Convert 2D endpoints to 3D floor positions
			x0 := (sx - float32(roomCentreX)) / coordScale
			z0 := (sy - float32(roomCentreY)) / coordScale
			x1 := (dx - float32(roomCentreX)) / coordScale
			z1 := (dy - float32(roomCentreY)) / coordScale

			// Extrude into a vertical wall quad.
			// Y is negated so that after the vertical buffer flip in ToRGBA,
			// floor (Y=0) appears at the bottom and ceiling at the top.
			q := Quad{
				Verts: [4]Vec3{
					{x0, 0, z0},            // bottom-left (floor)
					{x1, 0, z1},            // bottom-right (floor)
					{x1, -wallHeight, z1},  // top-right (ceiling)
					{x0, -wallHeight, z0},  // top-left (ceiling)
				},
				Color:     fillIdx,
				EdgeColor: edgeIdx,
			}
			quads = append(quads, q)
		}
	}

	return quads
}

// WallCache holds pre-built wall geometry for each room style+colour combination.
// Key: room number (0-149).
type WallCache struct {
	Walls [data.NumRooms][]Quad
}

// BuildWallCache pre-computes wall geometry for all 150 rooms.
func BuildWallCache() *WallCache {
	wc := &WallCache{}
	for i := 0; i < data.NumRooms; i++ {
		attr := data.RoomAttrs[i]
		styleIdx := int(attr.Style)
		if styleIdx >= len(data.RoomStyles) {
			continue
		}
		wc.Walls[i] = BuildRoomWalls(&data.RoomStyles[styleIdx], attr)
	}
	return wc
}
