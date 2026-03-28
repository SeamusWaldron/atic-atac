package data

// This file wires the auto-generated data (gen_*.go) into the game's
// existing data interfaces. After running `go run ./tools/extract`,
// all game data comes from the exact Z80 disassembly bytes.

func init() {
	// Replace hand-written panel data with extracted data
	PanelChars = GenPanelChars
	PanelGrid = GenPanelGrid
	ChickenFull = GenChickenFull
	ChickenEmpty = GenChickenEmpty

	// Replace hand-written player sprites
	KnightSprites = GenKnightSprites
	WizardSprites = GenWizardSprites
	SerfSprites = GenSerfSprites

	// Replace room attributes
	for i := 0; i < NumRooms && i < len(GenRoomAttrs); i++ {
		RoomAttrs[i] = RoomAttr{
			Colour: GenRoomAttrs[i][0],
			Style:  GenRoomAttrs[i][1],
		}
	}
}
