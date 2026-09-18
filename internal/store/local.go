package store

import (
	"context"
	"encoding/gob"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// localStore ist ein file-basierter Speicher mit In-Memory-Cosine-Suche.
// Für Wiki-Größen (tausende Chunks) ist die Brute-Force-Suche völlig ausreichend.
type localStore struct {
	mu    sync.RWMutex
	path  string
	items map[string]Item // key = Item.ID
}

func openLocal(dir, collection string) (*localStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &localStore{
		path:  filepath.Join(dir, collection+".gob"),
		items: map[string]Item{},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *localStore) load() error {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	var items []Item
	if err := gob.NewDecoder(f).Decode(&items); err != nil {
		return err
	}
	for _, it := range items {
		s.items[it.ID] = it
	}
	return nil
}

// persist schreibt atomar (temp + rename).
func (s *localStore) persist() error {
	items := make([]Item, 0, len(s.items))
	for _, it := range s.items {
		items = append(items, it)
	}
	tmp := s.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(f).Encode(items); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *localStore) Upsert(_ context.Context, items []Item) error {
	if len(items) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range items {
		s.items[it.ID] = it
	}
	return s.persist()
}

func (s *localStore) DeletePage(_ context.Context, pageID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for id, it := range s.items {
		if it.Meta.PageID == pageID {
			delete(s.items, id)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.persist()
}

func (s *localStore) Reset(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = map[string]Item{}
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *localStore) Count(_ context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items), nil
}

func (s *localStore) Query(_ context.Context, embedding []float32, topK int) ([]Result, error) {
	s.mu.RLock()
	results := make([]Result, 0, len(s.items))
	for _, it := range s.items {
		results = append(results, Result{
			Document: it.Document,
			Meta:     it.Meta,
			Distance: cosineDistance(embedding, it.Embedding),
		})
	}
	s.mu.RUnlock()

	sort.Slice(results, func(i, j int) bool {
		return results[i].Distance < results[j].Distance
	})
	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

func (s *localStore) Close() error { return nil }
