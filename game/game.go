package game

import (
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/seamuswaldron/aticatac/action"
	"github.com/seamuswaldron/aticatac/audio"
	"github.com/seamuswaldron/aticatac/data"
	"github.com/seamuswaldron/aticatac/engine"
	"github.com/seamuswaldron/aticatac/input"
	"github.com/seamuswaldron/aticatac/render3d"
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
	snd    *audio.Player
	img    *ebiten.Image
	pixels []byte
	result engine.StepResult
	room   byte
	menu   MenuState

	// 3D rendering
	renderer3d        *render3d.Renderer
	viewMode3D        bool
	tabWasPressed     bool
	cameraYaw         float32 // independent camera yaw in 3D mode (radians)
	lastLeftPressed   bool    // debounce for turn keys
	lastRightPressed  bool
	lastDownPressed   bool

	// Debounce for special keys
	nWasPressed     bool
	pWasPressed     bool
	starWasPressed  bool
	plusWasPressed  bool
	escWasPressed   bool
	keyJumpPressed  bool
	keyJumpIdx      int
	pausePressed       bool
	paused             bool
	lastPassagePressed bool
	passageJumpIdx     int
	debugMap           DebugMap
	debugMapPressed    bool
	lastKeys           [3]bool
}

// New creates a new Ebitengine game.
func New() *Game {
	g := &Game{
		eng:        engine.New(),
		snd:        audio.NewPlayer(),
		img:        ebiten.NewImage(screenW, screenH),
		pixels:     make([]byte, screenW*screenH*4),
		menu:       NewMenuState(),
		renderer3d: render3d.NewRenderer(),
	}
	g.result = g.eng.Step(0)
	return g
}

