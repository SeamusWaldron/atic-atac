package render3d

import "math"

// Vec3 is a 3D vector.
type Vec3 struct{ X, Y, Z float32 }

func (a Vec3) Sub(b Vec3) Vec3  { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }
func (a Vec3) Add(b Vec3) Vec3  { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }
func (a Vec3) Scale(s float32) Vec3 { return Vec3{a.X * s, a.Y * s, a.Z * s} }

func (a Vec3) Cross(b Vec3) Vec3 {
	return Vec3{
		a.Y*b.Z - a.Z*b.Y,
		a.Z*b.X - a.X*b.Z,
		a.X*b.Y - a.Y*b.X,
	}
}

func (a Vec3) Dot(b Vec3) float32 {
	return a.X*b.X + a.Y*b.Y + a.Z*b.Z
}

func (a Vec3) Len() float32 {
	return float32(math.Sqrt(float64(a.X*a.X + a.Y*a.Y + a.Z*a.Z)))
}

func (a Vec3) Normalize() Vec3 {
	l := a.Len()
	if l < 1e-9 {
		return Vec3{}
	}
	return Vec3{a.X / l, a.Y / l, a.Z / l}
}

// Quad is a 3D quadrilateral with flat shading.
type Quad struct {
	Verts     [4]Vec3 // corners (CCW winding viewed from front)
	Color     byte    // ZX Spectrum palette index (fill)
	EdgeColor byte    // palette index (wireframe edges)
}

// Normal returns the face normal of the quad (CCW winding).
func (q *Quad) Normal() Vec3 {
	e1 := q.Verts[1].Sub(q.Verts[0])
	e2 := q.Verts[3].Sub(q.Verts[0])
	return e1.Cross(e2).Normalize()
}
