// Package admin steuert die (Neu-)Indizierung aus der WebUI heraus.
// Es läuft immer nur EIN Lauf gleichzeitig; der Fortschritt ist per Status
// abrufbar (die WebUI pollt ihn).
package admin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"wiki-rog/internal/config"
	"wiki-rog/internal/ingest"
	"wiki-rog/internal/store"
)

// Target ist ein Indizierungsziel: eine Collection mit passendem BookStack-Token.
type Target struct {
	Group       string
	Collection  string
	TokenID     string
	TokenSecret string
}

// Status beschreibt den aktuellen/letzten Lauf.
type Status struct {
	Running    bool      `json:"running"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Pages      int       `json:"pages"`
	Chunks     int       `json:"chunks"`
	Result     string    `json:"result"`
	Error      string    `json:"error,omitempty"`
	Log        []string  `json:"log"`
}

// Runner führt Indizierungsläufe im Hintergrund aus.
type Runner struct {
	cfg config.Config
	mgr *store.Manager
	mu  sync.Mutex
	st  Status
}

func NewRunner(cfg config.Config, mgr *store.Manager) *Runner {
	return &Runner{cfg: cfg, mgr: mgr}
}

// Status liefert eine Kopie des aktuellen Zustands.
func (r *Runner) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := r.st
	cp.Log = append([]string(nil), r.st.Log...)
	return cp
}

// Start beginnt einen Lauf. Läuft bereits einer, wird ein Fehler zurückgegeben.
func (r *Runner) Start(targets []Target, reset bool) error {
	if len(targets) == 0 {
		return errors.New("keine Indizierungsziele konfiguriert")
	}
	r.mu.Lock()
	if r.st.Running {
		r.mu.Unlock()
		return errors.New("es läuft bereits eine Indizierung")
	}
	r.st = Status{Running: true, StartedAt: time.Now(), Log: []string{}}
	r.mu.Unlock()
	go r.run(targets, reset)
	return nil
}

func (r *Runner) log(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.st.Log = append(r.st.Log, line)
	if len(r.st.Log) > 300 { // nur die letzten Zeilen behalten
		r.st.Log = r.st.Log[len(r.st.Log)-300:]
	}
}

func (r *Runner) run(targets []Target, reset bool) {
	ctx := context.Background()
	var totalPages, totalChunks int
	var firstErr error

	for _, t := range targets {
		label := t.Collection
		if t.Group != "" {
			label = fmt.Sprintf("%s (%s)", t.Group, t.Collection)
		}
		r.log("→ Indexiere " + label)

		cfg := r.cfg
		if t.TokenID != "" {
			cfg.BookStackTokenID = t.TokenID
			cfg.BookStackTokenSecret = t.TokenSecret
		}

		st, err := r.mgr.Get(t.Collection)
		if err != nil {
			firstErr = orFirst(firstErr, err)
			r.log("  Fehler: " + err.Error())
			continue
		}
		if reset {
			if err := st.Reset(ctx); err != nil {
				firstErr = orFirst(firstErr, err)
				r.log("  Fehler beim Zurücksetzen: " + err.Error())
				continue
			}
		}
		stats, err := ingest.Run(ctx, cfg, st, func(m string) { r.log("  " + m) })
		if err != nil {
			firstErr = orFirst(firstErr, err)
			r.log("  Fehler: " + err.Error())
			continue
		}
		totalPages += stats.Pages
		totalChunks += stats.Chunks
		r.log(fmt.Sprintf("  fertig: %d Seite(n), %d Chunk(s)", stats.Pages, stats.Chunks))
	}

	r.mu.Lock()
	r.st.Running = false
	r.st.FinishedAt = time.Now()
	r.st.Pages = totalPages
	r.st.Chunks = totalChunks
	if firstErr != nil {
		r.st.Error = firstErr.Error()
		r.st.Result = fmt.Sprintf("Mit Fehlern beendet – %d Seite(n), %d Chunk(s)", totalPages, totalChunks)
	} else {
		r.st.Result = fmt.Sprintf("Erfolgreich – %d Seite(n), %d Chunk(s)", totalPages, totalChunks)
	}
	r.mu.Unlock()
}

func orFirst(first, err error) error {
	if first != nil {
		return first
	}
	return err
}
