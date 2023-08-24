package testdata

import "fmt"

func cond(x int) error {
	if x > 0 {
		return fmt.Errorf("this should never happen")
	}
	return nil
}