// Update is called every tick (target: 50 TPS).
func (g *Game) Update() error {
	// Screenshot: = key (original) or + key (numpad plus / shift+=)
	starPressed := ebiten.IsKeyPressed(ebiten.KeyEqual)
	plusPressed := ebiten.IsKeyPressed(ebiten.KeyKPAdd)
	if (starPressed && !g.starWasPressed) || (plusPressed && !g.plusWasPressed) {
		filename := g.saveScreenshot()
		mode := "2D"
		if g.viewMode3D {
			mode = "3D"
		}
		fmt.Printf("SCREENSHOT [%s]: %s player=(%d,%d) yaw=%.2f room=%d\n",
			mode, filename, g.result.PlayerX, g.result.PlayerY, g.cameraYaw, g.result.Room)
	}
	g.starWasPressed = starPressed
	g.plusWasPressed = plusPressed

	// Menu state
	if g.eng.State() == engine.StateMenu {
		if UpdateMenu(&g.menu, g.eng) {
			g.eng.StartGame()
			g.keyJumpIdx = 0
			g.viewMode3D = g.menu.View3D
			if g.viewMode3D {
				g.cameraYaw = render3d.DirToYaw(data.DirDown)
				g.renderer3d.SetCameraYaw(g.cameraYaw)
			}
		}
		DrawMenu(g.eng.Buffer(), &g.menu)
		g.result = g.eng.Step(0) // get buffer without advancing game
		return nil
	}

	shift := ebiten.IsKeyPressed(ebiten.KeyShift)

	// Tab: toggle 3D view mode
	tabNow := ebiten.IsKeyPressed(ebiten.KeyTab)
	if tabNow && !g.tabWasPressed {
		g.viewMode3D = !g.viewMode3D
		if g.viewMode3D {
			// Initialize camera yaw from current player direction
			g.cameraYaw = render3d.DirToYaw(g.result.PlayerDir)
			g.renderer3d.SetCameraYaw(g.cameraYaw)
			g.renderer3d.SnapCamera(g.result.PlayerX, g.result.PlayerY, g.result.PlayerDir)
			dirNames := [4]string{"LEFT", "RIGHT", "UP", "DOWN"}
			dirName := "?"
			if g.result.PlayerDir >= 0 && g.result.PlayerDir < 4 {
				dirName = dirNames[g.result.PlayerDir]
			}
			fmt.Printf("ENTER 3D: playerDir=%d (%s) → yaw=%.2f at player=(%d,%d)\n",
				g.result.PlayerDir, dirName, g.cameraYaw, g.result.PlayerX, g.result.PlayerY)
		} else {
			fmt.Println("EXIT 3D")
		}
	}
	g.tabWasPressed = tabNow

	// Escape: return to menu
	escNow := ebiten.IsKeyPressed(ebiten.KeyEscape)
	if escNow && !g.escWasPressed {
		g.eng.Reset()
		g.debugMap.Active = false
		g.paused = false
		return nil
	}
	g.escWasPressed = escNow

	// Shift+9: toggle pause
	pauseNow := shift && ebiten.IsKeyPressed(ebiten.KeyDigit9)
	if pauseNow && !g.pausePressed {
		g.paused = !g.paused
	}
	g.pausePressed = pauseNow
	if g.paused {
		return nil
	}

	// Shift+6: toggle debug map overlay
	dmPressed := shift && ebiten.IsKeyPressed(ebiten.KeyDigit6)
	if dmPressed && !g.debugMapPressed && g.menu.DebugMap {
		g.debugMap.Active = !g.debugMap.Active
		if g.debugMap.Active {
			// Init to current room's floor
			g.debugMap.Selected = int(g.eng.Room())
			for i, f := range floors {
				if g.debugMap.Selected >= f.Start && g.debugMap.Selected < f.End {
					g.debugMap.Floor = i
					break
				}
			}
		}
	}
	g.debugMapPressed = dmPressed

	// Debug map overlay: handle input and render
	if g.debugMap.Active {
		warpTo := UpdateDebugMap(&g.debugMap, g.eng)
		if warpTo >= 0 {
			g.room = byte(warpTo)
			g.eng.ChangeRoom(g.room)
			fmt.Printf("Debug map: warped to room %02X\n", warpTo)
		}
		// Draw overlay directly onto the buffer without calling Step
		DrawDebugMap(g.eng.Buffer(), &g.debugMap, g.eng)
		// Update result for the renderer (buffer is already set)
		g.result = engine.StepResult{
			Buffer: g.eng.Buffer(),
			Room:   g.eng.Room(),
			State:  g.eng.State(),
		}
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
	kjPressed := shift && ebiten.IsKeyPressed(ebiten.KeyDigit0) && g.menu.DebugKeys
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
	s3Pressed := shift && ebiten.IsKeyPressed(ebiten.KeyDigit3) && g.menu.DebugKeys
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

	var act action.Action
	if g.viewMode3D {
		act = g.read3DAction()
	} else {
		act = input.ReadAction()
	}
	prevRoom := g.result.Room
	g.result = g.eng.Step(act)

	// Snap 3D camera on room change to avoid lerp across rooms
	if g.viewMode3D && g.result.Room != prevRoom {
		g.renderer3d.SnapCamera(g.result.PlayerX, g.result.PlayerY, g.result.PlayerDir)
		g.cameraYaw = g.renderer3d.CameraYaw()
	}

	// Play queued sound effects
	if events := g.eng.DrainSounds(); len(events) > 0 {
		g.snd.Play(events)
	}

	return nil
}

// Draw renders the current frame.
func (g *Game) Draw(scr *ebiten.Image) {
	// Always render the full 2D frame (includes HUD panel on right side)
	screen.RenderToRGBA(g.result.Buffer, g.pixels)

	// In 3D mode, overwrite only the play area (left 192 columns) with 3D render
	if g.viewMode3D && g.result.State == engine.StatePlaying {
		rs := render3d.RenderState{
			Room:      g.result.Room,
			PlayerX:   g.result.PlayerX,
			PlayerY:   g.result.PlayerY,
			PlayerDir: g.result.PlayerDir,
			Entities:  g.eng.Entities(),
			Frame:     g.eng.Frame(),
		}
		pixels3d := g.renderer3d.Render(rs)
		// Copy only the play area (192 pixels wide) from the 3D buffer
		// into the full frame, leaving the HUD panel (columns 192-255) intact
		for y := 0; y < screenH; y++ {
			srcOff := y * render3d.PlayAreaW * 4
			dstOff := y * screenW * 4
			copy(g.pixels[dstOff:dstOff+render3d.PlayAreaW*4], pixels3d[srcOff:srcOff+render3d.PlayAreaW*4])
		}
	}

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

// read3DAction handles input in 3D mode.
// Left/Right turn the camera. Up/Down move forward/back in the camera's facing direction.
// Shift modifiers: Shift+Left/Right = 90° turn, Shift+Down = 180° turn.
func (g *Game) read3DAction() action.Action {
	var act action.Action
	shift := ebiten.IsKeyPressed(ebiten.KeyShift)

	// Turning (left/right change camera yaw, debounced)
	leftNow := ebiten.IsKeyPressed(ebiten.KeyLeft) || ebiten.IsKeyPressed(ebiten.KeyO)
	rightNow := ebiten.IsKeyPressed(ebiten.KeyRight) || ebiten.IsKeyPressed(ebiten.KeyP)
	downNow := ebiten.IsKeyPressed(ebiten.KeyDown) || ebiten.IsKeyPressed(ebiten.KeyA)

	if leftNow && !g.lastLeftPressed {
		if shift {
			g.cameraYaw += math.Pi / 2 // 90° left
		} else {
			g.cameraYaw += math.Pi / 4 // 45° left
		}
	}
	if rightNow && !g.lastRightPressed {
		if shift {
			g.cameraYaw -= math.Pi / 2 // 90° right
		} else {
			g.cameraYaw -= math.Pi / 4 // 45° right
		}
	}
	if shift && downNow && !g.lastDownPressed {
		g.cameraYaw += math.Pi // 180° turn
	}
	g.lastLeftPressed = leftNow
	g.lastRightPressed = rightNow
	g.lastDownPressed = downNow

	// Normalize yaw to [-π, π]
	for g.cameraYaw > math.Pi {
		g.cameraYaw -= 2 * math.Pi
	}
	for g.cameraYaw < -math.Pi {
		g.cameraYaw += 2 * math.Pi
	}

	// Set the camera yaw directly (bypass direction-based targeting)
	g.renderer3d.SetCameraYaw(g.cameraYaw)

	// Forward/backward movement: translate camera-relative to cardinal direction
	upNow := ebiten.IsKeyPressed(ebiten.KeyUp) || ebiten.IsKeyPressed(ebiten.KeyQ)
	if upNow {
		act |= g.yawToAction(g.cameraYaw)
	}
	if downNow && !shift {
		// Backward = opposite direction
		act |= g.yawToAction(g.cameraYaw + math.Pi)
	}

	// Fire and pickup pass through unchanged
	if ebiten.IsKeyPressed(ebiten.KeySpace) {
		act |= action.Fire
	}
	if ebiten.IsKeyPressed(ebiten.KeyEnter) {
		act |= action.Pickup
	}

	return act
}

// yawToAction converts a camera yaw angle to the nearest cardinal direction action.
func (g *Game) yawToAction(yaw float32) action.Action {
	// Normalize
	for yaw > math.Pi {
		yaw -= 2 * math.Pi
	}
	for yaw < -math.Pi {
		yaw += 2 * math.Pi
	}

	// Yaw 0 = looking +Z = "down" in 2D coordinates
	// π/2 = looking -X = "left"
	// π = looking -Z = "up"
	// -π/2 = looking +X = "right"
	//
	// Map to nearest cardinal direction
	if yaw >= -math.Pi/4 && yaw < math.Pi/4 {
		return action.Down // +Z
	} else if yaw >= math.Pi/4 && yaw < 3*math.Pi/4 {
		return action.Left // -X
	} else if yaw >= -3*math.Pi/4 && yaw < -math.Pi/4 {
		return action.Right // +X
	}
	return action.Up // -Z
}

// saveScreenshot saves the current frame as a PNG with a descriptive filename.
// Returns the filename for logging.
func (g *Game) saveScreenshot() string {
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
		return ""
	}
	defer f.Close()
	png.Encode(f, img)
	return filename
}
