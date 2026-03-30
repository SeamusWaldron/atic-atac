package game

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/seamuswaldron/aticatac/data"
	"github.com/seamuswaldron/aticatac/engine"
	"github.com/seamuswaldron/aticatac/input"
	"github.com/seamuswaldron/aticatac/screen"
)

const (
	screenW = screen.ScreenWidthPx
	screenH = screen.ScreenHeightPx
	scale   = 3
)

// Game is the Ebitengine wrapper around the headless engine.
type Game struct {
	eng    *engine.GameEnv
	img    *ebiten.Image
	pixels []byte
	result engine.StepResult
	room   byte
	menu   MenuState

	// Debounce for special keys
	nWasPressed     bool
	pWasPressed     bool
	starWasPressed  bool
	keyJumpPressed  bool
	keyJumpIdx      int
	pausePressed       bool
	paused             bool
	lastPassagePressed bool
	passageJumpIdx     int
	lastKeys           [3]bool
}

// New creates a new Ebitengine game.
func New() *Game {
	g := &Game{
		eng:    engine.New(),
		img:    ebiten.NewImage(screenW, screenH),
		pixels: make([]byte, screenW*screenH*4),
	}
	g.result = g.eng.Step(0)
	return g
}

// Update is called every tick (target: 50 TPS).
func (g *Game) Update() error {
	// Screenshot: = key
	starPressed := ebiten.IsKeyPressed(ebiten.KeyEqual)
	if starPressed && !g.starWasPressed {
		g.saveScreenshot()
	}
	g.starWasPressed = starPressed

	// Menu state
	if g.eng.State() == engine.StateMenu {
		if UpdateMenu(&g.menu, g.eng) {
			g.eng.StartGame()
			g.keyJumpIdx = 0
		}
		DrawMenu(g.eng.Buffer(), &g.menu)
		g.result = g.eng.Step(0) // get buffer without advancing game
		return nil
	}

	shift := ebiten.IsKeyPressed(ebiten.KeyShift)

	// Shift+9: toggle pause
	pauseNow := shift && ebiten.IsKeyPressed(ebiten.KeyDigit9)
	if pauseNow && !g.pausePressed {
		g.paused = !g.paused
	}
	g.pausePressed = pauseNow
	if g.paused {
		return nil
	}

	// Room browsing: Shift+2 = next, Shift+1 = previous (debug)
	nPressed := shift && ebiten.IsKeyPressed(ebiten.KeyDigit2)
	if nPressed && !g.nWasPressed {
		g.room++
		if int(g.room) >= data.NumRooms {
			g.room = 0
		}
		g.eng.ChangeRoom(g.room)
	}
	g.nWasPressed = nPressed

	pPressed := shift && ebiten.IsKeyPressed(ebiten.KeyDigit1)
	if pPressed && !g.pWasPressed {
		if g.room == 0 {
			g.room = byte(data.NumRooms - 1)
		} else {
			g.room--
		}
		g.eng.ChangeRoom(g.room)
	}
	g.pWasPressed = pPressed

	// Shift+0: jump to next key room (debug) — starts with yellow key
	kjPressed := shift && ebiten.IsKeyPressed(ebiten.KeyDigit0)
	if kjPressed && !g.keyJumpPressed {
		info := g.eng.KeyInfo()
		// Sort: colour keys first (YELLOW, RED, GREEN, CYAN), then ACG
		colourOrder := []string{"YELLOW", "RED", "GREEN", "CYAN"}
		var sorted []int
		for _, name := range colourOrder {
			for i, s := range info {
				if len(s) >= len(name) && s[:len(name)] == name {
					sorted = append(sorted, i)
				}
			}
		}
		for i := range info {
			found := false
			for _, j := range sorted {
				if i == j {
					found = true
					break
				}
			}
			if !found {
				sorted = append(sorted, i)
			}
		}
		rooms := g.eng.KeyRooms()
		if len(sorted) > 0 {
			if g.keyJumpIdx == 0 {
				fmt.Println("=== All keys ===")
				for _, idx := range sorted {
					fmt.Println(" ", info[idx])
				}
			}
			g.keyJumpIdx = g.keyJumpIdx % len(sorted)
			ri := sorted[g.keyJumpIdx]
			g.room = rooms[ri]
			g.eng.ChangeRoom(g.room)
			fmt.Printf("→ %s\n", info[ri])
			g.keyJumpIdx++
		}
	}
	g.keyJumpPressed = kjPressed

	// Shift+3: debug — cycle through rooms with secret passages for current character
	s3Pressed := shift && ebiten.IsKeyPressed(ebiten.KeyDigit3)
	if s3Pressed && !g.lastPassagePressed {
		passageType := byte(0)
		passageName := ""
		switch g.eng.Character() {
		case data.Knight:
			passageType = 0x10
			passageName = "clock"
		case data.Wizard:
			passageType = 0x17
			passageName = "bookcase"
		case data.Serf:
			passageType = 0x1A
			passageName = "barrel"
		}
		// Build list of passage rooms
		var passageRooms []byte
		for room := 0; room < data.NumRooms; room++ {
			entities := data.GenRoomEntityData[room]
			for _, pair := range entities {
				for side := 0; side < 2; side++ {
					var e [8]byte
					if side == 0 {
						copy(e[:], pair[0:8])
					} else {
						copy(e[:], pair[8:16])
					}
					if int(e[1]) != room || e[0] != passageType {
						continue
					}
					passageRooms = append(passageRooms, byte(room))
				}
			}
		}
		if len(passageRooms) > 0 {
			if g.passageJumpIdx == 0 {
				fmt.Printf("=== %s passages (%d rooms) ===\n", passageName, len(passageRooms))
				for _, r := range passageRooms {
					fmt.Printf("  Room %02X\n", r)
				}
			}
			g.passageJumpIdx = g.passageJumpIdx % len(passageRooms)
			g.room = passageRooms[g.passageJumpIdx]
			g.eng.ChangeRoom(g.room)
			fmt.Printf("→ %s room %02X (%d of %d)\n", passageName, g.room, g.passageJumpIdx+1, len(passageRooms))
			g.passageJumpIdx++
		} else {
			fmt.Println("No passages found!")
		}
	}
	g.lastPassagePressed = s3Pressed

	// Character select: 1=Wizard, 2=Knight, 3=Serf (only without Shift)
	if !shift {
		charKeys := [3]ebiten.Key{ebiten.KeyDigit1, ebiten.KeyDigit2, ebiten.KeyDigit3}
		charClasses := [3]data.CharacterClass{data.Wizard, data.Knight, data.Serf}
		for i, k := range charKeys {
			pressed := ebiten.IsKeyPressed(k)
			if pressed && !g.lastKeys[i] {
				g.eng.SetCharacter(charClasses[i])
			}
			g.lastKeys[i] = pressed
		}
	}

	act := input.ReadAction()
	g.result = g.eng.Step(act)
	return nil
}

