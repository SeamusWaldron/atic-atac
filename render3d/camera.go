package render3d

import "math"

// Camera is a first-person camera.
type Camera struct {
	X, Y, Z         float32 // current smoothed position in world space
	TargetX, TargetZ float32 // target position (from player)
	Yaw              float32 // rotation around Y axis (radians, 0 = looking +Z)
	TargetYaw        float32 // target yaw for smooth interpolation
	FOV              float32 // horizontal field of view (radians)
	Near             float32
	Far              float32
	// Derived (computed in Update)
	sinYaw, cosYaw float32
	halfW          float32 // half-width of near plane
}

// NewCamera creates a camera with sensible defaults.
func NewCamera() Camera {
	return Camera{
		FOV:  2.0, // ~115 degrees — wide enough to see the room
		Near: 0.05,
		Far:  50.0,
	}
}

// DirToYaw converts a game direction constant to a yaw angle.
// DirLeft=0, DirRight=1, DirUp=2, DirDown=3
func DirToYaw(dir int) float32 {
	switch dir {
	case 0: // Left
		return math.Pi / 2 // looking -X
	case 1: // Right
		return -math.Pi / 2 // looking +X
	case 2: // Up
		return math.Pi // looking -Z
	case 3: // Down
		return 0 // looking +Z
	default:
		return 0
	}
}

// Update recomputes derived values and interpolates position and yaw.
func (c *Camera) Update() {
	// Smooth position interpolation
	const posLerp = 0.3
	c.X += (c.TargetX - c.X) * posLerp
	c.Z += (c.TargetZ - c.Z) * posLerp

	// Smooth yaw interpolation — lerp toward target
	diff := c.TargetYaw - c.Yaw
	// Normalize to [-pi, pi]
	for diff > math.Pi {
		diff -= 2 * math.Pi
	}
	for diff < -math.Pi {
		diff += 2 * math.Pi
	}
	c.Yaw += diff * 0.25 // 25% per frame — smooth over ~4 frames

	c.sinYaw = float32(math.Sin(float64(c.Yaw)))
	c.cosYaw = float32(math.Cos(float64(c.Yaw)))
	c.halfW = float32(math.Tan(float64(c.FOV) / 2))
}

// WorldToCamera transforms a world-space point into camera space.
// Camera space: X = right, Y = up, Z = forward (into screen).
func (c *Camera) WorldToCamera(p Vec3) Vec3 {
	// Translate relative to camera
	dx := p.X - c.X
	dy := p.Y - c.Y
	dz := p.Z - c.Z
	// Rotate by -yaw around Y axis
	return Vec3{
		X: dx*c.cosYaw + dz*c.sinYaw,
		Y: dy,
		Z: -dx*c.sinYaw + dz*c.cosYaw,
	}
}

// Project transforms a camera-space point to screen coordinates.
// Returns screen X, screen Y, depth, and whether the point is in front of the camera.
// Screen coordinates: (0,0) = top-left, (screenW-1, screenH-1) = bottom-right.
func (c *Camera) Project(cs Vec3, screenW, screenH int) (sx, sy int, depth float32, visible bool) {
	if cs.Z <= c.Near {
		return 0, 0, 0, false
	}

	// Perspective divide
	aspect := float32(screenH) / float32(screenW)
	ndcX := cs.X / (cs.Z * c.halfW)
	ndcY := cs.Y / (cs.Z * c.halfW * aspect)

	// NDC to screen: NDC (-1,1) → screen (0, screenW-1)
	// Y is flipped: positive camera Y (up) → lower screen Y (top of screen)
	sx = int((ndcX*0.5 + 0.5) * float32(screenW))
	sy = int((ndcY*0.5 + 0.5) * float32(screenH))

	return sx, sy, cs.Z, true
}
