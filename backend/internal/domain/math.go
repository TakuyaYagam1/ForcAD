package domain

import "math"

func isInvalidFloat(value float64) bool {
	return math.IsNaN(value) || math.IsInf(value, 0)
}
