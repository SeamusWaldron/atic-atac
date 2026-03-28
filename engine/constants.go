package engine

// Game timing — Atic Atac uses the 50Hz PAL interrupt.
const (
	TargetFPS = 50

	// Initial player state.
	InitialEnergy = 0xF0 // $F0 = 240 from original Z80
	InitialLives  = 3    // original starts with 3 lives
)

// Game states.
type GameState byte

const (
	StateMenu    GameState = 0
	StatePlaying GameState = 1
	StateDead    GameState = 2
	StateGameOver GameState = 3
	StateWin     GameState = 4
)
