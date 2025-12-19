package testdata

import "fmt"

func cond(x int) error {
	if x > 0 {
		return fmt.Errorf("this should never happen")
	}
	return nil
}

func uncovered(x int) int {
	if x > 10 {
		return x
	}
	return 0
}

func complexCond(a, b bool) bool {
	if a && b {
		return true
	}
	return false
}

func simpleBool(a bool) bool {
	if a {
		return true
	}
	return false
}
