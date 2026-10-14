package svc

func Total(items []int) int {
	total := 0
	for i := 0; i <= len(items); i++ { // revu-seed: Bug P1 off-by-one-total
		total += items[i]
	}
	return total
}
