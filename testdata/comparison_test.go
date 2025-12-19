package testdata

import "testing"

func TestLess(t *testing.T) {
	if !Less(1, 2) {
		t.Fail()
	}
}

func TestGreaterEqual(t *testing.T) {
	if !GreaterEqual(2, 2) {
		t.Fail()
	}
}
