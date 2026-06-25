"""RAG Service (E2) — скелет по контракту §3.

Хранилище и поиск реализованы в памяти с наивным лексическим скорингом, чтобы каркас
работал без внешних зависимостей. В эпике E2 заменяется на эмбеддинги + Qdrant/pgvector,
а ингест расширяется парсерами сайта/Word/PDF (решение D5).
"""
from __future__ import annotations

import os
import re
import uuid

from fastapi import FastAPI, HTTPException

from .models import (
    Document,
    IngestRequest,
    IngestResponse,
    SearchRequest,
    SearchResponse,
    SearchResult,
)

app = FastAPI(title="RAG Service", version="0.1.0")

APP_MODE = os.getenv("APP_MODE", "mock")
CHUNK_SIZE = 500  # символов на чанк (грубое чанкование для скелета)

# In-memory индекс: chunk_id -> (text, source, metadata, doc_id)
_INDEX: dict[str, dict] = {}


def _chunk(text: str, size: int = CHUNK_SIZE) -> list[str]:
    text = re.sub(r"\s+", " ", text).strip()
    return [text[i : i + size] for i in range(0, len(text), size)] or [""]


def _tokenize(s: str) -> set[str]:
    return set(re.findall(r"\w+", s.lower()))


def _index_document(doc: Document) -> int:
    # Удаляем прежние чанки документа (идемпотентный ингест).
    for cid in [c for c, v in _INDEX.items() if v["doc_id"] == doc.doc_id]:
        del _INDEX[cid]
    chunks = _chunk(doc.text)
    for ch in chunks:
        cid = str(uuid.uuid4())
        _INDEX[cid] = {
            "text": ch,
            "source": doc.source or doc.title,
            "metadata": {**doc.metadata, "title": doc.title},
            "doc_id": doc.doc_id,
        }
    return len(chunks)


@app.get("/health")
@app.get("/v1/health")
async def health() -> dict[str, str]:
    return {"status": "ok", "mode": APP_MODE, "docs": str(len({v["doc_id"] for v in _INDEX.values()}))}


@app.post("/v1/ingest", response_model=IngestResponse)
async def ingest(req: IngestRequest) -> IngestResponse:
    total_chunks = sum(_index_document(d) for d in req.documents)
    return IngestResponse(ingested=len(req.documents), chunks=total_chunks)


@app.post("/v1/search", response_model=SearchResponse)
async def search(req: SearchRequest) -> SearchResponse:
    q_tokens = _tokenize(req.query)
    scored: list[SearchResult] = []
    for cid, v in _INDEX.items():
        overlap = q_tokens & _tokenize(v["text"])
        if not overlap:
            continue
        score = len(overlap) / (len(q_tokens) or 1)
        scored.append(
            SearchResult(
                chunk_id=cid,
                text=v["text"],
                score=round(score, 4),
                source=v["source"],
                metadata=v["metadata"],
            )
        )
    scored.sort(key=lambda r: r.score, reverse=True)
    return SearchResponse(results=scored[: req.top_k])


@app.delete("/v1/documents/{doc_id}")
async def delete_document(doc_id: str) -> dict[str, int]:
    to_delete = [c for c, v in _INDEX.items() if v["doc_id"] == doc_id]
    if not to_delete:
        raise HTTPException(status_code=404, detail="document not found")
    for cid in to_delete:
        del _INDEX[cid]
    return {"deleted_chunks": len(to_delete)}
