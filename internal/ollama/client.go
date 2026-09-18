// Package ollama ist ein dünner Client für die lokale Ollama-API
// (Embeddings + Chat, jeweils auch als Stream).
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 300 * time.Second},
	}
}

// Check stellt sicher, dass Ollama erreichbar ist.
func (c *Client) Check(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("Ollama nicht erreichbar unter %s (läuft 'ollama serve'?): %w", c.baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Ollama /api/tags -> HTTP %d", resp.StatusCode)
	}
	return nil
}

// Embed erzeugt einen Embedding-Vektor für text.
func (c *Client) Embed(ctx context.Context, model, text string) ([]float32, error) {
	body, _ := json.Marshal(map[string]any{"model": model, "prompt": text})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/embeddings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Ollama /api/embeddings -> HTTP %d (Modell '%s' installiert? 'ollama pull %s')", resp.StatusCode, model, model)
	}
	var out struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Embedding) == 0 {
		return nil, fmt.Errorf("Ollama lieferte kein Embedding (Modell '%s')", model)
	}
	return out.Embedding, nil
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (c *Client) chatRequest(ctx context.Context, model, system, user string, stream bool, temp float64) (*http.Response, error) {
	body, _ := json.Marshal(map[string]any{
		"model": model,
		"messages": []message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		"stream":  stream,
		"options": map[string]any{"temperature": temp},
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return c.http.Do(req)
}

// Chat gibt die vollständige Antwort zurück.
func (c *Client) Chat(ctx context.Context, model, system, user string) (string, error) {
	resp, err := c.chatRequest(ctx, model, system, user, false, 0.2)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("Ollama /api/chat -> HTTP %d (Modell '%s')", resp.StatusCode, model)
	}
	var out struct {
		Message message `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Message.Content, nil
}

// ChatStream ruft onToken für jedes Teilstück der Antwort auf.
func (c *Client) ChatStream(ctx context.Context, model, system, user string, onToken func(string)) error {
	resp, err := c.chatRequest(ctx, model, system, user, true, 0.2)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Ollama /api/chat -> HTTP %d (Modell '%s')", resp.StatusCode, model)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var chunk struct {
			Message message `json:"message"`
			Done    bool    `json:"done"`
		}
		if err := json.Unmarshal(line, &chunk); err != nil {
			continue
		}
		if chunk.Message.Content != "" {
			onToken(chunk.Message.Content)
		}
		if chunk.Done {
			break
		}
	}
	return sc.Err()
}
