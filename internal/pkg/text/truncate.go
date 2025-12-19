package text

func Truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if max > len(s) {
		max = len(s)
	}
	return s[:max] + "..."
}
