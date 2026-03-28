package engine

import "github.com/seamuswaldron/aticatac/screen"

// StepResult is the observation returned after each game step.
type StepResult struct {
	Buffer   *screen.Buffer
	Score    uint32
	Lives    byte
	Energy   byte
	Room     byte
	State    GameState
	GameOver bool
}
