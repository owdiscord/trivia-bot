package db

import (
	"testing"
)

func TestOpenPointStore(t *testing.T) {
	_, err := NewPointStore("../../test_points.json")
	if err != nil {
		t.Fatal(err)
	}
}
