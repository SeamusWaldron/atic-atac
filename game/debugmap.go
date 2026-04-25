package game

import (
	"fmt"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/seamuswaldron/aticatac/data"
	"github.com/seamuswaldron/aticatac/engine"
	"github.com/seamuswaldron/aticatac/entity"
	"github.com/seamuswaldron/aticatac/screen"
)

var floors = []struct {
	Name  string
	Start int
	End   int
}{
	{"GROUND FLOOR", 0, 29},
	{"BASEMENT", 29, 86},
	{"FIRST FLOOR", 86, 115},
	{"ATTIC", 115, 142},
}

// Room positions per floor, derived from door-only BFS with multi-seed
// placement for disconnected clusters. Doors-only (types $01-$0F).
var floorRoomPos = [4]map[int][2]int{
	// Floor 0: ground (29 rooms, 6x7, 87% adj correct)
	{0:{5,3},1:{5,2},2:{4,2},3:{2,2},4:{3,3},5:{3,4},6:{4,4},7:{5,4},8:{4,5},9:{4,6},10:{3,6},11:{2,6},12:{1,6},13:{0,6},14:{0,5},15:{0,4},16:{0,3},17:{0,2},18:{0,1},19:{0,0},20:{1,0},21:{2,0},22:{3,0},23:{4,0},24:{4,1},25:{4,3},26:{3,2},27:{3,1},28:{5,1}},
	// Floor 1: basement (57 rooms, 11x12, 93% adj correct)
	{29:{6,3},30:{10,7},31:{10,6},32:{9,6},33:{8,6},34:{7,7},35:{8,8},36:{9,8},37:{10,8},38:{8,7},39:{9,9},40:{9,10},41:{9,11},42:{8,11},43:{7,11},44:{7,10},45:{7,9},46:{8,9},47:{9,7},48:{0,1},49:{0,2},50:{0,3},51:{0,4},52:{0,5},53:{0,6},54:{1,4},55:{2,4},56:{3,4},57:{3,5},58:{4,4},59:{4,5},60:{4,6},61:{5,5},62:{5,6},63:{6,5},64:{7,5},65:{8,5},66:{7,4},67:{7,3},68:{8,3},69:{9,3},70:{7,2},71:{6,2},72:{5,2},73:{5,3},74:{4,2},75:{3,2},76:{3,1},77:{5,1},78:{5,0},79:{6,0},80:{7,0},81:{7,1},82:{8,0},83:{9,0},84:{1,1},85:{2,1}},
	// Floor 2: first (29 rooms, 10x9, 78% adj correct)
	{86:{1,1},87:{2,1},88:{3,1},89:{4,1},90:{1,2},91:{2,2},92:{3,2},93:{4,2},94:{1,3},95:{2,3},96:{4,3},97:{5,3},98:{1,4},99:{2,4},100:{4,4},101:{5,4},102:{3,3},103:{1,0},104:{4,0},105:{0,1},106:{0,4},107:{6,5},108:{7,5},109:{0,8},110:{1,8},111:{8,6},112:{8,7},113:{9,6},114:{9,5}},
	// Floor 3: attic (27 rooms, 10x13, 100% adj correct)
	{115:{9,11},116:{0,12},117:{1,0},118:{1,1},119:{2,1},120:{3,1},121:{3,2},122:{3,3},123:{2,3},124:{1,3},125:{1,2},126:{4,3},127:{5,4},128:{6,4},129:{5,5},130:{6,5},131:{5,9},132:{6,9},133:{5,10},134:{6,10},135:{7,5},136:{8,5},137:{7,9},138:{8,9},139:{7,6},140:{7,7},141:{7,8}},
}

type DebugMap struct {
	Active    bool
	Floor     int
	Selected  int
	wasUp     bool
	wasDown   bool
	wasLeft   bool
	wasRight  bool
	wasEnter  bool
	wasEscape bool
}

