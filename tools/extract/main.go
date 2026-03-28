// extract parses aticatac.skool and emits Go source files with exact binary data.
//
// Usage: go run ./tools/extract
//
// It reads the .skool file, builds a flat memory image (address → byte),
// then extracts specific regions by address into Go data tables.
package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// mem is the flat memory image: address → byte value.
var mem [65536]byte
var memSet [65536]bool // tracks which addresses have data

// labels maps label name → address.
var labels map[string]uint16

func main() {
	labels = make(map[string]uint16)

	f, err := os.Open("aticatac.skool")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	parseSkool(f)

	fmt.Fprintf(os.Stderr, "Parsed %d bytes of data, %d labels\n", countSet(), len(labels))

	// Generate all data files
	genPanelData()
	genRoomStyles()
	genRoomAttrs()
	genRoomTable()
	genSpriteTable()
	genDecorationSprites()
	genDoorData()
	genPlayerSprites()
	genCreatureSprites()
	genItemData()
	genRoomEntities()
}

func countSet() int {
	n := 0
	for _, s := range memSet {
		if s {
			n++
		}
	}
	return n
}

// ---------- SKOOL PARSER ----------

var (
	reLabel = regexp.MustCompile(`@label=(\w+)`)
	reData  = regexp.MustCompile(`^\s*[a-zA-Z]?\$([0-9a-fA-F]+)\s+(defb|defw|defs)\s+(.*)`)
)

func parseSkool(f *os.File) {
	scanner := bufio.NewScanner(f)
	var pendingLabel string

	for scanner.Scan() {
		line := scanner.Text()

		// Check for label
		if m := reLabel.FindStringSubmatch(line); m != nil {
			pendingLabel = m[1]
		}

		// Check for data definition
		if m := reData.FindStringSubmatch(line); m != nil {
			addr, _ := strconv.ParseUint(m[1], 16, 16)
			directive := m[2]
			operands := m[3]

			// Strip comments
			if idx := strings.Index(operands, ";"); idx >= 0 {
				operands = operands[:idx]
			}
			operands = strings.TrimSpace(operands)

			if pendingLabel != "" {
				labels[pendingLabel] = uint16(addr)
				pendingLabel = ""
			}

			cur := uint16(addr)
			switch directive {
			case "defb":
				for _, tok := range splitOperands(operands) {
					v, err := strconv.ParseUint(strings.TrimPrefix(tok, "$"), 16, 8)
					if err != nil {
						continue
					}
					mem[cur] = byte(v)
					memSet[cur] = true
					cur++
				}
			case "defw":
				for _, tok := range splitOperands(operands) {
					v, err := strconv.ParseUint(strings.TrimPrefix(tok, "$"), 16, 16)
					if err != nil {
						continue
					}
					mem[cur] = byte(v & 0xFF)     // LSB
					memSet[cur] = true
					mem[cur+1] = byte(v >> 8)     // MSB
					memSet[cur+1] = true
					cur += 2
				}
			case "defs":
				// defs N fills N zero bytes
				v, err := strconv.ParseUint(strings.TrimPrefix(operands, "$"), 16, 16)
				if err == nil {
					for i := uint16(0); i < uint16(v); i++ {
						mem[cur+i] = 0
						memSet[cur+i] = true
					}
				}
			}
		}
	}
}

func splitOperands(s string) []string {
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// ---------- MEMORY ACCESS HELPERS ----------

func getByte(addr uint16) byte {
	return mem[addr]
}

func getWord(addr uint16) uint16 {
	return uint16(mem[addr]) | (uint16(mem[addr+1]) << 8)
}

func getBytes(addr uint16, n int) []byte {
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = mem[addr+uint16(i)]
	}
	return out
}

// ---------- CODE GENERATORS ----------

