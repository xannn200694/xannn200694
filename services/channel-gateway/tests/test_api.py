from fastapi.testclient import TestClient

from app.main import app

client = TestClient(app)


def test_health():
    assert client.get("/health").status_code == 200


def test_telegram_webhook():
    update = {
        "message": {
            "message_id": 10,
            "chat": {"id": 555},
            "from": {"id": 777, "username": "user", "first_name": "Иван"},
            "text": "Здравствуйте",
        }
    }
    r = client.post("/webhooks/telegram", json=update)
    assert r.status_code == 200
    assert r.json()["status"] == "accepted"
    # дубль игнорируется
    assert client.post("/webhooks/telegram", json=update).json()["status"] == "ignored"


def test_whatsapp_verify():
    r = client.get(
        "/webhooks/whatsapp",
        params={"hub.verify_token": "verify_me", "hub.challenge": "12345"},
    )
    assert r.status_code == 200
    assert r.text == "12345"


def test_send():
    msg = {
        "message_id": "m1",
        "channel": "telegram",
        "direction": "outbound",
        "conversation_id": "555",
        "contact": {"external_id": "777"},
        "content": {"type": "text", "text": "Привет"},
        "timestamp": "2026-01-01T00:00:00Z",
    }
    r = client.post("/send", json=msg)
    assert r.status_code == 200
    assert r.json()["status"] == "queued"