func UpdateDebugMap(dm *DebugMap, eng *engine.GameEnv) int {
	positions := floorRoomPos[dm.Floor]

	// Get current selected room's grid position
	curPos, hasCur := positions[dm.Selected]

	// Arrow keys: move to nearest room in that direction on the map
	up := ebiten.IsKeyPressed(ebiten.KeyArrowUp)
	if up && !dm.wasUp && hasCur {
		dm.Selected = findNearest(positions, floors[dm.Floor], curPos, 0, -1)
	}
	dm.wasUp = up

	down := ebiten.IsKeyPressed(ebiten.KeyArrowDown)
	if down && !dm.wasDown && hasCur {
		dm.Selected = findNearest(positions, floors[dm.Floor], curPos, 0, 1)
	}
	dm.wasDown = down

	left := ebiten.IsKeyPressed(ebiten.KeyArrowLeft)
	if left && !dm.wasLeft && hasCur {
		dm.Selected = findNearest(positions, floors[dm.Floor], curPos, -1, 0)
	}
	dm.wasLeft = left

	right := ebiten.IsKeyPressed(ebiten.KeyArrowRight)
	if right && !dm.wasRight && hasCur {
		dm.Selected = findNearest(positions, floors[dm.Floor], curPos, 1, 0)
	}
	dm.wasRight = right

	// Q/A: switch floor
	qKey := ebiten.IsKeyPressed(ebiten.KeyQ)
	if qKey && !dm.wasEscape {
		dm.Floor--
		if dm.Floor < 0 { dm.Floor = len(floors) - 1 }
		dm.Selected = floors[dm.Floor].Start
	}
	// reuse wasEscape for Q debounce
	aKey := ebiten.IsKeyPressed(ebiten.KeyA)
	if aKey && !dm.wasEnter {
		dm.Floor++
		if dm.Floor >= len(floors) { dm.Floor = 0 }
		dm.Selected = floors[dm.Floor].Start
	}

	// Enter: warp
	enter := ebiten.IsKeyPressed(ebiten.KeyEnter)
	if enter && !dm.wasEnter {
		dm.Active = false
		return dm.Selected
	}
	dm.wasEnter = enter || aKey

	// Escape: close
	esc := ebiten.IsKeyPressed(ebiten.KeyEscape)
	if esc && !dm.wasEscape { dm.Active = false }
	dm.wasEscape = esc || qKey

	return -1
}

// findNearest finds the closest room in the given direction (dx, dy) from curPos.
func findNearest(positions map[int][2]int, f struct{ Name string; Start, End int }, curPos [2]int, dx, dy int) int {
	bestRoom := -1
	bestDist := 9999

	for room := f.Start; room < f.End; room++ {
		p, ok := positions[room]
		if !ok { continue }
		rx, ry := p[0]-curPos[0], p[1]-curPos[1]

		// Must be in the requested direction
		if dx < 0 && rx >= 0 { continue }
		if dx > 0 && rx <= 0 { continue }
		if dy < 0 && ry >= 0 { continue }
		if dy > 0 && ry <= 0 { continue }

		// Manhattan distance, prefer rooms closer to the main axis
		dist := abs(rx) + abs(ry)
		// Penalise off-axis distance
		if dx != 0 { dist += abs(ry) * 3 }
		if dy != 0 { dist += abs(rx) * 3 }

		if dist < bestDist {
			bestDist = dist
			bestRoom = room
		}
	}

	if bestRoom >= 0 {
		return bestRoom
	}
	return curPos[0] // no change
}

func abs(x int) int {
	if x < 0 { return -x }
	return x
}

// drawMiniRoom draws a tiny floor plan of a room at pixel position (cx, cy)
// scaled to fit in cellW x cellH pixels.
func drawMiniRoom(buf *screen.Buffer, room int, cx, cy, cellW, cellH int) {
	if room >= data.NumRooms { return }
	ra := data.RoomAttrs[room]
	if int(ra.Style) >= len(data.RoomStyles) { return }
	style := data.RoomStyles[ra.Style]

	sx := float64(cellW-2) / 191.0
	sy := float64(cellH-2) / 191.0

	for _, lg := range style.Lines {
		if len(lg.Dsts) == 0 { continue }
		srcIdx := int(lg.Src)
		if srcIdx >= len(style.Points) { continue }
		x1 := cx + 1 + int(float64(style.Points[srcIdx].X)*sx)
		y1 := cy + 1 + int(float64(style.Points[srcIdx].Y)*sy)
		for _, di := range lg.Dsts {
			dstIdx := int(di)
			if dstIdx >= len(style.Points) { continue }
			x2 := cx + 1 + int(float64(style.Points[dstIdx].X)*sx)
			y2 := cy + 1 + int(float64(style.Points[dstIdx].Y)*sy)
			buf.DrawLine(x1, y1, x2, y2)
		}
	}
}

