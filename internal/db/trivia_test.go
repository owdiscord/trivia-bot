package db

import (
	"reflect"
	"testing"
)

func TestReadTrivia(t *testing.T) {
	trivia, err := ReadTrivia("../../test.csv")
	if err != nil {
		t.Fatal(err)
	}

	if len(trivia) != 2 {
		t.Fatalf("testing trivia data length was incorrect (expected 2, got %d)\n", len(trivia))
	}

	expected := []*Trivia{
		{
			Question: "How many maps did Overwatch have on release?",
			Answers: map[string]bool{
				"11": false,
				"12": true,
				"16": false,
				"18": false,
				"22": false,
				"8":  false,
			},
		},
		{
			Question: "How many Overwatch heros were playable on release?",
			Answers: map[string]bool{
				"12": false,
				"13": false,
				"16": false,
				"21": true,
				"22": false,
				"8":  false,
			},
		},
	}

	if !reflect.DeepEqual(trivia, expected) {
		t.Fatalf("trivia mismatch, expected\n%#v\ngot%#v", expected, trivia)
	}
}
