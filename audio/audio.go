package audio

import (
	"encoding/binary"
	"math/rand"

	eaudio "github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/seamuswaldron/aticatac/engine"
)

const sampleRate = 44100

// Player handles sound effect playback using Ebitengine's audio system.
type Player struct {
	ctx    *eaudio.Context
	sounds map[engine.SoundEvent][]byte
}

// NewPlayer creates a new audio player and pre-generates all sound effects.
func NewPlayer() *Player {
	p := &Player{
		ctx:    eaudio.NewContext(sampleRate),
		sounds: make(map[engine.SoundEvent][]byte),
	}
	p.generateSounds()
	return p
}

// Play plays all queued sound events.
func (p *Player) Play(events []engine.SoundEvent) {
	for _, ev := range events {
		buf, ok := p.sounds[ev]
		if !ok || len(buf) == 0 {
			continue
		}
		sp := eaudio.NewPlayerFromBytes(p.ctx, buf)
		sp.SetVolume(0.15)
		sp.Play()
	}
}

// generateSounds pre-generates all sound effect PCM buffers.
func (p *Player) generateSounds() {
	// Walk click: short alternating tones. Z80: B=$40/$60, C=$04
	p.sounds[engine.SoundWalkClick] = squareTone(870, 3)

	// Knight axe: descending sweep, 12 steps
	p.sounds[engine.SoundAxeThrow] = sweep(1200, 400, 12, 8)

	// Serf sword: sawtooth-like sweep, 16 steps
	p.sounds[engine.SoundSwordThrow] = sweep(800, 1600, 8, 6)

	// Wizard fireball: V-shaped sweep (descend then ascend)
	desc := sweep(1000, 500, 8, 6)
	asc := sweep(500, 1000, 8, 6)
	p.sounds[engine.SoundFireball] = append(desc, asc...)

	// Weapon bounce: descending "boing"
	p.sounds[engine.SoundWeaponBounce] = sweep(900, 300, 6, 5)

	// Weapon pop / creature killed: short descending burst
	p.sounds[engine.SoundWeaponPop] = sweep(1500, 200, 8, 4)

	// Monster touch: brief beep. Z80: B=$64, C=$10
	p.sounds[engine.SoundMonsterTouch] = squareTone(550, 8)

	// Room entry: ascending two-part tone
	p.sounds[engine.SoundRoomEntry] = append(
		squareTone(400, 15),
		squareTone(600, 15)...)

	// Door noise: white noise burst. Z80: reads 48 ROM bytes
	p.sounds[engine.SoundDoorNoise] = whiteNoise(40)

	// Food eaten: ascending scale. Z80 tone table at $A4A0
	var food []byte
	for _, freq := range []float64{300, 400, 500, 600, 700, 800, 900, 1000} {
		food = append(food, squareTone(freq, 8)...)
	}
	p.sounds[engine.SoundFoodEaten] = food

	// Item pickup: medium beep. Z80: B=$40, C=$40
	p.sounds[engine.SoundItemPickup] = squareTone(870, 20)

	// Item drop: high long beep. Z80: B=$20, C=$80
	p.sounds[engine.SoundItemDrop] = squareTone(1740, 40)

	// Player spawn: ascending beep
	p.sounds[engine.SoundPlayerSpawn] = sweep(200, 800, 10, 10)

	// Player death: descending tone
	p.sounds[engine.SoundPlayerDeath] = sweep(800, 100, 15, 12)

	// Trap fall: descending whoosh (128 frames = ~2.5s, approximated)
	p.sounds[engine.SoundTrapFall] = sweep(1200, 100, 30, 20)
}

// squareTone generates a mono square wave at the given frequency and duration.
// Returns stereo 16-bit signed little-endian PCM (Ebitengine format).
func squareTone(freqHz float64, durationMs float64) []byte {
	numSamples := int(float64(sampleRate) * durationMs / 1000.0)
	samplesPerHalf := int(float64(sampleRate) / freqHz / 2.0)
	if samplesPerHalf < 1 {
		samplesPerHalf = 1
	}

	buf := make([]byte, numSamples*4) // stereo 16-bit = 4 bytes per sample
	for i := 0; i < numSamples; i++ {
		var val int16
		if (i/samplesPerHalf)%2 == 0 {
			val = 8000 // positive half
		} else {
			val = -8000 // negative half
		}
		off := i * 4
		binary.LittleEndian.PutUint16(buf[off:], uint16(val))   // L
		binary.LittleEndian.PutUint16(buf[off+2:], uint16(val)) // R
	}
	return buf
}

// sweep generates a frequency sweep from startHz to endHz over numSteps.
// Each step lasts stepMs milliseconds.
func sweep(startHz, endHz float64, numSteps int, stepMs float64) []byte {
	var buf []byte
	for i := 0; i < numSteps; i++ {
		t := float64(i) / float64(numSteps-1)
		freq := startHz + (endHz-startHz)*t
		buf = append(buf, squareTone(freq, stepMs)...)
	}
	return buf
}

// whiteNoise generates a white noise burst. Z80: reads ROM bytes as noise.
func whiteNoise(durationMs float64) []byte {
	numSamples := int(float64(sampleRate) * durationMs / 1000.0)
	buf := make([]byte, numSamples*4)
	for i := 0; i < numSamples; i++ {
		val := int16(rand.Intn(16000) - 8000)
		off := i * 4
		binary.LittleEndian.PutUint16(buf[off:], uint16(val))
		binary.LittleEndian.PutUint16(buf[off+2:], uint16(val))
	}
	return buf
}