// getCrossFloorExits returns directions (N/S/E/W) of doors leading to other floors.
func getCrossFloorExits(room int, floorStart, floorEnd int) []string {
	if room >= data.NumRooms { return nil }
	ra := data.RoomAttrs[room]
	if int(ra.Style) >= len(data.RoomStyles) { return nil }
	s := data.RoomStyles[ra.Style]
	rw, rh := int(s.Width), int(s.Height)
	cx, cy := 0x58, 0x68

	var dirs []string
	seen := make(map[string]bool)
	for _, pair := range data.GenRoomEntityData[room] {
		for side := 0; side < 2; side++ {
			var e, other [8]byte
			if side == 0 { copy(e[:], pair[0:8]); copy(other[:], pair[8:16]) } else { copy(e[:], pair[8:16]); copy(other[:], pair[0:8]) }
			if int(e[1]) != room { continue }
			dest := int(other[1])
			if dest == room || dest >= data.NumRooms { continue }
			if dest >= floorStart && dest < floorEnd { continue } // same floor
			t := e[0]
			if !((t >= 0x01 && t <= 0x03) || (t >= 0x08 && t <= 0x0F)) { continue }
			x, y := int(e[3]), int(e[4])
			dir := ""
			if y < cy-rh { dir = "N" }
			if y > cy+rh { dir = "S" }
			if x < cx-rw { dir = "W" }
			if x > cx+rw { dir = "E" }
			if dir != "" && !seen[dir] {
				seen[dir] = true
				dirs = append(dirs, dir)
			}
		}
	}
	return dirs
}

// drawCrossFloorArrows draws small arrows on room edges that lead to other floors.
func drawCrossFloorArrows(buf *screen.Buffer, x, y, cellW, cellH int, dirs []string) {
	midX := x + cellW/2
	midY := y + cellH/2
	for _, dir := range dirs {
		switch dir {
		case "N": // arrow at top centre
			buf.SetPixel(midX, y)
			buf.SetPixel(midX-1, y+1)
			buf.SetPixel(midX+1, y+1)
		case "S": // arrow at bottom centre
			buf.SetPixel(midX, y+cellH-1)
			buf.SetPixel(midX-1, y+cellH-2)
			buf.SetPixel(midX+1, y+cellH-2)
		case "W": // arrow at left centre
			buf.SetPixel(x, midY)
			buf.SetPixel(x+1, midY-1)
			buf.SetPixel(x+1, midY+1)
		case "E": // arrow at right centre
			buf.SetPixel(x+cellW-1, midY)
			buf.SetPixel(x+cellW-2, midY-1)
			buf.SetPixel(x+cellW-2, midY+1)
		}
	}
}

