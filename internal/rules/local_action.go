package rules

func strongerAction(a, b string) string {
	rank := map[string]int{"delete": 1, "mute": 2, "ban": 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}
