// Package webui stellt die Chat-Weboberfläche und die HTTP-API bereit.
package webui

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"

	"wiki-rog/internal/rag"
)

//go:embed index.html
var indexHTML []byte

type Server struct {
	engine *rag.Engine
}

func New(engine *rag.Engine) *Server {
	return &Server{engine: engine}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/api/ask", s.handleAsk)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

// handleAsk streamt die Antwort als Server-Sent Events.
func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	question := r.URL.Query().Get("q")
	if question == "" {
		http.Error(w, "Parameter q fehlt", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming nicht unterstützt", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	send := func(event string, payload any) {
		data, _ := json.Marshal(payload)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		flusher.Flush()
	}

	ctx := r.Context()
	sources, err := s.engine.AnswerStream(ctx, question, func(tok string) {
		send("token", tok)
	})
	if err != nil {
		send("error-msg", err.Error())
		send("done", struct{}{})
		return
	}
	send("sources", rag.FormatSources(sources))
	send("done", struct{}{})
}

// Serve startet den HTTP-Server (blockierend).
func Serve(_ context.Context, addr string, engine *rag.Engine) error {
	srv := &http.Server{Addr: addr, Handler: New(engine).Handler()}
	return srv.ListenAndServe()
}
