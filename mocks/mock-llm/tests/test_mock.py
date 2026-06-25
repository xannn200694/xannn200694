from fastapi.testclient import TestClient

from main import app

client = TestClient(app)


def test_health():
    assert client.get("/health").json()["mock"] is True


def test_chat():
    r = client.post("/v1/chat", json={"variables": {"user_message": "hi"}})
    assert r.status_code == 200
    assert "text" in r.json()
