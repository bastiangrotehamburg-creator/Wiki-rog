#!/usr/bin/env python3
"""Wiki-rog CLI – lokales RAG über BookStack-Daten.

Beispiele:
    python cli.py test          # Verbindungen (BookStack + Ollama) prüfen
    python cli.py ingest        # Wiki indexieren (inkrementell)
    python cli.py ingest --reset  # kompletter Neuaufbau des Index
    python cli.py ask "Wie richte ich VPN ein?"
    python cli.py chat          # interaktiver Chat
"""
from __future__ import annotations

import argparse
import sys

from config import Config
from src.bookstack_client import BookStackClient
from src.ingest import ingest
from src.ollama_client import OllamaClient
from src.rag import RagEngine


def cmd_test(cfg: Config) -> int:
    cfg.require_bookstack()
    print("→ Prüfe BookStack …")
    bs = BookStackClient(
        cfg.bookstack_url, cfg.bookstack_token_id, cfg.bookstack_token_secret
    )
    print("  " + bs.test_connection())

    print("→ Prüfe Ollama …")
    ollama = OllamaClient(cfg.ollama_url)
    ollama.check()
    dim = len(ollama.embed(cfg.ollama_embed_model, "test"))
    print(f"  Embeddings ok (Modell '{cfg.ollama_embed_model}', Dim={dim}).")
    reply = ollama.chat(cfg.ollama_llm_model, "Antworte mit 'ok'.", "Sag ok.")
    print(f"  LLM ok (Modell '{cfg.ollama_llm_model}'): {reply[:40]}")
    print("Alles bereit. ✅")
    return 0


def cmd_ingest(cfg: Config, reset: bool) -> int:
    print(f"→ Starte Indexierung (reset={reset}) …")
    stats = ingest(cfg, reset=reset)
    print(
        f"Fertig: {stats['pages']} Seite(n), {stats['chunks']} Chunk(s). "
        f"Index enthält jetzt {stats['collection_size']} Einträge."
    )
    return 0


def _print_sources(engine: RagEngine, sources) -> None:
    if sources:
        print("\nQuellen:")
        print(engine.format_sources(sources))


def cmd_ask(cfg: Config, question: str) -> int:
    engine = RagEngine(cfg)
    stream, sources = engine.answer_stream(question)
    for token in stream:
        sys.stdout.write(token)
        sys.stdout.flush()
    print()
    _print_sources(engine, sources)
    return 0


def cmd_chat(cfg: Config) -> int:
    engine = RagEngine(cfg)
    print("Wiki-Chat (nur BookStack-Wissen). 'exit' zum Beenden.\n")
    while True:
        try:
            question = input("Du: ").strip()
        except (EOFError, KeyboardInterrupt):
            print()
            break
        if question.lower() in {"exit", "quit", "q"}:
            break
        if not question:
            continue
        print("Wiki: ", end="")
        stream, sources = engine.answer_stream(question)
        for token in stream:
            sys.stdout.write(token)
            sys.stdout.flush()
        print()
        _print_sources(engine, sources)
        print()
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description="Wiki-rog – lokales RAG über BookStack.")
    sub = parser.add_subparsers(dest="command", required=True)

    sub.add_parser("test", help="BookStack- und Ollama-Verbindung prüfen")

    p_ingest = sub.add_parser("ingest", help="BookStack-Inhalte indexieren")
    p_ingest.add_argument(
        "--reset", action="store_true", help="Index vor dem Aufbau komplett leeren"
    )

    p_ask = sub.add_parser("ask", help="Einzelne Frage stellen")
    p_ask.add_argument("question", help="Die Frage an das Wiki")

    sub.add_parser("chat", help="Interaktiver Chat")

    args = parser.parse_args()
    cfg = Config.load()

    try:
        if args.command == "test":
            return cmd_test(cfg)
        if args.command == "ingest":
            return cmd_ingest(cfg, reset=args.reset)
        if args.command == "ask":
            return cmd_ask(cfg, args.question)
        if args.command == "chat":
            return cmd_chat(cfg)
    except RuntimeError as exc:
        print(f"Fehler: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
