package voxaudio

// Helper function: Calculate absolute value
func abs(x int16) int16 {
	if x < 0 {
		return -x
	}
	return x
}

func absFloat32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
