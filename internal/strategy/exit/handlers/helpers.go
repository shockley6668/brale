package handlers

func normalizePlanVersion(v int) int {
	if v <= 0 {
		return 1
	}
	return v
}
