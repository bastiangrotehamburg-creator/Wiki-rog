// Package bookstack ist ein Client für die BookStack REST-API.
// Doku: https://demo.bookstackapp.com/api/docs
package bookstack

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Page ist eine aufbereitete Wiki-Seite inkl. Inhalt.
type Page struct {
	ID          int
	Name        string
	Slug        string
	BookID      int
	BookName    string
	ChapterName string
	Content     string
	URL         string
	UpdatedAt   string
}

type Client struct {
	baseURL string
	auth    string
	http    *http.Client
}

func New(baseURL, tokenID, tokenSecret string, verifySSL bool) *Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !verifySSL}, //nolint:gosec // per Config gesteuert
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		auth:    fmt.Sprintf("Token %s:%s", tokenID, tokenSecret),
		http:    &http.Client{Timeout: 60 * time.Second, Transport: tr},
	}
}

func (c *Client) get(path string, params url.Values, out any) error {
	u := c.baseURL + "/api/" + strings.TrimLeft(path, "/")
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("BookStack-Authentifizierung fehlgeschlagen (401); prüfe BOOKSTACK_TOKEN_ID/SECRET")
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("BookStack %s -> HTTP %d: %s", path, resp.StatusCode, truncate(string(body), 200))
	}
	return json.Unmarshal(body, out)
}

// TestConnection prüft Erreichbarkeit/Auth und gibt eine Bestätigung zurück.
func (c *Client) TestConnection() (string, error) {
	var res struct {
		Total int `json:"total"`
	}
	if err := c.get("pages", url.Values{"count": {"1"}}, &res); err != nil {
		return "", err
	}
	return fmt.Sprintf("Verbindung ok – %d Seite(n) verfügbar.", res.Total), nil
}

type listResp struct {
	Data  []map[string]any `json:"data"`
	Total int              `json:"total"`
}

func (c *Client) listAll(path string, fn func(map[string]any) error) error {
	offset, count := 0, 500
	for {
		var res listResp
		params := url.Values{"count": {strconv.Itoa(count)}, "offset": {strconv.Itoa(offset)}}
		if err := c.get(path, params, &res); err != nil {
			return err
		}
		for _, item := range res.Data {
			if err := fn(item); err != nil {
				return err
			}
		}
		offset += len(res.Data)
		if len(res.Data) == 0 || offset >= res.Total {
			return nil
		}
	}
}

func (c *Client) bookIDsForShelves(shelfIDs []int) (map[int]bool, error) {
	out := map[int]bool{}
	for _, id := range shelfIDs {
		var res struct {
			Books []struct {
				ID int `json:"id"`
			} `json:"books"`
		}
		if err := c.get("shelves/"+strconv.Itoa(id), nil, &res); err != nil {
			return nil, err
		}
		for _, b := range res.Books {
			out[b.ID] = true
		}
	}
	return out, nil
}

// IterPages ruft fn für jede (gefilterte) Seite inkl. Inhalt auf.
// Leere Filter bedeuten: alle Seiten.
func (c *Client) IterPages(bookIDs, shelfIDs []int, fn func(Page) error) error {
	var allowed map[int]bool
	if len(bookIDs) > 0 || len(shelfIDs) > 0 {
		allowed = map[int]bool{}
		for _, id := range bookIDs {
			allowed[id] = true
		}
		if len(shelfIDs) > 0 {
			extra, err := c.bookIDsForShelves(shelfIDs)
			if err != nil {
				return err
			}
			for id := range extra {
				allowed[id] = true
			}
		}
	}

	bookNames := map[int]string{}

	return c.listAll("pages", func(stub map[string]any) error {
		bookID := toInt(stub["book_id"])
		if allowed != nil && !allowed[bookID] {
			return nil
		}
		pageID := toInt(stub["id"])

		var detail struct {
			ID        int    `json:"id"`
			Name      string `json:"name"`
			Slug      string `json:"slug"`
			BookID    int    `json:"book_id"`
			Markdown  string `json:"markdown"`
			HTML      string `json:"html"`
			UpdatedAt string `json:"updated_at"`
			Chapter   struct {
				Name string `json:"name"`
			} `json:"chapter"`
		}
		if err := c.get("pages/"+strconv.Itoa(pageID), nil, &detail); err != nil {
			return err
		}

		content := strings.TrimSpace(detail.Markdown)
		if content == "" {
			content = stripHTML(detail.HTML)
		}
		if content == "" {
			return nil
		}

		if _, ok := bookNames[detail.BookID]; !ok && detail.BookID != 0 {
			var b struct {
				Name string `json:"name"`
			}
			if err := c.get("books/"+strconv.Itoa(detail.BookID), nil, &b); err == nil {
				bookNames[detail.BookID] = b.Name
			} else {
				bookNames[detail.BookID] = ""
			}
		}

		return fn(Page{
			ID:          detail.ID,
			Name:        detail.Name,
			Slug:        detail.Slug,
			BookID:      detail.BookID,
			BookName:    bookNames[detail.BookID],
			ChapterName: detail.Chapter.Name,
			Content:     content,
			URL:         fmt.Sprintf("%s/link/%d", c.baseURL, detail.ID),
			UpdatedAt:   detail.UpdatedAt,
		})
	})
}

// --- HTML->Text-Fallback ---------------------------------------------------

var (
	reScriptStyle = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	reBr          = regexp.MustCompile(`(?i)<br\s*/?>`)
	reBlockEnd    = regexp.MustCompile(`(?i)</(p|div|li|h[1-6]|tr)>`)
	reTag         = regexp.MustCompile(`<[^>]+>`)
	reSpaces      = regexp.MustCompile(`[ \t]+`)
	reMultiNL     = regexp.MustCompile(`\n{3,}`)
)

var htmlEntities = strings.NewReplacer(
	"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'",
)

func stripHTML(html string) string {
	if strings.TrimSpace(html) == "" {
		return ""
	}
	s := reScriptStyle.ReplaceAllString(html, "")
	s = reBr.ReplaceAllString(s, "\n")
	s = reBlockEnd.ReplaceAllString(s, "\n")
	s = reTag.ReplaceAllString(s, "")
	s = htmlEntities.Replace(s)
	s = reSpaces.ReplaceAllString(s, " ")
	s = reMultiNL.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
