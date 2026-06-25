"""Схемы CRM-коннектора по контракту §4 (docs/03-interfaces.md)."""
from __future__ import annotations

from typing import Any

from pydantic import BaseModel, Field


class ContactIn(BaseModel):
    external_id: str | None = None
    name: str | None = None
    phone: str | None = None
    username: str | None = None
    metadata: dict[str, Any] = Field(default_factory=dict)


class LeadIn(BaseModel):
    contact: ContactIn
    title: str = "Лид из чата"
    source: str = ""
    metadata: dict[str, Any] = Field(default_factory=dict)


class DealStageIn(BaseModel):
    stage: str


class TaskIn(BaseModel):
    assignee: str | None = None
    due_at: str | None = None
    text: str
    related_id: str | None = None


class NoteIn(BaseModel):
    entity_id: str
    text: str


class IdResponse(BaseModel):
    id: str


class ContactOut(BaseModel):
    crm_contact_id: str
    name: str | None = None
    phone: str | None = None
