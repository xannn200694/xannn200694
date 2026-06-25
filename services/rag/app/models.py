"""Pydantic-схемы по контракту §3 (docs/03-interfaces.md)."""
from __future__ import annotations

from typing import Any

from pydantic import BaseModel, Field


class Document(BaseModel):
    doc_id: str
    title: str = ""
    text: str
    source: str = ""
    metadata: dict[str, Any] = Field(default_factory=dict)


class IngestRequest(BaseModel):
    documents: list[Document]


class IngestResponse(BaseModel):
    ingested: int
    chunks: int


class SearchRequest(BaseModel):
    query: str
    top_k: int = 5
    filters: dict[str, Any] = Field(default_factory=dict)


class SearchResult(BaseModel):
    chunk_id: str
    text: str
    score: float
    source: str = ""
    metadata: dict[str, Any] = Field(default_factory=dict)


class SearchResponse(BaseModel):
    results: list[SearchResult]
