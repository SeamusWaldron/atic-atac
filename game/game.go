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
	nWasPressed    bool
	pWasPressed    bool
	starWasPressed bool
	lastKeys       [3]bool
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
	// Screenshot: * key (Shift+8 on most keyboards)
	starPressed := ebiten.IsKeyPressed(ebiten.KeyKPMultiply) ||
		(ebiten.IsKeyPressed(ebiten.Key8) && (ebiten.IsKeyPressed(ebiten.KeyShift)))
	if starPressed && !g.starWasPressed {
		g.saveScreenshot()
	}
	g.starWasPressed = starPressed

	// Menu state
	if g.eng.State() == engine.StateMenu {
		if UpdateMenu(&g.menu, g.eng) {
			g.eng.StartGame()
		}
		DrawMenu(g.eng.Buffer(), &g.menu)
		g.result = g.eng.Step(0) // get buffer without advancing game
		return nil
	}

	// Room browsing: F2 = next, F1 = previous (debug)
	nPressed := ebiten.IsKeyPressed(ebiten.KeyF2)
	if nPressed && !g.nWasPressed {
		g.room++
		if int(g.room) >= data.NumRooms {
			g.room = 0
		}
		g.eng.ChangeRoom(g.room)
	}
	g.nWasPressed = nPressed

	pPressed := ebiten.IsKeyPressed(ebiten.KeyF1)
	if pPressed && !g.pWasPressed {
		if g.room == 0 {
			g.room = byte(data.NumRooms - 1)
		} else {
			g.room--
		}
		g.eng.ChangeRoom(g.room)
	}
	g.pWasPressed = pPressed

	// Character select: 1=Wizard, 2=Knight, 3=Serf
	charKeys := [3]ebiten.Key{ebiten.Key1, ebiten.Key2, ebiten.Key3}
	charClasses := [3]data.CharacterClass{data.Wizard, data.Knight, data.Serf}
	for i, k := range charKeys {
		pressed := ebiten.IsKeyPressed(k)
		if pressed && !g.lastKeys[i] {
			g.eng.SetCharacter(charClasses[i])
		}
		g.lastKeys[i] = pressed
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
