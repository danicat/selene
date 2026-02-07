package testdata

import (
	"testing"
)

func TestCond(t *testing.T) {
	err := cond(-1)
	if err != nil {
		t.Fatal(err)
	}
}

func TestFake(t *testing.T) {
	_ = cond(100) // this doesn't test anything
}
