package testdata

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fail()
	}
}