func DrawDebugMap(buf *screen.Buffer, dm *DebugMap, eng *engine.GameEnv) {
	for i := range buf.Pixels { buf.Pixels[i] = 0 }
	buf.FillAttrArea(0, 0, 32, 24, 0x00)

	f := floors[dm.Floor]
	positions := floorRoomPos[dm.Floor]
	cs := &data.GenCharset

	// Collect entity markers
	playerRoom := eng.Room()
	type roomMark struct { marker string; attr byte }
	marks := make(map[byte]roomMark)
	for i := range eng.Entities().Entities {
		e := &eng.Entities().Entities[i]
		if !e.Active { continue }
		switch e.Type {
		case entity.TypeKey:
			m := ""
			switch {
			case e.Graphic == 0x81 && e.Attr == 0x42: m = "R"
			case e.Graphic == 0x81 && e.Attr == 0x44: m = "G"
			case e.Graphic == 0x81 && e.Attr == 0x45: m = "C"
			case e.Graphic == 0x81 && e.Attr == 0x46: m = "Y"
			case e.Graphic == 0x8C: m = "1"
			case e.Graphic == 0x8D: m = "2"
			case e.Graphic == 0x8E: m = "3"
			}
			if m != "" { marks[e.Room] = roomMark{m, 0x44} }
		case entity.TypeBoss:
			m := ""
			switch e.Timer {
			case entity.BossMummy: m = "M"
			case entity.BossDracula: m = "D"
			case entity.BossDevil: m = "V"
			case entity.BossFrankenstein: m = "F"
			case entity.BossHunchback: m = "H"
			}
			if m != "" { marks[e.Room] = roomMark{m, 0x42} }
		}
	}

	// Find grid dimensions from positions
	maxCol, maxRow := 0, 0
	for _, p := range positions {
		if p[0] > maxCol { maxCol = p[0] }
		if p[1] > maxRow { maxRow = p[1] }
	}
	gridCols := maxCol + 1
	gridRows := maxRow + 1

	// Cell size to fit in play area (192 wide, ~168 tall after header/footer)
	cellW := 188 / gridCols
	cellH := 156 / gridRows
	if cellW > 28 { cellW = 28 }
	if cellH > 22 { cellH = 22 }
	// Keep aspect ratio reasonable
	if cellW > cellH*2 { cellW = cellH * 2 }
	if cellH > cellW*2 { cellH = cellW * 2 }

	gridW := gridCols * cellW
	gridH := gridRows * cellH
	startX := (192 - gridW) / 2
	startY := 12 + (164-gridH)/2
	if startX < 0 { startX = 0 }
	if startY < 12 { startY = 12 }

	// Draw each room at its map position
	for room := f.Start; room < f.End; room++ {
		p, ok := positions[room]
		if !ok { continue }
		x := startX + p[0]*cellW
		y := startY + p[1]*cellH

		// Clip to play area
		if x < 0 || x+cellW > 192 || y < 0 || y+cellH > 192 { continue }

		// Draw miniature room shape
		drawMiniRoom(buf, room, x, y, cellW, cellH)

		// Draw arrows for cross-floor exits
		crossDirs := getCrossFloorExits(room, f.Start, f.End)
		if len(crossDirs) > 0 {
			drawCrossFloorArrows(buf, x, y, cellW, cellH, crossDirs)
		}

		// Attr colour
		ra := data.RoomAttrs[room]
		attr := ra.Colour
		if attr == 0 { attr = 0x07 }

		// Markers
		if mk, ok := marks[byte(room)]; ok {
			attr = mk.attr
			buf.DrawStringFrom(x+cellW/2-3, y+cellH/2-3, mk.marker, cs)
		}
		if byte(room) == playerRoom {
			attr = 0x46
			buf.DrawStringFrom(x+cellW/2-3, y+cellH/2-3, "P", cs)
		}
		if room == dm.Selected {
			attr |= 0x80
		}

		// Paint cell attrs
		for ar := y >> 3; ar <= (y+cellH-1)>>3 && ar < 24; ar++ {
			for ac := x >> 3; ac <= (x+cellW-1)>>3 && ac < 24; ac++ {
				buf.SetCellAttr(ac, ar, attr)
			}
		}
	}

	// Header
	buf.DrawStringFrom(4, 0, fmt.Sprintf("%s %d/%d", f.Name, dm.Floor+1, len(floors)), cs)
	buf.FillAttrArea(0, 0, 24, 1, 0x47)

	// Info line
	info := fmt.Sprintf("$%02X", dm.Selected)
	if mk, ok := marks[byte(dm.Selected)]; ok { info += " " + mk.marker }
	if byte(dm.Selected) == playerRoom { info += " *YOU*" }
	if xd := getCrossFloorExits(dm.Selected, f.Start, f.End); len(xd) > 0 {
		for _, d := range xd { info += " " + d + "!" }
	}
	buf.DrawStringFrom(4, 184, info, cs)
	buf.FillAttrArea(0, 23, 24, 1, 0x46)

	// Controls
	buf.DrawStringFrom(4, 176, "Q/A#FLR O/P#RM ENT#GO", cs)
	buf.FillAttrArea(0, 22, 24, 1, 0x45)
}
