"""CRM Connector (E5) — скелет по контракту §4.

Единый API поверх адаптеров amoCRM/Bitrix24 (решение D1).
"""
from __future__ import annotations

import os

from fastapi import FastAPI, HTTPException, Request

from .adapters import get_adapter
from .models import (
    ContactIn,
    ContactOut,
    DealStageIn,
    IdResponse,
    LeadIn,
    NoteIn,
    TaskIn,
)

app = FastAPI(title="CRM Connector", version="0.1.0")
adapter = get_adapter()


@app.get("/health")
async def health() -> dict[str, str]:
    return {"status": "ok", "mode": os.getenv("APP_MODE", "mock"), "provider": adapter.name}


@app.post("/v1/contacts/upsert", response_model=IdResponse)
async def upsert_contact(contact: ContactIn) -> IdResponse:
    return IdResponse(id=adapter.upsert_contact(contact))


@app.post("/v1/leads/upsert", response_model=IdResponse)
async def upsert_lead(lead: LeadIn) -> IdResponse:
    return IdResponse(id=adapter.upsert_lead(lead))


@app.post("/v1/deals/{deal_id}/stage")
async def update_deal_stage(deal_id: str, body: DealStageIn) -> dict[str, str]:
    adapter.update_deal_stage(deal_id, body.stage)
    return {"status": "ok"}


@app.post("/v1/tasks", response_model=IdResponse)
async def create_task(task: TaskIn) -> IdResponse:
    return IdResponse(id=adapter.create_task(task))


@app.post("/v1/notes", response_model=IdResponse)
async def add_note(note: NoteIn) -> IdResponse:
    return IdResponse(id=adapter.add_note(note))


@app.get("/v1/contacts/by-phone/{phone}", response_model=ContactOut)
async def get_contact_by_phone(phone: str) -> ContactOut:
    contact = adapter.get_contact_by_phone(phone)
    if not contact:
        raise HTTPException(status_code=404, detail="contact not found")
    return contact


@app.post("/webhooks/crm")
async def crm_webhook(request: Request) -> dict[str, str]:
    # TODO(E5): обработка входящих изменений из CRM и проброс в n8n.
    _ = await request.body()
    return {"status": "accepted"}
