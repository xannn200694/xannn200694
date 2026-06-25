from fastapi.testclient import TestClient

from app.main import app

client = TestClient(app)


def test_health():
    r = client.get("/health")
    assert r.status_code == 200
    assert r.json()["provider"] in {"memory", "amocrm", "bitrix24"}


def test_contact_upsert_and_lookup():
    payload = {"name": "Иван", "phone": "+996700000000"}
    r = client.post("/v1/contacts/upsert", json=payload)
    assert r.status_code == 200
    cid = r.json()["id"]
    # повторный upsert по тому же телефону возвращает тот же id
    assert client.post("/v1/contacts/upsert", json=payload).json()["id"] == cid

    found = client.get("/v1/contacts/by-phone/+996700000000")
    assert found.status_code == 200
    assert found.json()["crm_contact_id"] == cid


def test_lead_and_task():
    lead = {"contact": {"name": "Лид", "phone": "+996700000001"}, "title": "Заявка"}
    assert client.post("/v1/leads/upsert", json=lead).status_code == 200
    assert client.post("/v1/tasks", json={"text": "Перезвонить"}).status_code == 200


def test_crm_webhook():
    assert client.post("/webhooks/crm", json={"event": "deal.update"}).status_code == 200
