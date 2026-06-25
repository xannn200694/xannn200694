"""Canonical Message (контракт §0) и схема исходящей отправки."""
from __future__ import annotations

from typing import Any

from pydantic import BaseModel, Field


class Contact(BaseModel):
    external_id: str
    phone: str | None = None
    username: str | None = None
    display_name: str | None = None


class Content(BaseModel):
    type: str = "text"
    text: str | None = None
    media_url: str | None = None
    payload: dict[str, Any] = Field(default_factory=dict)


class CanonicalMessage(BaseModel):
    message_id: str
    channel: str  # whatsapp | telegram
    direction: str = "inbound"  # inbound | outbound
    conversation_id: str
    contact: Contact
    content: Content
    timestamp: str
    metadata: dict[str, Any] = Field(default_factory=dict)


class SendResult(BaseModel):
    status: str  # queued | sent | failed
    provider_message_id: str | None = None
    error: str | None = None
