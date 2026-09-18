// Package store kapselt den Vektorspeicher. Es gibt zwei Backends:
//   - local:  eingebettet, pure Go (File + In-Memory-Cosine-Suche)
//   - qdrant: externer Qdrant-Dienst über HTTP
package store

import (
	"context"
	"fmt"
	"math"
	"sync"

	"wiki-rog/internal/config"
)

// Meta beschreibt die Herkunft eines Chunks.
type Meta struct {
	PageID      int    `json:"page_id"`
	PageName    string `json:"page_name"`
	BookName    string `json:"book_name"`
	ChapterName string `json:"chapter_name"`
	URL         string `json:"url"`
	ChunkIndex  int    `json:"chunk_index"`
	UpdatedAt   string `json:"updated_at"`
}

// Item ist ein zu speichernder Chunk inkl. Embedding.
type Item struct {
	ID        string    `json:"id"`
	Embedding []float32 `json:"embedding"`
	Document  string    `json:"document"`
	Meta      Meta      `json:"meta"`
}

// Result ist ein Suchtreffer (Distance: Cosine-Distanz 0..2, kleiner = ähnlicher).
type Result struct {
	Document string
	Meta     Meta
	Distance float64
}

// Store ist das gemeinsame Interface beider Backends.
type Store interface {
	Upsert(ctx context.Context, items []Item) error
	DeletePage(ctx context.Context, pageID int) error
	Reset(ctx context.Context) error
	Count(ctx context.Context) (int, error)
	Query(ctx context.Context, embedding []float32, topK int) ([]Result, error)
	Close() error
}

// Open erstellt das in der Config gewählte Backend für die Standard-Collection.
func Open(cfg config.Config) (Store, error) {
	return OpenCollection(cfg, cfg.VectorCollection)
}

// OpenCollection erstellt das Backend für eine bestimmte Collection.
func OpenCollection(cfg config.Config, collection string) (Store, error) {
	switch cfg.VectorBackend {
	case "", "local":
		return openLocal(cfg.StoreDir, collection)
	case "qdrant":
		return openQdrant(cfg.QdrantURL, cfg.QdrantAPIKey, collection), nil
	default:
		return nil, fmt.Errorf("unbekanntes VECTOR_BACKEND %q (erlaubt: local, qdrant)", cfg.VectorBackend)
	}
}

// Manager öffnet und cached Stores je Collection (für gruppenbasierten Zugriff).
type Manager struct {
	cfg   config.Config
	mu    sync.Mutex
	cache map[string]Store
}

// NewManager erstellt einen Collection-Manager.
func NewManager(cfg config.Config) *Manager {
	return &Manager{cfg: cfg, cache: map[string]Store{}}
}

// Get liefert (und cached) den Store einer Collection.
func (m *Manager) Get(collection string) (Store, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.cache[collection]; ok {
		return st, nil
	}
	st, err := OpenCollection(m.cfg, collection)
	if err != nil {
		return nil, err
	}
	m.cache[collection] = st
	return st, nil
}

// Close schließt alle geöffneten Stores.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, st := range m.cache {
		_ = st.Close()
	}
	m.cache = map[string]Store{}
}

// cosineDistance = 1 - Cosine-Ähnlichkeit (0 = identisch, 1 = orthogonal).
func cosineDistance(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 2
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 2
	}
	return 1 - dot/(math.Sqrt(na)*math.Sqrt(nb))
}
