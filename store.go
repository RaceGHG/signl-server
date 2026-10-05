package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Zustände eines Downloads.
const (
	stateQueued      = "queued"
	stateDownloading = "downloading"
	stateCompleted   = "completed"
	stateFailed      = "failed"
	stateCancelled   = "cancelled"
)

// Download ist ein Eintrag der Liste. Die Quelladresse enthält oft
// Zugangsdaten des Anbieters und wird deshalb nie nach außen gegeben.
type Download struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Subtitle   string     `json:"subtitle,omitempty"`
	Poster     string     `json:"poster,omitempty"`
	Kind       string     `json:"kind"`
	Folder     string     `json:"folder,omitempty"`
	Duration   float64    `json:"duration,omitempty"`
	State      string     `json:"state"`
	Progress   float64    `json:"progress"`
	Bytes      int64      `json:"bytes"`
	Total      int64      `json:"total"`
	File       string     `json:"file,omitempty"` // relativ zu media/
	Error      string     `json:"error,omitempty"`
	AddedAt    time.Time  `json:"addedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`

	SourceURL string `json:"sourceUrl,omitempty"` // nur in state.json
}

// public liefert die Außenansicht ohne Quelladresse.
func (d Download) public() Download {
	d.SourceURL = ""
	return d
}

type store struct {
	mu    sync.Mutex
	path  string
	items map[string]*Download
}

func newStore(dir string) (*store, error) {
	s := &store{path: filepath.Join(dir, "state.json"), items: map[string]*Download{}}
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var list []*Download
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, err
	}
	for _, d := range list {
		s.items[d.ID] = d
	}
	return s, nil
}

func (s *store) saveLocked() {
	list := make([]*Download, 0, len(s.items))
	for _, d := range s.items {
		list = append(list, d)
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, s.path)
	}
}

func (s *store) get(id string) (Download, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.items[id]
	if !ok {
		return Download{}, false
	}
	return *d, true
}

func (s *store) put(d Download) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := d
	s.items[d.ID] = &cp
	s.saveLocked()
}

// update ändert einen Eintrag; persist=false für häufige Fortschrittsmeldungen.
func (s *store) update(id string, persist bool, f func(*Download)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.items[id]
	if !ok {
		return
	}
	f(d)
	if persist {
		s.saveLocked()
	}
}

func (s *store) remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, id)
	s.saveLocked()
}

// list: neueste zuerst.
func (s *store) list() []Download {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Download, 0, len(s.items))
	for _, d := range s.items {
		out = append(out, d.public())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AddedAt.After(out[j].AddedAt) })
	return out
}

func (s *store) snapshot() []Download {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Download, 0, len(s.items))
	for _, d := range s.items {
		out = append(out, *d)
	}
	return out
}
