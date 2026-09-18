// Package ingest baut den Vektorindex aus den BookStack-Inhalten auf.
package ingest

import (
	"context"
	"fmt"
	"strings"

	"wiki-rog/internal/bookstack"
	"wiki-rog/internal/chunk"
	"wiki-rog/internal/config"
	"wiki-rog/internal/ollama"
	"wiki-rog/internal/store"
)

type Stats struct {
	Pages          int
	Chunks         int
	CollectionSize int
}

func pageHeader(p bookstack.Page) string {
	var parts []string
	for _, s := range []string{p.BookName, p.ChapterName, p.Name} {
		if strings.TrimSpace(s) != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " > ")
}

// Run liest BookStack aus, zerlegt/embedded die Seiten und schreibt sie in den Store.
// reset=true leert den Store zuvor komplett.
func Run(ctx context.Context, cfg config.Config, st store.Store, progress func(string)) (Stats, error) {
	if err := cfg.RequireBookStack(); err != nil {
		return Stats{}, err
	}
	oc := ollama.New(cfg.OllamaURL)
	if err := oc.Check(ctx); err != nil {
		return Stats{}, err
	}
	bs := bookstack.New(cfg.BookStackURL, cfg.BookStackTokenID, cfg.BookStackTokenSecret, cfg.BookStackVerifySSL)

	var stats Stats
	err := bs.IterPages(cfg.BookIDs, cfg.ShelfIDs, func(p bookstack.Page) error {
		// Alte Chunks dieser Seite entfernen (inkrementelles Update).
		if err := st.DeletePage(ctx, p.ID); err != nil {
			return err
		}
		header := pageHeader(p)
		chunks := chunk.Text(p.Content, cfg.ChunkSize, cfg.ChunkOverlap)
		if len(chunks) == 0 {
			return nil
		}
		items := make([]store.Item, 0, len(chunks))
		for idx, c := range chunks {
			embedInput := c
			if header != "" {
				embedInput = header + "\n\n" + c
			}
			emb, err := oc.Embed(ctx, cfg.OllamaEmbedModel, embedInput)
			if err != nil {
				return err
			}
			items = append(items, store.Item{
				ID:        fmt.Sprintf("page-%d-chunk-%d", p.ID, idx),
				Embedding: emb,
				Document:  c,
				Meta: store.Meta{
					PageID:      p.ID,
					PageName:    p.Name,
					BookName:    p.BookName,
					ChapterName: p.ChapterName,
					URL:         p.URL,
					ChunkIndex:  idx,
					UpdatedAt:   p.UpdatedAt,
				},
			})
		}
		if err := st.Upsert(ctx, items); err != nil {
			return err
		}
		stats.Pages++
		stats.Chunks += len(chunks)
		if progress != nil {
			progress(fmt.Sprintf("indexiert: %s (%d Chunks)", p.Name, len(chunks)))
		}
		return nil
	})
	if err != nil {
		return stats, err
	}
	size, err := st.Count(ctx)
	if err != nil {
		return stats, err
	}
	stats.CollectionSize = size
	return stats, nil
}