func genPanelData() {
	f := createFile("data/gen_panel.go")
	defer f.Close()

	fmt.Fprintln(f, "package data")
	fmt.Fprintln(f, "")
	fmt.Fprintln(f, "// Auto-generated from aticatac.skool — do not edit.")
	fmt.Fprintln(f, "")

	// Panel chars at $B03A: each char is 8 bytes
	// Panel has indices up to ~$5D = 93, so extract 94 chars
	fmt.Fprintln(f, "// GenPanelChars: 94 custom characters (8 bytes each) from $B03A.")
	fmt.Fprintln(f, "var GenPanelChars = [94][8]byte{")
	base := uint16(0xB03A)
	for i := 0; i < 94; i++ {
		addr := base + uint16(i*8)
		b := getBytes(addr, 8)
		fmt.Fprintf(f, "\t{0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X}, // %d ($%02X)\n",
			b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7], i, i)
	}
	fmt.Fprintln(f, "}")
	fmt.Fprintln(f, "")

	// Panel grid at $B32A: 24 rows × 8 columns = 192 bytes
	fmt.Fprintln(f, "// GenPanelGrid: 24×8 grid of character indices from $B32A.")
	fmt.Fprintln(f, "var GenPanelGrid = [24][8]byte{")
	base = uint16(0xB32A)
	for row := 0; row < 24; row++ {
		addr := base + uint16(row*8)
		b := getBytes(addr, 8)
		fmt.Fprintf(f, "\t{0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X}, // row %d\n",
			b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7], row)
	}
	fmt.Fprintln(f, "}")
	fmt.Fprintln(f, "")

	// Chicken sprites
	// Full chicken at $C542: header 06 1E (width=6, height=30), then 180 bytes
	fmt.Fprintln(f, "// GenChickenFull: 6×30 pixel sprite from $C544 (skipping 2-byte header).")
	emitByteSlice(f, "GenChickenFull", 0xC544, 6*30)

	// Empty chicken at $C48C: header 06 1E, then 180 bytes
	fmt.Fprintln(f, "// GenChickenEmpty: 6×30 pixel sprite from $C48E (skipping 2-byte header).")
	emitByteSlice(f, "GenChickenEmpty", 0xC48E, 6*30)
}

func genRoomStyles() {
	f := createFile("data/gen_roomstyles.go")
	defer f.Close()

	fmt.Fprintln(f, "package data")
	fmt.Fprintln(f, "")
	fmt.Fprintln(f, "// Auto-generated from aticatac.skool — do not edit.")
	fmt.Fprintln(f, "")

	// Room styles at $A982: 13 entries × 6 bytes each
	fmt.Fprintln(f, "// GenRoomStyleEntries: 13 room style raw entries from $A982.")
	fmt.Fprintln(f, "// Format: [width, height, points_ptr_lo, points_ptr_hi, lines_ptr_lo, lines_ptr_hi]")
	fmt.Fprintln(f, "var GenRoomStyleEntries = [13][6]byte{")
	base := uint16(0xA982)
	for i := 0; i < 13; i++ {
		addr := base + uint16(i*6)
		b := getBytes(addr, 6)
		ptsAddr := uint16(b[2]) | uint16(b[3])<<8
		lnsAddr := uint16(b[4]) | uint16(b[5])<<8
		fmt.Fprintf(f, "\t{0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X}, // style %d: W=%d H=%d pts=$%04X lns=$%04X\n",
			b[0], b[1], b[2], b[3], b[4], b[5], i, b[0], b[1], ptsAddr, lnsAddr)
	}
	fmt.Fprintln(f, "}")
}

func genRoomAttrs() {
	f := createFile("data/gen_roomattrs.go")
	defer f.Close()

	fmt.Fprintln(f, "package data")
	fmt.Fprintln(f, "")
	fmt.Fprintln(f, "// Auto-generated from aticatac.skool — do not edit.")
	fmt.Fprintln(f, "")

	// Room attrs at $A854: 150 entries × 2 bytes (colour, style)
	fmt.Fprintln(f, "// GenRoomAttrs: 150 room attributes from $A854.")
	fmt.Fprintln(f, "// Format: [colour_attr, style_index]")
	fmt.Fprintln(f, "var GenRoomAttrs = [150][2]byte{")
	base := uint16(0xA854)
	for i := 0; i < 150; i++ {
		addr := base + uint16(i*2)
		lo := getByte(addr)     // style (low byte of defw)
		hi := getByte(addr + 1) // colour (high byte of defw)
		// The defw stores as little-endian: lo=style, hi=colour
		fmt.Fprintf(f, "\t{0x%02X,0x%02X}, // room %d\n", hi, lo, i)
	}
	fmt.Fprintln(f, "}")
}

