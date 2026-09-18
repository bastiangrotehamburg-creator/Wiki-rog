// Package webui stellt die Chat-Weboberfläche und die HTTP-API bereit.
// Bei aktivierter Auth (auth.Service != nil) gibt es Login + gruppenbasierte
// Collections; sonst läuft die UI offen mit den Standard-Collections.
// Admins können das Wiki per Button neu einlesen (Reindex).
package webui

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"

	"wiki-rog/internal/admin"
	"wiki-rog/internal/auth"
	"wiki-rog/internal/rag"
)

//go:embed index.html
var indexHTML []byte

//go:embed login.html
var loginHTML []byte

// AdminConfig bündelt alles für die Reindex-Funktion.
type AdminConfig struct {
	Runner       *admin.Runner
	OpenAdmin    bool           // Reindex im offenen Modus erlauben
	OpenTargets  []admin.Target // Ziel(e) im offenen Modus
	GroupTargets []admin.Target // Ziele im Gruppenmodus (aus access.json)
}

type Server struct {
	engine   *rag.Engine
	auth     *auth.Service // nil = Auth deaktiviert
	defaults []string      // Collections im offenen Modus
	admin    AdminConfig
}

// New erstellt den Server. authSvc darf nil sein (offener Modus).
func New(engine *rag.Engine, authSvc *auth.Service, defaultCollections []string, adm AdminConfig) *Server {
	return &Server{engine: engine, auth: authSvc, defaults: defaultCollections, admin: adm}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/api/ask", s.handleAsk)
	mux.HandleFunc("/api/admin/reindex", s.handleReindex)
	mux.HandleFunc("/api/admin/status", s.handleAdminStatus)
	mux.HandleFunc("/", s.handleIndex)
	return mux
}

func (s *Server) authEnabled() bool { return s.auth != nil }

// isAdmin prüft, ob die Anfrage Admin-Rechte hat.
func (s *Server) isAdmin(r *http.Request) bool {
	if !s.authEnabled() {
		return s.admin.OpenAdmin
	}
	sess, ok := s.auth.SessionFrom(r)
	return ok && sess.Admin
}

// reindexTargets liefert die Ziele, die diese Anfrage neu einlesen darf.
func (s *Server) reindexTargets() []admin.Target {
	if !s.authEnabled() {
		return s.admin.OpenTargets
	}
	return s.admin.GroupTargets
}

// collectionsForRequest ermittelt die erlaubten Collections der Anfrage.
func (s *Server) collectionsForRequest(r *http.Request) ([]string, bool) {
	if !s.authEnabled() {
		return s.defaults, true
	}
	sess, ok := s.auth.SessionFrom(r)
	if !ok {
		return nil, false
	}
	return s.auth.CollectionsFor(sess.Groups), true
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	page := indexHTML
	if s.authEnabled() {
		if _, ok := s.auth.SessionFrom(r); !ok {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		page = bytes.Replace(page, []byte(`data-auth="0"`), []byte(`data-auth="1"`), 1)
	}
	if s.isAdmin(r) {
		page = bytes.Replace(page, []byte(`data-admin="0"`), []byte(`data-admin="1"`), 1)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(page)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.authEnabled() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(loginHTML)
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "ungültige Anfrage", http.StatusBadRequest)
			return
		}
		user, ok := s.auth.Authenticate(r.PostFormValue("username"), r.PostFormValue("password"))
		if !ok {
			http.Redirect(w, r, "/login?e=1", http.StatusFound)
			return
		}
		http.SetCookie(w, s.auth.IssueCookie(user))
		http.Redirect(w, r, "/", http.StatusFound)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if s.authEnabled() {
		http.SetCookie(w, s.auth.ClearCookie())
	}
	http.Redirect(w, r, "/login", http.StatusFound)
}

// handleAsk streamt die Antwort als Server-Sent Events.
func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	collections, ok := s.collectionsForRequest(r)
	if !ok {
		http.Error(w, "nicht angemeldet", http.StatusUnauthorized)
		return
	}
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

	sources, err := s.engine.AnswerStream(r.Context(), question, collections, func(tok string) {
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

// handleReindex startet einen (Neu-)Indizierungslauf (nur Admin).
func (s *Server) handleReindex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.isAdmin(r) {
		http.Error(w, "keine Admin-Berechtigung", http.StatusForbidden)
		return
	}
	reset := r.URL.Query().Get("reset") == "1"
	if err := s.admin.Runner.Start(s.reindexTargets(), reset); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, s.admin.Runner.Status())
}

// handleAdminStatus liefert den aktuellen Indizierungsstatus (nur Admin).
func (s *Server) handleAdminStatus(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		http.Error(w, "keine Admin-Berechtigung", http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, s.admin.Runner.Status())
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// Serve startet den HTTP-Server (blockierend).
func Serve(_ context.Context, addr string, engine *rag.Engine, authSvc *auth.Service, defaultCollections []string, adm AdminConfig) error {
	srv := &http.Server{Addr: addr, Handler: New(engine, authSvc, defaultCollections, adm).Handler()}
	return srv.ListenAndServe()
}
