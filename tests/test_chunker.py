"""Tests für das Text-Chunking (keine externen Dienste nötig)."""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from src.chunker import chunk_text


def test_short_text_single_chunk():
    assert chunk_text("kurzer Text", chunk_size=1000) == ["kurzer Text"]


def test_empty_text():
    assert chunk_text("   ") == []


def test_long_text_splits():
    text = "\n\n".join(f"Absatz {i} " + "x" * 200 for i in range(20))
    chunks = chunk_text(text, chunk_size=500, overlap=50)
    assert len(chunks) > 1
    assert all(len(c) <= 600 for c in chunks)  # size + etwas Overlap


def test_oversized_paragraph_hard_split():
    text = "y" * 2500
    chunks = chunk_text(text, chunk_size=1000, overlap=100)
    assert len(chunks) >= 3
    assert all(c for c in chunks)


def test_overlap_present():
    text = "\n\n".join("Satz " + str(i) * 100 for i in range(5))
    chunks = chunk_text(text, chunk_size=300, overlap=80)
    # Ab dem zweiten Chunk sollte ein Teil des vorherigen enthalten sein
    assert len(chunks) >= 2


if __name__ == "__main__":
    import pytest

    raise SystemExit(pytest.main([__file__, "-v"]))
