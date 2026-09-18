"""ChromaDB-Wrapper: persistenter, lokaler Vektorspeicher."""
from __future__ import annotations

from dataclasses import dataclass

import chromadb
from chromadb.config import Settings


@dataclass
class Retrieved:
    text: str
    metadata: dict
    distance: float


class VectorStore:
    def __init__(self, persist_dir: str, collection: str):
        self.client = chromadb.PersistentClient(
            path=persist_dir,
            settings=Settings(anonymized_telemetry=False, allow_reset=True),
        )
        # Cosine-Distanz für normalisierte Ähnlichkeit
        self.collection = self.client.get_or_create_collection(
            name=collection,
            metadata={"hnsw:space": "cosine"},
        )

    def reset(self) -> None:
        name = self.collection.name
        self.client.delete_collection(name)
        self.collection = self.client.get_or_create_collection(
            name=name, metadata={"hnsw:space": "cosine"}
        )

    def delete_page(self, page_id: int) -> None:
        self.collection.delete(where={"page_id": page_id})

    def add(
        self,
        ids: list[str],
        embeddings: list[list[float]],
        documents: list[str],
        metadatas: list[dict],
    ) -> None:
        if not ids:
            return
        self.collection.upsert(
            ids=ids,
            embeddings=embeddings,
            documents=documents,
            metadatas=metadatas,
        )

    def count(self) -> int:
        return self.collection.count()

    def query(self, embedding: list[float], top_k: int) -> list[Retrieved]:
        res = self.collection.query(
            query_embeddings=[embedding],
            n_results=top_k,
            include=["documents", "metadatas", "distances"],
        )
        out: list[Retrieved] = []
        docs = (res.get("documents") or [[]])[0]
        metas = (res.get("metadatas") or [[]])[0]
        dists = (res.get("distances") or [[]])[0]
        for text, meta, dist in zip(docs, metas, dists):
            out.append(Retrieved(text=text, metadata=meta or {}, distance=dist))
        return out
