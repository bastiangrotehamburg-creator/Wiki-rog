"""Zerlegt lange Texte in überlappende Chunks für die Vektorsuche."""
from __future__ import annotations

import re

_PARA_SPLIT = re.compile(r"\n\s*\n")


def _split_paragraphs(text: str) -> list[str]:
    return [p.strip() for p in _PARA_SPLIT.split(text) if p.strip()]


def chunk_text(text: str, chunk_size: int = 1000, overlap: int = 150) -> list[str]:
    """Teilt Text in Chunks von ~chunk_size Zeichen mit Überlappung.

    Es wird versucht, an Absatzgrenzen zu schneiden; überlange Absätze
    werden hart geteilt.
    """
    text = text.strip()
    if not text:
        return []
    if len(text) <= chunk_size:
        return [text]

    overlap = max(0, min(overlap, chunk_size - 1))
    chunks: list[str] = []
    current = ""

    def flush() -> None:
        nonlocal current
        if current.strip():
            chunks.append(current.strip())
        current = ""

    for para in _split_paragraphs(text):
        # Absatz alleine schon zu groß -> hart schneiden
        if len(para) > chunk_size:
            flush()
            start = 0
            while start < len(para):
                chunks.append(para[start : start + chunk_size].strip())
                start += chunk_size - overlap
            continue

        if not current:
            current = para
        elif len(current) + 2 + len(para) <= chunk_size:
            current += "\n\n" + para
        else:
            flush()
            current = para

    flush()

    # Überlappung zwischen benachbarten Chunks einfügen
    if overlap > 0 and len(chunks) > 1:
        overlapped: list[str] = [chunks[0]]
        for i in range(1, len(chunks)):
            tail = chunks[i - 1][-overlap:]
            overlapped.append((tail + "\n\n" + chunks[i]).strip())
        return overlapped

    return chunks
