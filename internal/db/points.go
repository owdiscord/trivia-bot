package db

import (
	"cmp"
	"encoding/json"
	"log/slog"
	"maps"
	"os"
	"slices"
	"sync"
	"time"
)

type PointStore struct {
	path string

	mu    sync.RWMutex
	data  map[string]int
	dirty bool

	flushInterval time.Duration
	stopCh        chan struct{}
	doneCh        chan struct{}
}

// NewPointStore will load the database file, or create it if it doesn't exist.
func NewPointStore(path string) (*PointStore, error) {
	s := &PointStore{
		path:          path,
		data:          map[string]int{},
		flushInterval: 500 * time.Millisecond,
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
	}

	// load existing file
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

// Close the channel when we're done with it
func (s *PointStore) Close() {
	close(s.stopCh)
	<-s.doneCh
}

type PointEntry struct {
	UserID string
	Points int
}

func (s *PointStore) TopN(count int) []PointEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := make([]PointEntry, 0, len(s.data))
	for userID, points := range s.data {
		entries = append(entries, PointEntry{UserID: userID, Points: points})
	}

	slices.SortFunc(entries, func(a, b PointEntry) int {
		return cmp.Compare(b.Points, a.Points) // descending
	})

	if count > len(entries) {
		count = len(entries)
	}
	return entries[:count]
}

// Get returns current points for a user
func (s *PointStore) Get(userID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.data[userID]
}

// Set explicitly sets a user's points
func (s *PointStore) Set(userID string, points int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[userID] = points
	return nil
}

// Add increments points
func (s *PointStore) Add(userID string, delta int) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[userID] += delta
	s.dirty = true

	return s.data[userID]
}

// SaveNow will force a save to the file with whatever data is in the in-memory cache
func (s *PointStore) SaveNow() {
	s.flush()
}

// Run the flush loop to flush newly input data to the file on occassion
func (s *PointStore) flushLoop() {
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()
	defer close(s.doneCh)

	for {
		select {
		case <-ticker.C:
			s.flush()
		case <-s.stopCh:
			s.flush() // final flush
			return
		}
	}
}

// Flush cached in-memory data to the database file
func (s *PointStore) flush() {
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return
	}

	// copy data to avoid holding lock during IO
	dataCopy := make(map[string]int, len(s.data))
	maps.Copy(dataCopy, s.data)
	s.dirty = false
	s.mu.Unlock()

	b, err := json.MarshalIndent(dataCopy, "", "  ")
	if err != nil {
		slog.Error("could not serialise the JSON data correctly", "json_err", err)
		return
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		slog.Error("could not write tmp file", "io_err", err)
		return
	}

	_ = os.Rename(tmp, s.path)
}
