package utils

import "math/rand"

func RandomBetween(min, max int) int {
	if min >= max {
		return min
	}
	return min + RandomInt(0, max-min)
}

func RandomInt(min, max int) int {
	if min >= max {
		return min
	}
	return min + rand.Intn(max-min)
}

func Random(max int) int {
	if max <= 0 {
		return 0
	}
	return rand.Intn(max)
}
