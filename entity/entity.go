package entity

// Entity represents a game entity (creature, item, projectile).
type Entity struct {
	Active  bool
	Type    EntityType
	Room    byte
	X       int // pixel position
	Y       int // pixel position
	VX      int // velocity (signed, pixels per frame)
	VY      int // velocity (signed, pixels per frame)
	Graphic byte
	Attr    byte // ZX Spectrum colour attribute
	Frame   byte // animation frame counter
	Timer   byte // general-purpose timer (life, spawn delay, etc.)
}

// EntityType identifies what kind of entity this is.
type EntityType byte

const (
	TypeNone       EntityType = 0
	TypeCreature   EntityType = 1
	TypeWeapon     EntityType = 2
	TypeItem       EntityType = 3
	TypeExplosion  EntityType = 4
	TypeFood       EntityType = 5
	TypeKey        EntityType = 6
	TypeCollectible EntityType = 7
)

// CreatureKind identifies the creature subtype (graphic set).
type CreatureKind byte

const (
	KindSpider   CreatureKind = 0
	KindSpikey   CreatureKind = 1
	KindBat      CreatureKind = 2
	KindWitch    CreatureKind = 4
	KindMonk     CreatureKind = 6
	KindBlob     CreatureKind = 10
	KindGhoul    CreatureKind = 11
	KindPumpkin  CreatureKind = 12
	KindGhostlet CreatureKind = 13
	KindGhost    CreatureKind = 14
	KindBatlet   CreatureKind = 15
)

// CreatureGraphics maps creature kind index to base graphic ID.
// Extracted from $8B7A in the Z80 source.
var CreatureGraphics = [16]byte{
	0x5C, 0x5E, 0x98, 0x98, 0x90, 0x90, 0x94, 0x94,
	0x5C, 0x5E, 0x60, 0x62, 0x4C, 0x4E, 0x68, 0x6A,
}

// MaxCreaturesPerRoom is the spawn cap.
const MaxCreaturesPerRoom = 3

// MaxEntities is the total entity pool size.
const MaxEntities = 64

// Pool holds all active entities.
type Pool struct {
	Entities [MaxEntities]Entity
}

// NewPool creates an empty entity pool.
func NewPool() *Pool {
	return &Pool{}
}

// Clear deactivates all entities.
func (p *Pool) Clear() {
	for i := range p.Entities {
		p.Entities[i].Active = false
	}
}

// Spawn finds a free slot and returns a pointer to it, or nil if full.
func (p *Pool) Spawn() *Entity {
	for i := range p.Entities {
		if !p.Entities[i].Active {
			p.Entities[i] = Entity{Active: true}
			return &p.Entities[i]
		}
	}
	return nil
}

// CountInRoom returns the number of active entities of a given type in a room.
func (p *Pool) CountInRoom(room byte, t EntityType) int {
	n := 0
	for i := range p.Entities {
		e := &p.Entities[i]
		if e.Active && e.Room == room && e.Type == t {
			n++
		}
	}
	return n
}

// ForEachInRoom calls fn for every active entity in the given room.
func (p *Pool) ForEachInRoom(room byte, fn func(e *Entity)) {
	for i := range p.Entities {
		e := &p.Entities[i]
		if e.Active && e.Room == room {
			fn(e)
		}
	}
}
