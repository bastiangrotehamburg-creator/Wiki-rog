"""RAG-Kern: Retrieval aus dem Vektorspeicher + Antwort ausschließlich
auf Basis der gefundenen BookStack-Inhalte.
"""
from __future__ import annotations

from dataclasses import dataclass
from typing import Iterator

from config import Config
from src.ollama_client import OllamaClient
from src.vectorstore import Retrieved, VectorStore

SYSTEM_PROMPT = """\
Du bist der Wiki-Assistent und beantwortest Fragen AUSSCHLIESSLICH auf Basis \
der bereitgestellten Auszüge aus dem BookStack-Wiki (KONTEXT).

Regeln:
- Nutze NUR Informationen aus dem KONTEXT. Verwende KEIN sonstiges Vorwissen.
- Wenn die Antwort nicht im KONTEXT steht, sage klar: "Das steht nicht im Wiki."
- Erfinde nichts und rate nicht.
- Antworte in der Sprache der Frage, präzise und knapp.
- Verweise wo sinnvoll auf die Quelle über die Nummer in eckigen Klammern, z.B. [1].
"""

NO_CONTEXT_MSG = "Dazu finde ich nichts im Wiki."


@dataclass
class Answer:
    text: str
    sources: list[Retrieved]


def _build_context(chunks: list[Retrieved]) -> str:
    blocks = []
    for i, ch in enumerate(chunks, start=1):
        meta = ch.metadata
        loc = " > ".join(
            p for p in [meta.get("book_name"), meta.get("page_name")] if p
        )
        blocks.append(f"[{i}] Quelle: {loc}\n{ch.text}")
    return "\n\n---\n\n".join(blocks)


def _format_sources(chunks: list[Retrieved]) -> str:
    seen: dict[int, dict] = {}
    order: list[int] = []
    for ch in chunks:
        pid = ch.metadata.get("page_id")
        if pid not in seen:
            seen[pid] = ch.metadata
            order.append(pid)
    lines = []
    for i, pid in enumerate(order, start=1):
        meta = seen[pid]
        loc = " > ".join(
            p for p in [meta.get("book_name"), meta.get("page_name")] if p
        )
        url = meta.get("url", "")
        lines.append(f"[{i}] {loc} – {url}" if url else f"[{i}] {loc}")
    return "\n".join(lines)


class RagEngine:
    def __init__(self, cfg: Config):
        self.cfg = cfg
        self.ollama = OllamaClient(cfg.ollama_url)
        self.store = VectorStore(cfg.chroma_dir, cfg.chroma_collection)

    def retrieve(self, question: str) -> list[Retrieved]:
        q_emb = self.ollama.embed(self.cfg.ollama_embed_model, question)
        hits = self.store.query(q_emb, self.cfg.retrieval_top_k)
        # Nach Distanzschwelle filtern (irrelevante Treffer verwerfen)
        return [h for h in hits if h.distance <= self.cfg.retrieval_max_distance]

    def _user_prompt(self, question: str, chunks: list[Retrieved]) -> str:
        context = _build_context(chunks)
        return f"KONTEXT:\n{context}\n\nFRAGE: {question}\n\nAntwort:"

    def answer(self, question: str) -> Answer:
        chunks = self.retrieve(question)
        if not chunks:
            return Answer(text=NO_CONTEXT_MSG, sources=[])
        text = self.ollama.chat(
            self.cfg.ollama_llm_model,
            SYSTEM_PROMPT,
            self._user_prompt(question, chunks),
        )
        return Answer(text=text, sources=chunks)

    def answer_stream(self, question: str) -> tuple[Iterator[str], list[Retrieved]]:
        """Gibt (Token-Stream, Quellen) zurück. Bei fehlendem Kontext ein Ein-Token-Stream."""
        chunks = self.retrieve(question)
        if not chunks:
            return iter([NO_CONTEXT_MSG]), []
        stream = self.ollama.chat_stream(
            self.cfg.ollama_llm_model,
            SYSTEM_PROMPT,
            self._user_prompt(question, chunks),
        )
        return stream, chunks

    @staticmethod
    def format_sources(chunks: list[Retrieved]) -> str:
        return _format_sources(chunks)
