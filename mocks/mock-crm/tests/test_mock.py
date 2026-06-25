from fastapi.testclient import TestClient

from main import app

client = TestClient(app)


def test_upsert_contact():
    r = client.post("/v1/contacts/upsert", json={"name": "x"})
    assert r.status_code == 200
    assert r.json()["id"] == "mock-contact-1"
