package testdata

import "testing"

func TestMulti(t *testing.T) {
	if Multi(1, 2, 3) != 6 {
		t.Fail()
	}
}
