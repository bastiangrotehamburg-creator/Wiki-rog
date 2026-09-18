package store

import (
	"context"
	"testing"
)

func TestLocalStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := openLocal(dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	items := []Item{
		{ID: "page-1-chunk-0", Embedding: []float32{1, 0, 0}, Document: "Katze", Meta: Meta{PageID: 1, PageName: "Tiere"}},
		{ID: "page-1-chunk-1", Embedding: []float32{0, 1, 0}, Document: "Hund", Meta: Meta{PageID: 1, PageName: "Tiere"}},
		{ID: "page-2-chunk-0", Embedding: []float32{0, 0, 1}, Document: "Auto", Meta: Meta{PageID: 2, PageName: "Technik"}},
	}
	if err := st.Upsert(ctx, items); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.Count(ctx); n != 3 {
		t.Fatalf("Count=%d, erwartet 3", n)
	}

	// Ähnlichste zu [1,0,0] muss "Katze" sein.
	res, err := st.Query(ctx, []float32{1, 0, 0}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].Document != "Katze" {
		t.Fatalf("unerwartetes Query-Ergebnis: %+v", res)
	}
	if res[0].Distance > 1e-6 {
		t.Fatalf("Distanz zu identischem Vektor sollte ~0 sein, war %v", res[0].Distance)
	}

	// Persistenz: neu öffnen und erneut zählen.
	st2, err := openLocal(dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := st2.Count(ctx); n != 3 {
		t.Fatalf("nach Reload Count=%d, erwartet 3", n)
	}

	// DeletePage entfernt beide Chunks von Seite 1.
	if err := st2.DeletePage(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if n, _ := st2.Count(ctx); n != 1 {
		t.Fatalf("nach DeletePage Count=%d, erwartet 1", n)
	}

	if err := st2.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := st2.Count(ctx); n != 0 {
		t.Fatalf("nach Reset Count=%d, erwartet 0", n)
	}
}
