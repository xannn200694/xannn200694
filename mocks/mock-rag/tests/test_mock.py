from fastapi.testclient import TestClient

from main import app

client = TestClient(app)


def test_search():
    r = client.post("/v1/search", json={"query": "test"})
    assert r.status_code == 200
    assert r.json()["results"][0]["score"] == 0.99
