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