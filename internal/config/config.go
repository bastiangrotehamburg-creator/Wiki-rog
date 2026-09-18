// Package config lädt die gesamte Konfiguration aus der Umgebung (optional .env).
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	// BookStack (externes Wiki)
	BookStackURL         string
	BookStackTokenID     string
	BookStackTokenSecret string
	BookIDs              []int
	ShelfIDs             []int
	BookStackVerifySSL   bool

	// Ollama
	OllamaURL        string
	OllamaLLMModel   string
	OllamaEmbedModel string

	// Vektorspeicher
	VectorBackend    string // "local" oder "qdrant"
	VectorCollection string
	StoreDir         string // Backend "local": Verzeichnis der Speicherdatei
	QdrantURL        string
	QdrantAPIKey     string

	// Chunking / Retrieval
	ChunkSize            int
	ChunkOverlap         int
	RetrievalTopK        int
	RetrievalMaxDistance float64

	// Web-Server
	HTTPAddr string

	// Zugriff / Auth (Gruppenmodell). Ist AccessConfig leer bzw. die Datei
	// nicht vorhanden, läuft die WebUI offen (kein Login, eine Collection).
	AccessConfig    string
	SessionSecret   string
	SessionTTLHours int
	SessionSecure   bool
}

// Load liest die Konfiguration. Eine vorhandene .env wird geladen, ohne
// bereits gesetzte Umgebungsvariablen zu überschreiben.
func Load() Config {
	loadDotEnv(".env")
	return Config{
		BookStackURL:         strings.TrimRight(env("BOOKSTACK_URL", ""), "/"),
		BookStackTokenID:     env("BOOKSTACK_TOKEN_ID", ""),
		BookStackTokenSecret: env("BOOKSTACK_TOKEN_SECRET", ""),
		BookIDs:              idList("BOOKSTACK_BOOK_IDS"),
		ShelfIDs:             idList("BOOKSTACK_SHELF_IDS"),
		BookStackVerifySSL:   boolEnv("BOOKSTACK_VERIFY_SSL", true),

		OllamaURL:        strings.TrimRight(env("OLLAMA_URL", "http://localhost:11434"), "/"),
		OllamaLLMModel:   env("OLLAMA_LLM_MODEL", "llama3.2:1b"),
		OllamaEmbedModel: env("OLLAMA_EMBED_MODEL", "nomic-embed-text"),

		VectorBackend:    strings.ToLower(env("VECTOR_BACKEND", "local")),
		VectorCollection: env("VECTOR_COLLECTION", "bookstack"),
		StoreDir:         env("STORE_DIR", "./data/store"),
		QdrantURL:        strings.TrimRight(env("QDRANT_URL", "http://qdrant:6333"), "/"),
		QdrantAPIKey:     env("QDRANT_API_KEY", ""),

		ChunkSize:            intEnv("CHUNK_SIZE", 1000),
		ChunkOverlap:         intEnv("CHUNK_OVERLAP", 150),
		RetrievalTopK:        intEnv("RETRIEVAL_TOP_K", 5),
		RetrievalMaxDistance: floatEnv("RETRIEVAL_MAX_DISTANCE", 1.0),

		HTTPAddr: env("HTTP_ADDR", ":8080"),

		AccessConfig:    env("ACCESS_CONFIG", "./config/access.json"),
		SessionSecret:   os.Getenv("SESSION_SECRET"),
		SessionTTLHours: intEnv("SESSION_TTL_HOURS", 12),
		SessionSecure:   boolEnv("SESSION_SECURE", false),
	}
}

// RequireBookStack prüft, ob die BookStack-Pflichtfelder gesetzt sind.
func (c Config) RequireBookStack() error {
	var missing []string
	if c.BookStackURL == "" {
		missing = append(missing, "BOOKSTACK_URL")
	}
	if c.BookStackTokenID == "" {
		missing = append(missing, "BOOKSTACK_TOKEN_ID")
	}
	if c.BookStackTokenSecret == "" {
		missing = append(missing, "BOOKSTACK_TOKEN_SECRET")
	}
	if len(missing) > 0 {
		return fmt.Errorf("fehlende BookStack-Konfiguration: %s (siehe .env.example)",
			strings.Join(missing, ", "))
	}
	return nil
}

// --- Helfer ----------------------------------------------------------------

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func intEnv(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func floatEnv(key string, def float64) float64 {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	return def
}

func boolEnv(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on", "ja":
		return true
	default:
		return false
	}
}

func idList(key string) []int {
	raw := os.Getenv(key)
	var out []int
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if n, err := strconv.Atoi(part); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// loadDotEnv lädt einfache KEY=VALUE-Zeilen; bereits gesetzte Variablen bleiben.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
}
