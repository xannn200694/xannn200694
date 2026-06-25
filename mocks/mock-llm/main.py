"""Мок LLM Gateway (контракт §2). Детерминированные ответы для изоляции эпиков."""
from fastapi import FastAPI

app = FastAPI(title="mock-llm")


@app.get("/health")
@app.get("/v1/health")
def health():
    return {"status": "ok", "mock": True}


@app.post("/v1/chat")
def chat(req: dict):
    user = (req.get("variables") or {}).get("user_message", "")
    return {
        "text": f"[mock-llm] Получено: {user}",
        "model_used": "mock-model",
        "finish_reason": "stop",
        "confidence": 0.9,
        "should_escalate": False,
        "usage": {"prompt_tokens": 1, "completion_tokens": 1, "cost_usd": 0.0},
        "trace_id": "mock-trace",
    }


@app.post("/v1/classify")
def classify(req: dict):
    return {"intent": "question", "lead_score": 0.5, "trace_id": "mock-trace"}


@app.get("/v1/prompts")
def prompts():
    return [{"id": "sales_assistant", "name": "sales_assistant", "version": "v1", "body": "mock"}]
