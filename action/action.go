package action

// Action represents a player input action.
type Action uint8

const (
	None   Action = 0
	Up     Action = 1 << 0
	Down   Action = 1 << 1
	Left   Action = 1 << 2
	Right  Action = 1 << 3
	Fire   Action = 1 << 4
	Pickup Action = 1 << 5
	Enter  Action = 1 << 6
	Escape Action = 1 << 7
)
