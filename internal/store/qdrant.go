package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"time"
)

// qdrantStore spricht einen externen Qdrant-Dienst über die HTTP-API an.
type qdrantStore struct {
	baseURL    string
	apiKey     string
	collection string
	http       *http.Client
	ensured    bool
}

func openQdrant(baseURL, apiKey, collection string) *qdrantStore {
	return &qdrantStore{
		baseURL:    baseURL,
		apiKey:     apiKey,
		collection: collection,
		http:       &http.Client{Timeout: 60 * time.Second},
	}
}

func (q *qdrantStore) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, q.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if q.apiKey != "" {
		req.Header.Set("api-key", q.apiKey)
	}
	resp, err := q.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Qdrant %s %s -> HTTP %d: %s", method, path, resp.StatusCode, string(data))
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (q *qdrantStore) ensureCollection(ctx context.Context, dim int) error {
	if q.ensured {
		return nil
	}
	// Existiert die Collection schon?
	err := q.do(ctx, http.MethodGet, "/collections/"+q.collection, nil, nil)
	if err == nil {
		q.ensured = true
		return nil
	}
	// Andernfalls anlegen (Cosine-Distanz).
	create := map[string]any{
		"vectors": map[string]any{"size": dim, "distance": "Cosine"},
	}
	if err := q.do(ctx, http.MethodPut, "/collections/"+q.collection, create, nil); err != nil {
		return err
	}
	q.ensured = true
	return nil
}

func pointID(id string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return h.Sum64()
}

func (q *qdrantStore) Upsert(ctx context.Context, items []Item) error {
	if len(items) == 0 {
		return nil
	}
	if err := q.ensureCollection(ctx, len(items[0].Embedding)); err != nil {
		return err
	}
	points := make([]map[string]any, 0, len(items))
	for _, it := range items {
		points = append(points, map[string]any{
			"id":     pointID(it.ID),
			"vector": it.Embedding,
			"payload": map[string]any{
				"orig_id":      it.ID,
				"document":     it.Document,
				"page_id":      it.Meta.PageID,
				"page_name":    it.Meta.PageName,
				"book_name":    it.Meta.BookName,
				"chapter_name": it.Meta.ChapterName,
				"url":          it.Meta.URL,
				"chunk_index":  it.Meta.ChunkIndex,
				"updated_at":   it.Meta.UpdatedAt,
			},
		})
	}
	body := map[string]any{"points": points}
	return q.do(ctx, http.MethodPut, "/collections/"+q.collection+"/points?wait=true", body, nil)
}

func (q *qdrantStore) DeletePage(ctx context.Context, pageID int) error {
	body := map[string]any{
		"filter": map[string]any{
			"must": []map[string]any{
				{"key": "page_id", "match": map[string]any{"value": pageID}},
			},
		},
	}
	err := q.do(ctx, http.MethodPost, "/collections/"+q.collection+"/points/delete?wait=true", body, nil)
	// Fehlende Collection ist beim ersten Lauf ok.
	if err != nil && !q.ensured {
		return nil
	}
	return err
}

func (q *qdrantStore) Reset(ctx context.Context) error {
	q.ensured = false
	err := q.do(ctx, http.MethodDelete, "/collections/"+q.collection, nil, nil)
	if err != nil {
		return nil // Collection existierte evtl. nicht – ok
	}
	return nil
}

func (q *qdrantStore) Count(ctx context.Context) (int, error) {
	var out struct {
		Result struct {
			Count int `json:"count"`
		} `json:"result"`
	}
	err := q.do(ctx, http.MethodPost, "/collections/"+q.collection+"/points/count", map[string]any{"exact": true}, &out)
	if err != nil {
		return 0, err
	}
	return out.Result.Count, nil
}

func (q *qdrantStore) Query(ctx context.Context, embedding []float32, topK int) ([]Result, error) {
	body := map[string]any{
		"vector":       embedding,
		"limit":        topK,
		"with_payload": true,
	}
	var out struct {
		Result []struct {
			Score   float64 `json:"score"`
			Payload struct {
				Document    string `json:"document"`
				PageID      int    `json:"page_id"`
				PageName    string `json:"page_name"`
				BookName    string `json:"book_name"`
				ChapterName string `json:"chapter_name"`
				URL         string `json:"url"`
				ChunkIndex  int    `json:"chunk_index"`
				UpdatedAt   string `json:"updated_at"`
			} `json:"payload"`
		} `json:"result"`
	}
	if err := q.do(ctx, http.MethodPost, "/collections/"+q.collection+"/points/search", body, &out); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(out.Result))
	for _, r := range out.Result {
		results = append(results, Result{
			Document: r.Payload.Document,
			Meta: Meta{
				PageID:      r.Payload.PageID,
				PageName:    r.Payload.PageName,
				BookName:    r.Payload.BookName,
				ChapterName: r.Payload.ChapterName,
				URL:         r.Payload.URL,
				ChunkIndex:  r.Payload.ChunkIndex,
				UpdatedAt:   r.Payload.UpdatedAt,
			},
			Distance: 1 - r.Score, // Cosine-Ähnlichkeit -> Distanz
		})
	}
	return results, nil
}

func (q *qdrantStore) Close() error { return nil }
