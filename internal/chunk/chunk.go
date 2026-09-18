// Package chunk zerlegt lange Texte in überlappende Chunks für die Vektorsuche.
package chunk

import (
	"regexp"
	"strings"
)

var paraSplit = regexp.MustCompile(`\n\s*\n`)

func splitParagraphs(text string) []string {
	var out []string
	for _, p := range paraSplit.Split(text, -1) {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Text teilt s in Chunks von ~size Zeichen mit overlap Überlappung.
// Es wird an Absatzgrenzen geschnitten; überlange Absätze werden hart geteilt.
func Text(s string, size, overlap int) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if len([]rune(s)) <= size {
		return []string{s}
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap > size-1 {
		overlap = size - 1
	}

	var chunks []string
	var current strings.Builder

	flush := func() {
		if c := strings.TrimSpace(current.String()); c != "" {
			chunks = append(chunks, c)
		}
		current.Reset()
	}

	for _, para := range splitParagraphs(s) {
		pr := []rune(para)
		if len(pr) > size {
			flush()
			for start := 0; start < len(pr); start += size - overlap {
				end := start + size
				if end > len(pr) {
					end = len(pr)
				}
				chunks = append(chunks, strings.TrimSpace(string(pr[start:end])))
			}
			continue
		}
		switch {
		case current.Len() == 0:
			current.WriteString(para)
		case len([]rune(current.String()))+2+len(pr) <= size:
			current.WriteString("\n\n")
			current.WriteString(para)
		default:
			flush()
			current.WriteString(para)
		}
	}
	flush()

	// Überlappung zwischen benachbarten Chunks einfügen.
	if overlap > 0 && len(chunks) > 1 {
		out := make([]string, 0, len(chunks))
		out = append(out, chunks[0])
		for i := 1; i < len(chunks); i++ {
			prev := []rune(chunks[i-1])
			tailStart := len(prev) - overlap
			if tailStart < 0 {
				tailStart = 0
			}
			tail := string(prev[tailStart:])
			out = append(out, strings.TrimSpace(tail+"\n\n"+chunks[i]))
		}
		return out
	}
	return chunks
}