func genRoomTable() {
	f := createFile("data/gen_roomtable.go")
	defer f.Close()

	fmt.Fprintln(f, "package data")
	fmt.Fprintln(f, "")
	fmt.Fprintln(f, "// Auto-generated from aticatac.skool — do not edit.")
	fmt.Fprintln(f, "")

	// Room table at $757D: 150 entries × 2 bytes (pointer to room entity list)
	fmt.Fprintln(f, "// GenRoomTable: 150 room pointers from $757D.")
	fmt.Fprintln(f, "var GenRoomTable = [150]uint16{")
	base := uint16(0x757D)
	for i := 0; i < 150; i++ {
		addr := base + uint16(i*2)
		ptr := getWord(addr)
		fmt.Fprintf(f, "\t0x%04X, // room %d\n", ptr, i)
	}
	fmt.Fprintln(f, "}")
	fmt.Fprintln(f, "")

	// Now extract each room's entity pointer list
	// Each room pointer leads to a null-terminated list of 2-byte pointers
	fmt.Fprintln(f, "// GenRoomEntityPtrs: per-room entity pointer lists (null-terminated).")
	fmt.Fprintln(f, "// Each entry is a 2-byte pointer into the entity data area.")
	fmt.Fprintln(f, "var GenRoomEntityPtrs = map[int][]uint16{")
	for i := 0; i < 150; i++ {
		addr := base + uint16(i*2)
		roomPtr := getWord(addr)
		if roomPtr == 0 {
			continue
		}
		var ptrs []uint16
		cur := roomPtr
		for {
			p := getWord(cur)
			if p == 0 {
				break
			}
			ptrs = append(ptrs, p)
			cur += 2
		}
		if len(ptrs) > 0 {
			fmt.Fprintf(f, "\t%d: {", i)
			for j, p := range ptrs {
				if j > 0 {
					fmt.Fprint(f, ",")
				}
				fmt.Fprintf(f, "0x%04X", p)
			}
			fmt.Fprintln(f, "},")
		}
	}
	fmt.Fprintln(f, "}")
}

func genSpriteTable() {
	f := createFile("data/gen_spritetable.go")
	defer f.Close()

	fmt.Fprintln(f, "package data")
	fmt.Fprintln(f, "")
	fmt.Fprintln(f, "// Auto-generated from aticatac.skool — do not edit.")
	fmt.Fprintln(f, "")

	// Sprite table at $A4BE: 161 entries × 4 frames × 2 bytes = 4 pointers per group
	// Each group has 4 2-byte pointers to sprite data
	fmt.Fprintln(f, "// GenSpriteTable: sprite frame pointers from $A4BE.")
	fmt.Fprintln(f, "// 41 groups × 4 frames, each frame is a 2-byte pointer to sprite data.")
	fmt.Fprintln(f, "var GenSpriteTable = [41][4]uint16{")
	base := uint16(0xA4BE)
	for i := 0; i < 41; i++ {
		addr := base + uint16(i*8)
		f0 := getWord(addr)
		f1 := getWord(addr + 2)
		f2 := getWord(addr + 4)
		f3 := getWord(addr + 6)
		fmt.Fprintf(f, "\t{0x%04X,0x%04X,0x%04X,0x%04X}, // group %d\n", f0, f1, f2, f3, i)
	}
	fmt.Fprintln(f, "}")
}

