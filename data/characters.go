package data

// CharacterClass identifies the three playable characters.
type CharacterClass byte

const (
	Wizard CharacterClass = 0
	Knight CharacterClass = 1
	Serf   CharacterClass = 2
)

// CharacterDef holds per-class parameters.
type CharacterDef struct {
	Class        CharacterClass
	Name         string
	Deceleration byte // movement deceleration factor
	Acceleration byte // movement acceleration factor
	StartRoom    byte // starting room number
	StartX       byte
	StartY       byte
}

// Characters defines the three playable character classes.
var Characters = [3]CharacterDef{
	{
		Class:        Wizard,
		Name:         "WIZARD",
		Deceleration: 0x20,
		Acceleration: 0x20,
		StartRoom:    0x00,
		StartX:       0x60,
		StartY:       0x60,
	},
	{
		Class:        Knight,
		Name:         "KNIGHT",
		Deceleration: 0x20,
		Acceleration: 0x03,
		StartRoom:    0x00,
		StartX:       0x60,
		StartY:       0x60,
	},
	{
		Class:        Serf,
		Name:         "SERF",
		Deceleration: 0x20,
		Acceleration: 0x01,
		StartRoom:    0x00,
		StartX:       0x60,
		StartY:       0x60,
	},
}