// Draw renders the current frame.
func (g *Game) Draw(scr *ebiten.Image) {
	screen.RenderToRGBA(g.result.Buffer, g.pixels)
	g.img.WritePixels(g.pixels)
	op := &ebiten.DrawImageOptions{}
	scr.DrawImage(g.img, op)
}

// Layout returns the game's logical screen size.
func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return screenW, screenH
}

// ScreenSize returns the window dimensions.
func ScreenSize() (int, int) {
	return screenW * scale, screenH * scale
}

// saveScreenshot saves the current frame as a PNG with a descriptive filename.
func (g *Game) saveScreenshot() {
	r := g.result
	state := "playing"
	switch r.State {
	case engine.StateMenu:
		state = "menu"
	case engine.StateDead:
		state = "dead"
	case engine.StateGameOver:
		state = "gameover"
	case engine.StateWin:
		state = "win"
	case engine.StateFalling:
		state = "falling"
	case engine.StateDying:
		state = "dying"
	case engine.StateSpawning:
		state = "spawning"
	}

	charNames := [3]string{"wizard", "knight", "serf"}
	char := "knight"
	if int(g.eng.Character()) < len(charNames) {
		char = charNames[g.eng.Character()]
	}

	ts := time.Now().Format("150405")
	filename := fmt.Sprintf("screenshot_%s_room%02X_%s_%s_e%d_s%d.png",
		state, r.Room, char, ts, r.Energy, r.Score)

	img := image.NewRGBA(image.Rect(0, 0, screenW, screenH))
	copy(img.Pix, g.pixels)

	f, err := os.Create(filename)
	if err != nil {
		fmt.Println("Screenshot failed:", err)
		return
	}
	defer f.Close()
	png.Encode(f, img)
	fmt.Println("Screenshot saved:", filename)
}