func genDecorationSprites() {
	f := createFile("data/gen_decorations.go")
	defer f.Close()

	fmt.Fprintln(f, "package data")
	fmt.Fprintln(f, "")
	fmt.Fprintln(f, "// Auto-generated from aticatac.skool — do not edit.")
	fmt.Fprintln(f, "")

	// gfx_data table at $A600: 39 entries × 2 bytes (pointer to sprite)
	fmt.Fprintln(f, "// GenGfxDataPtrs: decoration type → sprite data pointer from $A600.")
	fmt.Fprintln(f, "var GenGfxDataPtrs = [39]uint16{")
	for i := 0; i < 39; i++ {
		addr := uint16(0xA600) + uint16(i*2)
		ptr := getWord(addr)
		fmt.Fprintf(f, "\t0x%04X, // type %d\n", ptr, i)
	}
	fmt.Fprintln(f, "}")
	fmt.Fprintln(f, "")

	// gfx_attrs table at $A64E: 39 entries × 2 bytes
	fmt.Fprintln(f, "// GenGfxAttrsPtrs: decoration type → attribute data pointer from $A64E.")
	fmt.Fprintln(f, "var GenGfxAttrsPtrs = [39]uint16{")
	for i := 0; i < 39; i++ {
		addr := uint16(0xA64E) + uint16(i*2)
		ptr := getWord(addr)
		fmt.Fprintf(f, "\t0x%04X, // type %d\n", ptr, i)
	}
	fmt.Fprintln(f, "}")
	fmt.Fprintln(f, "")

	// Extract actual sprite data for each gfx_data pointer
	// Sprite format: first byte = width, second byte = height, then width*height bytes
	fmt.Fprintln(f, "// GenDecoSprites: extracted sprite pixel data for each decoration type.")
	fmt.Fprintln(f, "// Format: first 2 bytes = [widthBytes, height], then widthBytes*height pixel bytes.")
	fmt.Fprintln(f, "var GenDecoSprites = map[int][]byte{")
	for i := 0; i < 39; i++ {
		addr := uint16(0xA600) + uint16(i*2)
		ptr := getWord(addr)
		if ptr == 0 || ptr == 0xAEEA { // skip null/empty
			continue
		}
		w := int(getByte(ptr))
		h := int(getByte(ptr + 1))
		if w == 0 || h == 0 || w > 8 || h > 48 {
			continue // sanity check
		}
		total := 2 + w*h
		b := getBytes(ptr, total)
		fmt.Fprintf(f, "\t%d: {", i)
		for j, v := range b {
			if j > 0 {
				fmt.Fprint(f, ",")
			}
			if j > 0 && j%16 == 0 {
				fmt.Fprintf(f, "\n\t\t")
			}
			fmt.Fprintf(f, "0x%02X", v)
		}
		fmt.Fprintf(f, "}, // type %d: %dx%d at $%04X\n", i, w, h, ptr)
	}
	fmt.Fprintln(f, "}")
	fmt.Fprintln(f, "")

	// Extract attribute data for each gfx_attrs pointer
	fmt.Fprintln(f, "// GenDecoAttrs: extracted attribute data for each decoration type.")
	fmt.Fprintln(f, "// Format: first 2 bytes = [width_cells, height_cells], then w*h attribute bytes.")
	fmt.Fprintln(f, "var GenDecoAttrs = map[int][]byte{")
	for i := 0; i < 39; i++ {
		addr := uint16(0xA64E) + uint16(i*2)
		ptr := getWord(addr)
		if ptr == 0 || ptr == 0xAEEA {
			continue
		}
		w := int(getByte(ptr))
		h := int(getByte(ptr + 1))
		if w == 0 || h == 0 || w > 8 || h > 8 {
			continue
		}
		total := 2 + w*h
		b := getBytes(ptr, total)
		fmt.Fprintf(f, "\t%d: {", i)
		for j, v := range b {
			if j > 0 {
				fmt.Fprint(f, ",")
			}
			fmt.Fprintf(f, "0x%02X", v)
		}
		fmt.Fprintf(f, "}, // type %d: %dx%d at $%04X\n", i, w, h, ptr)
	}
	fmt.Fprintln(f, "}")
}

