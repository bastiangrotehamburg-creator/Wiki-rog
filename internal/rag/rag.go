// Package rag verbindet Retrieval (Vektorspeicher) mit der Antwortgenerierung.
// Es wird ausschließlich der abgerufene BookStack-Kontext genutzt.
package rag

import (
	"context"
	"fmt"
	"strings"

	"wiki-rog/internal/config"
	"wiki-rog/internal/ollama"
	"wiki-rog/internal/store"
)

const SystemPrompt = `Du bist der Wiki-Assistent und beantwortest Fragen AUSSCHLIESSLICH auf Basis der bereitgestellten Auszüge aus dem BookStack-Wiki (KONTEXT).

Regeln:
- Nutze NUR Informationen aus dem KONTEXT. Verwende KEIN sonstiges Vorwissen.
- Wenn die Antwort nicht im KONTEXT steht, sage klar: "Das steht nicht im Wiki."
- Erfinde nichts und rate nicht.
- Antworte in der Sprache der Frage, präzise und knapp.
- Verweise wo sinnvoll auf die Quelle über die Nummer in eckigen Klammern, z.B. [1].`

const NoContextMsg = "Dazu finde ich nichts im Wiki."

type Engine struct {
	cfg    config.Config
	ollama *ollama.Client
	store  store.Store
}

func New(cfg config.Config, oc *ollama.Client, st store.Store) *Engine {
	return &Engine{cfg: cfg, ollama: oc, store: st}
}

// Retrieve holt die relevanten Chunks (nach Distanzschwelle gefiltert).
func (e *Engine) Retrieve(ctx context.Context, question string) ([]store.Result, error) {
	emb, err := e.ollama.Embed(ctx, e.cfg.OllamaEmbedModel, question)
	if err != nil {
		return nil, err
	}
	hits, err := e.store.Query(ctx, emb, e.cfg.RetrievalTopK)
	if err != nil {
		return nil, err
	}
	out := hits[:0]
	for _, h := range hits {
		if h.Distance <= e.cfg.RetrievalMaxDistance {
			out = append(out, h)
		}
	}
	return out, nil
}

func buildContext(chunks []store.Result) string {
	var b strings.Builder
	for i, ch := range chunks {
		loc := joinNonEmpty(" > ", ch.Meta.BookName, ch.Meta.PageName)
		if i > 0 {
			b.WriteString("\n\n---\n\n")
		}
		fmt.Fprintf(&b, "[%d] Quelle: %s\n%s", i+1, loc, ch.Document)
	}
	return b.String()
}

func (e *Engine) userPrompt(question string, chunks []store.Result) string {
	return fmt.Sprintf("KONTEXT:\n%s\n\nFRAGE: %s\n\nAntwort:", buildContext(chunks), question)
}

// Answer liefert die vollständige Antwort inkl. Quellen.
func (e *Engine) Answer(ctx context.Context, question string) (string, []store.Result, error) {
	chunks, err := e.Retrieve(ctx, question)
	if err != nil {
		return "", nil, err
	}
	if len(chunks) == 0 {
		return NoContextMsg, nil, nil
	}
	text, err := e.ollama.Chat(ctx, e.cfg.OllamaLLMModel, SystemPrompt, e.userPrompt(question, chunks))
	if err != nil {
		return "", nil, err
	}
	return text, chunks, nil
}

// AnswerStream streamt die Antwort token-weise über onToken und gibt die Quellen zurück.
func (e *Engine) AnswerStream(ctx context.Context, question string, onToken func(string)) ([]store.Result, error) {
	chunks, err := e.Retrieve(ctx, question)
	if err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		onToken(NoContextMsg)
		return nil, nil
	}
	err = e.ollama.ChatStream(ctx, e.cfg.OllamaLLMModel, SystemPrompt, e.userPrompt(question, chunks), onToken)
	return chunks, err
}

// FormatSources erzeugt eine nummerierte Quellenliste (dedupliziert je Seite).
func FormatSources(chunks []store.Result) string {
	seen := map[int]bool{}
	var lines []string
	n := 0
	for _, ch := range chunks {
		if seen[ch.Meta.PageID] {
			continue
		}
		seen[ch.Meta.PageID] = true
		n++
		loc := joinNonEmpty(" > ", ch.Meta.BookName, ch.Meta.PageName)
		if ch.Meta.URL != "" {
			lines = append(lines, fmt.Sprintf("[%d] %s – %s", n, loc, ch.Meta.URL))
		} else {
			lines = append(lines, fmt.Sprintf("[%d] %s", n, loc))
		}
	}
	return strings.Join(lines, "\n")
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}
