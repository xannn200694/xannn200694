from fastapi.testclient import TestClient

from app.main import app

client = TestClient(app)


def test_health():
    r = client.get("/health")
    assert r.status_code == 200
    assert r.json()["status"] == "ok"


def test_list_prompts():
    r = client.get("/v1/prompts")
    assert r.status_code == 200
    assert any(p["name"] == "sales_assistant" for p in r.json())


def test_chat_basic():
    r = client.post("/v1/chat", json={"variables": {"user_message": "Здравствуйте"}})
    assert r.status_code == 200
    body = r.json()
    assert "text" in body and body["model_used"]
    assert "trace_id" in body


def test_chat_escalation():
    r = client.post("/v1/chat", json={"variables": {"user_message": "хочу менеджера"}})
    assert r.status_code == 200
    assert r.json()["should_escalate"] is True


def test_classify():
    r = client.post("/v1/classify", json={"text": "сколько стоит?"})
    assert r.status_code == 200
    assert r.json()["intent"] == "purchase_intent"
