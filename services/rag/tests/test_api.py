from fastapi.testclient import TestClient

from app.main import app

client = TestClient(app)


def test_health():
    assert client.get("/v1/health").status_code == 200


def test_ingest_and_search():
    docs = {
        "documents": [
            {
                "doc_id": "d1",
                "title": "Доставка",
                "text": "Доставка по Бишкеку занимает один день. Оплата при получении.",
                "source": "site",
            }
        ]
    }
    r = client.post("/v1/ingest", json=docs)
    assert r.status_code == 200
    assert r.json()["ingested"] == 1

    s = client.post("/v1/search", json={"query": "сколько идёт доставка", "top_k": 3})
    assert s.status_code == 200
    results = s.json()["results"]
    assert results and "доставка" in results[0]["text"].lower()


def test_delete():
    client.post(
        "/v1/ingest",
        json={"documents": [{"doc_id": "d2", "title": "t", "text": "тестовый документ"}]},
    )
    r = client.delete("/v1/documents/d2")
    assert r.status_code == 200
    assert r.json()["deleted_chunks"] >= 1
    assert client.delete("/v1/documents/d2").status_code == 404
