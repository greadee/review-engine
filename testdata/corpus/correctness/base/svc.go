package svc

func Total(items []int) int {
	total := 0
	for i := 0; i < len(items); i++ {
		total += items[i]
	}
	return total
}
