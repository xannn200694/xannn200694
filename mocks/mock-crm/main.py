"""Мок CRM Connector (контракт §4). Возвращает фиктивные идентификаторы."""
from fastapi import FastAPI

app = FastAPI(title="mock-crm")


@app.get("/health")
def health():
    return {"status": "ok", "mock": True, "provider": "mock"}


@app.post("/v1/contacts/upsert")
def upsert_contact(body: dict):
    return {"id": "mock-contact-1"}


@app.post("/v1/leads/upsert")
def upsert_lead(body: dict):
    return {"id": "mock-lead-1"}


@app.post("/v1/deals/{deal_id}/stage")
def update_stage(deal_id: str, body: dict):
    return {"status": "ok"}


@app.post("/v1/tasks")
def create_task(body: dict):
    return {"id": "mock-task-1"}


@app.post("/v1/notes")
def add_note(body: dict):
    return {"id": "mock-note-1"}


@app.get("/v1/contacts/by-phone/{phone}")
def by_phone(phone: str):
    return {"crm_contact_id": "mock-contact-1", "phone": phone}
