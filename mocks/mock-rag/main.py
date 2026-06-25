"""Мок RAG Service (контракт §3). Возвращает фиксированный фрагмент знаний."""
from fastapi import FastAPI

app = FastAPI(title="mock-rag")


@app.get("/health")
@app.get("/v1/health")
def health():
    return {"status": "ok", "mock": True}


@app.post("/v1/ingest")
def ingest(req: dict):
    docs = req.get("documents", [])
    return {"ingested": len(docs), "chunks": len(docs)}


@app.post("/v1/search")
def search(req: dict):
    query = req.get("query", "")
    return {
        "results": [
            {
                "chunk_id": "mock-chunk-1",
                "text": f"[mock-rag] Релевантный фрагмент для запроса: {query}",
                "score": 0.99,
                "source": "mock-kb",
                "metadata": {"title": "mock"},
            }
        ]
    }