func genDoorData() {
	f := createFile("data/gen_doors.go")
	defer f.Close()

	fmt.Fprintln(f, "package data")
	fmt.Fprintln(f, "")
	fmt.Fprintln(f, "// Auto-generated from aticatac.skool — do not edit.")
	fmt.Fprintln(f, "")

	// Door pair data starts at $645D, each pair is 16 bytes
	// Continue until we hit a region that doesn't look like door data
	fmt.Fprintln(f, "// GenDoorPairs: raw door pair data from $645D.")
	fmt.Fprintln(f, "// Each pair is 16 bytes: 8 bytes for side A, 8 bytes for side B.")
	fmt.Fprintln(f, "var GenDoorPairs = [][16]byte{")
	addr := uint16(0x645D)
	for i := 0; i < 100; i++ { // safety limit
		b := getBytes(addr, 16)
		// Stop if we hit non-door data (first byte should be a valid door type 0x01-0x0F)
		if b[0] == 0 && b[8] == 0 {
			break
		}
		if b[0] > 0x0F && b[8] > 0x0F {
			break
		}
		fmt.Fprintf(f, "\t{0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X, 0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X}, // pair %d at $%04X\n",
			b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7],
			b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15],
			i, addr)
		addr += 16
	}
	fmt.Fprintln(f, "}")
}

func genPlayerSprites() {
	f := createFile("data/gen_playersprites.go")
	defer f.Close()

	fmt.Fprintln(f, "package data")
	fmt.Fprintln(f, "")
	fmt.Fprintln(f, "// Auto-generated from aticatac.skool — do not edit.")
	fmt.Fprintln(f, "")

	// Player sprite groups: Knight=0-3, Wizard=4-7, Serf=8-11
	// Each group has 4 frame pointers at sprite_table ($A4BE)
	// Directions: 0=left, 1=right, 2=up, 3=down
	chars := []struct {
		name  string
		base  int // sprite table group index
	}{
		{"Knight", 0},
		{"Wizard", 4},
		{"Serf", 8},
	}

	for _, ch := range chars {
		fmt.Fprintf(f, "// Gen%sSprites: 4 directions × 3 unique frames.\n", ch.name)
		fmt.Fprintf(f, "var Gen%sSprites = [4][3][]byte{\n", ch.name)
		for dir := 0; dir < 4; dir++ {
			groupIdx := ch.base + dir
			groupAddr := uint16(0xA4BE) + uint16(groupIdx*8)
			// 4 frame pointers, pattern 0,1,2,1 — so 3 unique
			ptrs := [4]uint16{
				getWord(groupAddr),
				getWord(groupAddr + 2),
				getWord(groupAddr + 4),
				getWord(groupAddr + 6),
			}
			// Unique frames: ptrs[0], ptrs[1], ptrs[2]
			fmt.Fprintln(f, "\t{")
			for frame := 0; frame < 3; frame++ {
				sAddr := ptrs[frame]
				height := int(getByte(sAddr))
				total := 1 + height*2 // height byte + height*2 pixel bytes
				b := getBytes(sAddr, total)
				fmt.Fprintf(f, "\t\t{")
				for j, v := range b {
					if j > 0 {
						fmt.Fprint(f, ",")
					}
					fmt.Fprintf(f, "0x%02X", v)
				}
				fmt.Fprintf(f, "}, // frame %d at $%04X\n", frame, sAddr)
			}
			fmt.Fprintln(f, "\t},")
		}
		fmt.Fprintln(f, "}")
		fmt.Fprintln(f, "")
	}
}

func genCreatureSprites() {
	f := createFile("data/gen_creatures.go")
	defer f.Close()

	fmt.Fprintln(f, "package data")
	fmt.Fprintln(f, "")
	fmt.Fprintln(f, "// Auto-generated from aticatac.skool — do not edit.")
	fmt.Fprintln(f, "")

	// Creature type table at $8B7A: 16 bytes mapping kind → graphic base
	fmt.Fprintln(f, "// GenCreatureTypes: 16 creature kind → graphic ID mapping from $8B7A.")
	b := getBytes(0x8B7A, 16)
	fmt.Fprintf(f, "var GenCreatureTypes = [16]byte{")
	for i, v := range b {
		if i > 0 {
			fmt.Fprint(f, ",")
		}
		fmt.Fprintf(f, "0x%02X", v)
	}
	fmt.Fprintln(f, "}")
}

