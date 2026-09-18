"""Client für die BookStack REST-API.

Lädt Seiten (inkl. Inhalt) und deren Metadaten. Dokumentation:
https://demo.bookstackapp.com/api/docs
"""
from __future__ import annotations

import re
from dataclasses import dataclass
from typing import Iterator

import requests

_HTML_TAG = re.compile(r"<[^>]+>")
_WS = re.compile(r"[ \t]+")
_MULTI_NL = re.compile(r"\n{3,}")


@dataclass
class Page:
    id: int
    name: str
    slug: str
    book_id: int | None
    book_name: str
    chapter_name: str
    content: str
    url: str
    updated_at: str


def _strip_html(html: str) -> str:
    """Sehr einfacher HTML->Text-Fallback, falls kein Markdown vorliegt."""
    text = re.sub(r"<(script|style)[^>]*>.*?</\1>", "", html, flags=re.S | re.I)
    text = re.sub(r"<br\s*/?>", "\n", text, flags=re.I)
    text = re.sub(r"</(p|div|li|h[1-6]|tr)>", "\n", text, flags=re.I)
    text = _HTML_TAG.sub("", text)
    text = (
        text.replace("&nbsp;", " ")
        .replace("&amp;", "&")
        .replace("&lt;", "<")
        .replace("&gt;", ">")
        .replace("&quot;", '"')
        .replace("&#39;", "'")
    )
    text = _WS.sub(" ", text)
    text = _MULTI_NL.sub("\n\n", text)
    return text.strip()


class BookStackClient:
    def __init__(self, base_url: str, token_id: str, token_secret: str, timeout: int = 60):
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout
        self.session = requests.Session()
        self.session.headers.update(
            {
                "Authorization": f"Token {token_id}:{token_secret}",
                "Accept": "application/json",
            }
        )

    # -- interne Helfer ----------------------------------------------------
    def _get(self, path: str, params: dict | None = None) -> dict:
        resp = self.session.get(
            f"{self.base_url}/api/{path.lstrip('/')}",
            params=params,
            timeout=self.timeout,
        )
        if resp.status_code == 401:
            raise RuntimeError(
                "BookStack-Authentifizierung fehlgeschlagen (401). "
                "Prüfe BOOKSTACK_TOKEN_ID / BOOKSTACK_TOKEN_SECRET."
            )
        resp.raise_for_status()
        return resp.json()

    def _list_all(self, path: str, params: dict | None = None) -> Iterator[dict]:
        """Iteriert über alle Einträge einer paginierten Liste."""
        offset = 0
        count = 500
        while True:
            page_params = dict(params or {})
            page_params.update({"count": count, "offset": offset})
            data = self._get(path, page_params)
            items = data.get("data", [])
            for item in items:
                yield item
            total = data.get("total", 0)
            offset += len(items)
            if not items or offset >= total:
                break

    # -- öffentliche API ---------------------------------------------------
    def test_connection(self) -> str:
        """Gibt bei Erfolg den Namen der ersten Seite / eine Bestätigung zurück."""
        data = self._get("pages", {"count": 1})
        return f"Verbindung ok – {data.get('total', 0)} Seite(n) verfügbar."

    def _book_ids_for_shelves(self, shelf_ids: list[int]) -> set[int]:
        book_ids: set[int] = set()
        for shelf_id in shelf_ids:
            data = self._get(f"shelves/{shelf_id}")
            for book in data.get("books", []):
                if "id" in book:
                    book_ids.add(book["id"])
        return book_ids

    def iter_pages(
        self,
        book_ids: list[int] | None = None,
        shelf_ids: list[int] | None = None,
    ) -> Iterator[Page]:
        """Liefert alle (gefilterten) Seiten inkl. Inhalt.

        book_ids/shelf_ids: leer/None => keine Filterung (alle Seiten).
        """
        allowed_books: set[int] | None = None
        if book_ids or shelf_ids:
            allowed_books = set(book_ids or [])
            if shelf_ids:
                allowed_books |= self._book_ids_for_shelves(shelf_ids)

        # Buchnamen für schöne Quellenangaben cachen
        book_names: dict[int, str] = {}

        for stub in self._list_all("pages"):
            book_id = stub.get("book_id")
            if allowed_books is not None and book_id not in allowed_books:
                continue

            detail = self._get(f"pages/{stub['id']}")
            markdown = (detail.get("markdown") or "").strip()
            content = markdown if markdown else _strip_html(detail.get("html", ""))
            if not content:
                continue

            if book_id is not None and book_id not in book_names:
                try:
                    book_names[book_id] = self._get(f"books/{book_id}").get("name", "")
                except requests.HTTPError:
                    book_names[book_id] = ""

            yield Page(
                id=detail["id"],
                name=detail.get("name", ""),
                slug=detail.get("slug", ""),
                book_id=book_id,
                book_name=book_names.get(book_id, ""),
                chapter_name=(detail.get("chapter") or {}).get("name", "")
                if isinstance(detail.get("chapter"), dict)
                else "",
                content=content,
                url=f"{self.base_url}/link/{detail['id']}",
                updated_at=detail.get("updated_at", ""),
            )
