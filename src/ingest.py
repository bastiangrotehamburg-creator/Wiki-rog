"""Ingest-Pipeline: BookStack -> Chunks -> Embeddings -> Vektorspeicher."""
from __future__ import annotations

from tqdm import tqdm

from config import Config
from src.bookstack_client import BookStackClient, Page
from src.chunker import chunk_text
from src.ollama_client import OllamaClient
from src.vectorstore import VectorStore


def _page_header(page: Page) -> str:
    """Kontextzeile, die jedem Chunk vorangestellt wird (bessere Retrieval-Treffer)."""
    parts = [p for p in [page.book_name, page.chapter_name, page.name] if p]
    return " > ".join(parts)


def ingest(cfg: Config, reset: bool = False) -> dict:
    cfg.require_bookstack()

    bs = BookStackClient(
        cfg.bookstack_url, cfg.bookstack_token_id, cfg.bookstack_token_secret
    )
    ollama = OllamaClient(cfg.ollama_url)
    ollama.check()

    store = VectorStore(cfg.chroma_dir, cfg.chroma_collection)
    if reset:
        store.reset()

    pages = 0
    chunks_total = 0

    page_iter = bs.iter_pages(book_ids=cfg.book_ids, shelf_ids=cfg.shelf_ids)
    for page in tqdm(page_iter, desc="Seiten indexieren", unit="Seite"):
        # Bei erneutem Lauf ohne reset: alte Chunks dieser Seite entfernen
        if not reset:
            store.delete_page(page.id)

        header = _page_header(page)
        chunks = chunk_text(page.content, cfg.chunk_size, cfg.chunk_overlap)
        if not chunks:
            continue

        ids, embeddings, documents, metadatas = [], [], [], []
        for idx, chunk in enumerate(chunks):
            embed_input = f"{header}\n\n{chunk}" if header else chunk
            embeddings.append(ollama.embed(cfg.ollama_embed_model, embed_input))
            ids.append(f"page-{page.id}-chunk-{idx}")
            documents.append(chunk)
            metadatas.append(
                {
                    "page_id": page.id,
                    "page_name": page.name,
                    "book_name": page.book_name,
                    "chapter_name": page.chapter_name,
                    "url": page.url,
                    "chunk_index": idx,
                    "updated_at": page.updated_at,
                }
            )

        store.add(ids, embeddings, documents, metadatas)
        pages += 1
        chunks_total += len(chunks)

    return {
        "pages": pages,
        "chunks": chunks_total,
        "collection_size": store.count(),
    }
