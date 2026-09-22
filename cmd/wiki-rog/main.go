// Command wiki-rog ist ein lokales RAG über BookStack-Daten (Ollama + Vektorspeicher).
//
// Unterbefehle:
//
//	wiki-rog test               Verbindungen (BookStack + Ollama) prüfen
//	wiki-rog ingest [--reset]   Wiki indexieren (inkrementell bzw. Neuaufbau)
//	wiki-rog ask "<Frage>"      einzelne Frage stellen
//	wiki-rog chat               interaktiver Chat im Terminal
//	wiki-rog serve              Web-UI starten (HTTP)
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"wiki-rog/internal/admin"
	"wiki-rog/internal/auth"
	"wiki-rog/internal/bookstack"
	"wiki-rog/internal/config"
	"wiki-rog/internal/ingest"
	"wiki-rog/internal/ollama"
	"wiki-rog/internal/rag"
	"wiki-rog/internal/store"
	"wiki-rog/internal/webui"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cfg := config.Load()
	ctx := context.Background()

	var err error
	switch os.Args[1] {
	case "test":
		err = cmdTest(ctx, cfg)
	case "ingest":
		err = cmdIngest(ctx, cfg, os.Args[2:])
	case "ask":
		err = cmdAsk(ctx, cfg, os.Args[2:])
	case "chat":
		err = cmdChat(ctx, cfg)
	case "serve":
		err = cmdServe(ctx, cfg)
	case "hashpw":
		err = cmdHashpw(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "Unbekannter Befehl: %s\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Fehler:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `wiki-rog – lokales RAG über BookStack-Daten

Verwendung:
  wiki-rog test               Verbindungen (BookStack + Ollama) prüfen
  wiki-rog ingest [--reset]   Wiki indexieren (inkrementell bzw. Neuaufbau)
  wiki-rog ask "<Frage>"      einzelne Frage stellen
  wiki-rog chat               interaktiver Chat im Terminal
  wiki-rog serve              Web-UI starten (HTTP)
  wiki-rog hashpw [passwort]  Passwort-Hash für die Access-Konfig erzeugen
`)
}

func cmdTest(ctx context.Context, cfg config.Config) error {
	if err := cfg.RequireBookStack(); err != nil {
		return err
	}
	fmt.Println("→ Prüfe BookStack (extern) …")
	fmt.Println("  Wiki:", cfg.BookStackURL)
	bs := bookstack.New(cfg.BookStackURL, cfg.BookStackTokenID, cfg.BookStackTokenSecret, cfg.BookStackVerifySSL)
	msg, err := bs.TestConnection()
	if err != nil {
		return err
	}
	fmt.Println("  " + msg)

	fmt.Println("→ Prüfe Ollama …")
	oc := ollama.New(cfg.OllamaURL)
	if err := oc.Check(ctx); err != nil {
		return err
	}
	emb, err := oc.Embed(ctx, cfg.OllamaEmbedModel, "test")
	if err != nil {
		return err
	}
	fmt.Printf("  Embeddings ok (Modell '%s', Dim=%d).\n", cfg.OllamaEmbedModel, len(emb))
	reply, err := oc.Chat(ctx, cfg.OllamaLLMModel, "Antworte mit 'ok'.", "Sag ok.")
	if err != nil {
		return err
	}
	fmt.Printf("  LLM ok (Modell '%s'): %s\n", cfg.OllamaLLMModel, truncate(reply, 40))

	fmt.Printf("→ Vektorspeicher: %s (Collection '%s')\n", cfg.VectorBackend, cfg.VectorCollection)
	fmt.Println("Alles bereit. ✅")
	return nil
}

func cmdIngest(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("ingest", flag.ExitOnError)
	reset := fs.Bool("reset", false, "Index vor dem Aufbau komplett leeren")
	_ = fs.Parse(args)

	st, err := store.Open(cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	if *reset {
		fmt.Println("→ Leere bestehenden Index …")
		if err := st.Reset(ctx); err != nil {
			return err
		}
	}

	fmt.Printf("→ Starte Indexierung (reset=%v) …\n", *reset)
	stats, err := ingest.Run(ctx, cfg, st, func(msg string) {
		fmt.Println("  " + msg)
	})
	if err != nil {
		return err
	}
	fmt.Printf("Fertig: %d Seite(n), %d Chunk(s). Index enthält jetzt %d Einträge.\n",
		stats.Pages, stats.Chunks, stats.CollectionSize)
	return nil
}

func newEngine(cfg config.Config) (*rag.Engine, *store.Manager) {
	mgr := store.NewManager(cfg)
	return rag.New(cfg, ollama.New(cfg.OllamaURL), mgr), mgr
}

func cmdAsk(ctx context.Context, cfg config.Config, args []string) error {
	question := strings.TrimSpace(strings.Join(args, " "))
	if question == "" {
		return fmt.Errorf("bitte eine Frage angeben: wiki-rog ask \"...\"")
	}
	engine, mgr := newEngine(cfg)
	defer mgr.Close()

	sources, err := engine.AnswerStream(ctx, question, []string{cfg.VectorCollection}, func(tok string) {
		fmt.Print(tok)
	})
	if err != nil {
		return err
	}
	fmt.Println()
	printSources(sources)
	return nil
}

func cmdChat(ctx context.Context, cfg config.Config) error {
	engine, mgr := newEngine(cfg)
	defer mgr.Close()
	collections := []string{cfg.VectorCollection}

	fmt.Println("Wiki-Chat (nur BookStack-Wissen). 'exit' zum Beenden.")
	fmt.Println()
	sc := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("Du: ")
		if !sc.Scan() {
			break
		}
		question := strings.TrimSpace(sc.Text())
		switch strings.ToLower(question) {
		case "":
			continue
		case "exit", "quit", "q":
			return nil
		}
		fmt.Print("Wiki: ")
		sources, err := engine.AnswerStream(ctx, question, collections, func(tok string) {
			fmt.Print(tok)
		})
		if err != nil {
			fmt.Println("\nFehler:", err)
			continue
		}
		fmt.Println()
		printSources(sources)
		fmt.Println()
	}
	return nil
}

func cmdServe(ctx context.Context, cfg config.Config) error {
	engine, mgr := newEngine(cfg)
	defer mgr.Close()

	authSvc, err := auth.Load(cfg.AccessConfig, cfg.UsersFile, cfg.SessionSecret, cfg.SessionTTLHours, cfg.SessionSecure)
	if err != nil {
		return err
	}
	mode := "offen (kein Login, eine Collection)"
	if authSvc != nil {
		mode = "Login + gruppenbasierter Zugriff"
	}

	// Admin-Reindex: Ziele je nach Modus.
	adm := webui.AdminConfig{
		Runner:    admin.NewRunner(cfg, mgr),
		OpenAdmin: cfg.OpenAdmin,
		OpenTargets: []admin.Target{{
			Collection:  cfg.VectorCollection,
			TokenID:     cfg.BookStackTokenID,
			TokenSecret: cfg.BookStackTokenSecret,
		}},
	}
	if authSvc != nil {
		for _, t := range authSvc.GroupTargets() {
			adm.GroupTargets = append(adm.GroupTargets, admin.Target{
				Group:       t.Group,
				Collection:  t.Collection,
				TokenID:     t.TokenID,
				TokenSecret: t.TokenSecret,
			})
		}
	}

	fmt.Printf("Wiki-rog Web-UI läuft auf %s (Backend: %s, Zugriff: %s)\n",
		cfg.HTTPAddr, cfg.VectorBackend, mode)
	return webui.Serve(ctx, cfg.HTTPAddr, engine, authSvc, []string{cfg.VectorCollection}, adm)
}

func cmdHashpw(args []string) error {
	var pw string
	if len(args) > 0 {
		pw = args[0]
	} else {
		fmt.Print("Passwort: ")
		sc := bufio.NewScanner(os.Stdin)
		if sc.Scan() {
			pw = sc.Text()
		}
	}
	pw = strings.TrimSpace(pw)
	if pw == "" {
		return fmt.Errorf("leeres Passwort")
	}
	h, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	fmt.Println(h)
	return nil
}

func printSources(sources []store.Result) {
	if len(sources) == 0 {
		return
	}
	fmt.Println("\nQuellen:")
	fmt.Println(rag.FormatSources(sources))
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