func genItemData() {
	f := createFile("data/gen_items.go")
	defer f.Close()

	fmt.Fprintln(f, "package data")
	fmt.Fprintln(f, "")
	fmt.Fprintln(f, "// Auto-generated from aticatac.skool — do not edit.")
	fmt.Fprintln(f, "")

	// Item init data
	items := []struct {
		name string
		addr uint16
		count int
	}{
		{"GenACGKeyInit", 0x6025, 3},
		{"GenGreenKeyInit", 0x603D, 1},
		{"GenRedKeyInit", 0x6045, 1},
		{"GenCyanKeyInit", 0x604D, 1},
		{"GenYellowKeyInit", 0x6055, 1},
		{"GenLeafInit", 0x605D, 1},
		{"GenFoodInit", 0x60D5, 48},
	}

	for _, item := range items {
		fmt.Fprintf(f, "var %s = [%d][8]byte{\n", item.name, item.count)
		for i := 0; i < item.count; i++ {
			addr := item.addr + uint16(i*8)
			b := getBytes(addr, 8)
			fmt.Fprintf(f, "\t{0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X},\n",
				b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7])
		}
		fmt.Fprintln(f, "}")
		fmt.Fprintln(f, "")
	}

	// Collectibles at $6085-$60CD (8 bytes each, 11 items)
	fmt.Fprintln(f, "var GenCollectibleInit = [11][8]byte{")
	for i := 0; i < 11; i++ {
		addr := uint16(0x605D) + uint16(i*8)
		b := getBytes(addr, 8)
		fmt.Fprintf(f, "\t{0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X},\n",
			b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7])
	}
	fmt.Fprintln(f, "}")
}

func genRoomEntities() {
	f := createFile("data/gen_roomentities.go")
	defer f.Close()

	fmt.Fprintln(f, "package data")
	fmt.Fprintln(f, "")
	fmt.Fprintln(f, "// Auto-generated from aticatac.skool — do not edit.")
	fmt.Fprintln(f, "")
	fmt.Fprintln(f, "// GenRoomEntityData: per-room entity records.")
	fmt.Fprintln(f, "// Each entity is [type, room, flags, x, y, attr, p1, p2] (8 bytes).")
	fmt.Fprintln(f, "// For linked pairs (doors), both sides are emitted — the engine")
	fmt.Fprintln(f, "// must check which side's room matches the current room.")
	fmt.Fprintln(f, "// Format: [16]byte = side_A (8 bytes) + side_B (8 bytes).")
	fmt.Fprintln(f, "var GenRoomEntityData = map[int][][16]byte{")

	roomTableBase := uint16(0x757D)
	for room := 0; room < 150; room++ {
		roomPtr := getWord(roomTableBase + uint16(room*2))
		if roomPtr == 0 {
			continue
		}

		var entities [][16]byte
		cur := roomPtr
		for {
			entityPtr := getWord(cur)
			if entityPtr == 0 {
				break
			}
			cur += 2

			// Read BOTH sides of the linked pair (16 bytes total)
			b := getBytes(entityPtr, 16)
			var e [16]byte
			copy(e[:], b)
			entities = append(entities, e)
		}

		if len(entities) > 0 {
			fmt.Fprintf(f, "\t%d: {\n", room)
			for _, e := range entities {
				fmt.Fprintf(f, "\t\t{0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X, 0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X,0x%02X},\n",
					e[0], e[1], e[2], e[3], e[4], e[5], e[6], e[7],
					e[8], e[9], e[10], e[11], e[12], e[13], e[14], e[15])
			}
			fmt.Fprintln(f, "\t},")
		}
	}
	fmt.Fprintln(f, "}")
}

// ---------- FILE HELPERS ----------

func createFile(path string) *os.File {
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create %s: %v\n", path, err)
		os.Exit(1)
	}
	return f
}

func emitByteSlice(f *os.File, name string, addr uint16, length int) {
	b := getBytes(addr, length)
	fmt.Fprintf(f, "var %s = []byte{", name)
	for i, v := range b {
		if i > 0 {
			fmt.Fprint(f, ",")
		}
		if i%16 == 0 {
			fmt.Fprintf(f, "\n\t")
		}
		fmt.Fprintf(f, "0x%02X", v)
	}
	fmt.Fprintln(f, ",")
	fmt.Fprintln(f, "}")
	fmt.Fprintln(f, "")
}
