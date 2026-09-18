"""Zentrale Konfiguration – liest alle Werte aus der Umgebung (.env)."""
from __future__ import annotations

import os
from dataclasses import dataclass, field

from dotenv import load_dotenv

load_dotenv()


def _int(name: str, default: int) -> int:
    raw = os.getenv(name)
    try:
        return int(raw) if raw not in (None, "") else default
    except ValueError:
        return default


def _float(name: str, default: float) -> float:
    raw = os.getenv(name)
    try:
        return float(raw) if raw not in (None, "") else default
    except ValueError:
        return default


def _bool(name: str, default: bool) -> bool:
    raw = os.getenv(name)
    if raw in (None, ""):
        return default
    return raw.strip().lower() in {"1", "true", "yes", "on", "ja"}


def _id_list(name: str) -> list[int]:
    raw = os.getenv(name, "") or ""
    ids: list[int] = []
    for part in raw.split(","):
        part = part.strip()
        if part.isdigit():
            ids.append(int(part))
    return ids


@dataclass
class Config:
    # BookStack
    bookstack_url: str = ""
    bookstack_token_id: str = ""
    bookstack_token_secret: str = ""
    book_ids: list[int] = field(default_factory=list)
    shelf_ids: list[int] = field(default_factory=list)
    # TLS-Verifizierung gegen das externe Wiki (bei self-signed ggf. False,
    # besser: eigene CA über REQUESTS_CA_BUNDLE einbinden).
    bookstack_verify_ssl: bool = True

    # Ollama
    ollama_url: str = "http://localhost:11434"
    ollama_llm_model: str = "llama3.2:1b"
    ollama_embed_model: str = "nomic-embed-text"

    # Vektorspeicher / Chunking / Retrieval
    chroma_dir: str = "./data/chroma"
    chroma_collection: str = "bookstack"
    chunk_size: int = 1000
    chunk_overlap: int = 150
    retrieval_top_k: int = 5
    retrieval_max_distance: float = 1.0

    @classmethod
    def load(cls) -> "Config":
        return cls(
            bookstack_url=os.getenv("BOOKSTACK_URL", "").rstrip("/"),
            bookstack_token_id=os.getenv("BOOKSTACK_TOKEN_ID", ""),
            bookstack_token_secret=os.getenv("BOOKSTACK_TOKEN_SECRET", ""),
            book_ids=_id_list("BOOKSTACK_BOOK_IDS"),
            shelf_ids=_id_list("BOOKSTACK_SHELF_IDS"),
            bookstack_verify_ssl=_bool("BOOKSTACK_VERIFY_SSL", True),
            ollama_url=os.getenv("OLLAMA_URL", "http://localhost:11434").rstrip("/"),
            ollama_llm_model=os.getenv("OLLAMA_LLM_MODEL", "llama3.2:1b"),
            ollama_embed_model=os.getenv("OLLAMA_EMBED_MODEL", "nomic-embed-text"),
            chroma_dir=os.getenv("CHROMA_DIR", "./data/chroma"),
            chroma_collection=os.getenv("CHROMA_COLLECTION", "bookstack"),
            chunk_size=_int("CHUNK_SIZE", 1000),
            chunk_overlap=_int("CHUNK_OVERLAP", 150),
            retrieval_top_k=_int("RETRIEVAL_TOP_K", 5),
            retrieval_max_distance=_float("RETRIEVAL_MAX_DISTANCE", 1.0),
        )

    def require_bookstack(self) -> None:
        missing = [
            name
            for name, val in [
                ("BOOKSTACK_URL", self.bookstack_url),
                ("BOOKSTACK_TOKEN_ID", self.bookstack_token_id),
                ("BOOKSTACK_TOKEN_SECRET", self.bookstack_token_secret),
            ]
            if not val
        ]
        if missing:
            raise RuntimeError(
                "Fehlende BookStack-Konfiguration: "
                + ", ".join(missing)
                + ". Bitte .env ausfüllen (siehe .env.example)."
            )
