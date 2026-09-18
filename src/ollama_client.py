"""Dünner Client für die lokale Ollama-API (Embeddings + Generierung)."""
from __future__ import annotations

from typing import Iterator

import requests


class OllamaClient:
    def __init__(self, base_url: str, timeout: int = 300):
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout

    # -- Embeddings --------------------------------------------------------
    def embed(self, model: str, text: str) -> list[float]:
        resp = requests.post(
            f"{self.base_url}/api/embeddings",
            json={"model": model, "prompt": text},
            timeout=self.timeout,
        )
        resp.raise_for_status()
        embedding = resp.json().get("embedding")
        if not embedding:
            raise RuntimeError(
                f"Ollama lieferte kein Embedding zurück (Modell '{model}'). "
                "Ist das Modell installiert? -> ollama pull " + model
            )
        return embedding

    def embed_batch(self, model: str, texts: list[str]) -> list[list[float]]:
        return [self.embed(model, t) for t in texts]

    # -- Generierung -------------------------------------------------------
    def chat(
        self,
        model: str,
        system: str,
        user: str,
        temperature: float = 0.2,
    ) -> str:
        resp = requests.post(
            f"{self.base_url}/api/chat",
            json={
                "model": model,
                "messages": [
                    {"role": "system", "content": system},
                    {"role": "user", "content": user},
                ],
                "stream": False,
                "options": {"temperature": temperature},
            },
            timeout=self.timeout,
        )
        resp.raise_for_status()
        return resp.json().get("message", {}).get("content", "").strip()

    def chat_stream(
        self,
        model: str,
        system: str,
        user: str,
        temperature: float = 0.2,
    ) -> Iterator[str]:
        import json

        with requests.post(
            f"{self.base_url}/api/chat",
            json={
                "model": model,
                "messages": [
                    {"role": "system", "content": system},
                    {"role": "user", "content": user},
                ],
                "stream": True,
                "options": {"temperature": temperature},
            },
            timeout=self.timeout,
            stream=True,
        ) as resp:
            resp.raise_for_status()
            for line in resp.iter_lines():
                if not line:
                    continue
                data = json.loads(line)
                token = data.get("message", {}).get("content", "")
                if token:
                    yield token
                if data.get("done"):
                    break

    def check(self) -> None:
        """Wirft einen Fehler, wenn Ollama nicht erreichbar ist."""
        try:
            requests.get(f"{self.base_url}/api/tags", timeout=10).raise_for_status()
        except requests.RequestException as exc:
            raise RuntimeError(
                f"Ollama nicht erreichbar unter {self.base_url}. "
                "Läuft der Dienst? -> ollama serve"
            ) from exc
