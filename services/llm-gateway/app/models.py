"""Pydantic-схемы по контракту §2 (docs/03-interfaces.md)."""
from __future__ import annotations

from typing import Any

from pydantic import BaseModel, ConfigDict, Field


class _Base(BaseModel):
    model_config = ConfigDict(protected_namespaces=())


class ChatRequest(_Base):
    prompt_id: str = "sales_assistant"
    prompt_version: str = "latest"
    variables: dict[str, Any] = Field(default_factory=dict)
    model: str = "auto"
    conversation_id: str | None = None
    temperature: float = 0.2
    max_tokens: int = 1024


class Usage(BaseModel):
    prompt_tokens: int = 0
    completion_tokens: int = 0
    cost_usd: float = 0.0


class ChatResponse(_Base):
    text: str
    model_used: str
    finish_reason: str = "stop"
    confidence: float = 1.0
    should_escalate: bool = False
    usage: Usage = Field(default_factory=Usage)
    trace_id: str


class ClassifyRequest(BaseModel):
    text: str
    conversation_id: str | None = None


class ClassifyResponse(BaseModel):
    intent: str
    lead_score: float
    trace_id: str


class PromptInfo(_Base):
    id: str
    name: str
    version: str
    body: str
    model_defaults: dict[str, Any] = Field(default_factory=dict)
