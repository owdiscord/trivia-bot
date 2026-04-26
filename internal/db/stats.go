package db

import (
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"time"
)

// RoundStat records everything about a single trivia round.
type RoundStat struct {
	RoundID   string         `json:"round_id"`
	Channel   string         `json:"channel"`
	Question  string         `json:"question"`
	Options   []string       `json:"options"`
	Correct   string         `json:"correct"`
	Votes     map[string]int `json:"votes"` // userID -> option index
	StartedAt time.Time      `json:"started_at"`
	ClosedAt  time.Time      `json:"closed_at"`
}

type StatStore struct {
	path          string
	mu            sync.RWMutex
	data          []*RoundStat
	dirty         bool
	flushInterval time.Duration
	stopCh        chan struct{}
	doneCh        chan struct{}
}

func NewStatStore(path string) (*StatStore, error) {
	s := &StatStore{
		path:          path,
		data:          []*RoundStat{},
		flushInterval: 500 * time.Millisecond,
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
	}

	b, err := os.ReadFile(path)
	if err == nil && len(b) > 0 {
		if err := json.Unmarshal(b, &s.data); err != nil {
			return nil, err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	go s.flushLoop()
	return s, nil
}

func (s *StatStore) Close() {
	close(s.stopCh)
	<-s.doneCh
}

// RecordRoundOpened is called when a round starts, before any votes come in.
// Returns the stat so closeRound can mutate it later via RecordRoundClosed.
func (s *StatStore) RecordRoundOpened(roundID, channel, question string, options []string, correct string, startedAt time.Time) *RoundStat {
	stat := &RoundStat{
		RoundID:   roundID,
		Channel:   channel,
		Question:  question,
		Options:   options,
		Correct:   correct,
		Votes:     map[string]int{},
		StartedAt: startedAt,
	}

	s.mu.Lock()
	s.data = append(s.data, stat)
	s.dirty = true
	s.mu.Unlock()

	return stat
}

// RecordVote adds a user's vote to an open round stat.
func (s *StatStore) RecordVote(stat *RoundStat, userID string, optionIdx int) {
	s.mu.Lock()
	stat.Votes[userID] = optionIdx
	s.dirty = true
	s.mu.Unlock()
}

// RecordRoundClosed stamps the close time on the round.
func (s *StatStore) RecordRoundClosed(stat *RoundStat) {
	s.mu.Lock()
	stat.ClosedAt = time.Now()
	s.dirty = true
	s.mu.Unlock()
}

func (s *StatStore) SaveNow() {
	s.flush()
}

func (s *StatStore) flushLoop() {
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()
	defer close(s.doneCh)
	for {
		select {
		case <-ticker.C:
			s.flush()
		case <-s.stopCh:
			s.flush()
			return
		}
	}
}

func (s *StatStore) flush() {
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return
	}
	// snapshot the slice of pointers — safe because we never mutate
	// the slice itself during flush, only append to it
	snapshot := make([]*RoundStat, len(s.data))
	copy(snapshot, s.data)
	s.dirty = false
	s.mu.Unlock()

	b, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		slog.Error("could not serialise stat data", "err", err)
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		slog.Error("could not write stat tmp file", "err", err)
		return
	}
	_ = os.Rename(tmp, s.path)
}
