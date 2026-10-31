package guide

func Clamp(v, limit int) int {
	if v > limit {
		return limit
	}
	return v
}
