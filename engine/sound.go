package engine

// SoundEvent identifies a game sound effect to be played.
type SoundEvent byte

const (
	SoundNone         SoundEvent = 0
	SoundWalkClick    SoundEvent = 1
	SoundAxeThrow     SoundEvent = 2  // Knight weapon
	SoundSwordThrow   SoundEvent = 3  // Serf weapon
	SoundFireball     SoundEvent = 4  // Wizard weapon
	SoundWeaponBounce SoundEvent = 5  // weapon hits wall
	SoundWeaponPop    SoundEvent = 6  // creature killed
	SoundMonsterTouch SoundEvent = 7  // creature damages player
	SoundRoomEntry    SoundEvent = 8  // room transition
	SoundDoorNoise    SoundEvent = 9  // door open/close
	SoundFoodEaten    SoundEvent = 10 // food auto-consumed
	SoundItemPickup   SoundEvent = 11 // key/collectible picked up
	SoundItemDrop     SoundEvent = 12 // item dropped from inventory
	SoundPlayerSpawn  SoundEvent = 13 // materialise animation
	SoundPlayerDeath  SoundEvent = 14 // death sequence
	SoundTrapFall     SoundEvent = 15 // falling through trap door
)

// emitSound queues a sound event for the game wrapper to play.
func (g *GameEnv) emitSound(s SoundEvent) {
	g.sounds = append(g.sounds, s)
}

// DrainSounds returns all queued sound events and clears the queue.
// Called by the game wrapper after each Step().
func (g *GameEnv) DrainSounds() []SoundEvent {
	if len(g.sounds) == 0 {
		return nil
	}
	out := g.sounds
	g.sounds = g.sounds[:0]
	return out
}
