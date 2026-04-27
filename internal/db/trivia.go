package db

import (
	"encoding/csv"
	"errors"
	"os"
)

type Trivia struct {
	Question string
	Answers  map[string]bool
}

func ReadTrivia(path string) ([]*Trivia, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	csvReader := csv.NewReader(f)
	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, err
	}

	if len(records) < 2 {
		return nil, errors.New("not enough trivia data present")
	}

	trivia := make([]*Trivia, len(records)-1)
	for i, question := range records[1:] {
		answers := map[string]bool{}

		for i, answer := range question[1:] {
			if answer != "" {
				answers[answer] = i == 0
			}
		}

		trivia[i] = &Trivia{
			Question: question[0],
			Answers:  answers,
		}
	}

	return trivia, nil
}
