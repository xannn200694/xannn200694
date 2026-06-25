"""LLM Gateway (E1) — скелет по контракту §2.

Реализует API-поверхность и базовую логику (тиринг моделей, эвристика эскалации,
управление промптами). Реальные вызовы OpenAI/Anthropic и чтение промптов из БД
подключаются в рамках эпика E1 — здесь оставлены явные точки расширения.
"""
from __future__ import annotations

import os
import uuid

import httpx
from fastapi import FastAPI, HTTPException

from .models import (
    ChatRequest,
    ChatResponse,
    ClassifyRequest,
    ClassifyResponse,
    PromptInfo,
    Usage,
)

app = FastAPI(title="LLM Gateway", version="0.1.0")

APP_MODE = os.getenv("APP_MODE", "mock")
MODEL_CHEAP = os.getenv("LLM_MODEL_CHEAP", "gpt-4o-mini")
MODEL_STRONG = os.getenv("LLM_MODEL_STRONG", "gpt-4o")
RAG_URL = os.getenv("RAG_URL", "http://mock-rag:8000")
RAG_TOP_K = int(os.getenv("RAG_TOP_K", "5"))

# Встроенный стартовый набор промптов (в проде — таблица prompts в PostgreSQL).
_PROMPTS: dict[str, PromptInfo] = {
    "sales_assistant": PromptInfo(
        id="sales_assistant",
        name="sales_assistant",
        version="v1",
        body=(
            "Ты — вежливый ассистент отдела продаж Kivano. Отвечай кратко на языке клиента. "
            "Используй только контекст из базы знаний; если ответа нет — предложи менеджера."
        ),
        model_defaults={"temperature": 0.2, "tier": "cheap"},
    )
}

ESCALATION_KEYWORDS = ("менеджер", "человек", "оператор", "жалоба", "manager", "human")


def select_model(requested: str, tier_hint: str = "cheap") -> str:
    """Маршрутизация по тирам (решение D4)."""
    if requested and requested != "auto":
        return requested
    return MODEL_STRONG if tier_hint == "strong" else MODEL_CHEAP


async def fetch_kb_context(query: str) -> str:
    """Сборка контекста из RAG. В mock-режиме допускаем отсутствие RAG."""
    if not query:
        return ""
    try:
        async with httpx.AsyncClient(timeout=5.0) as client:
            resp = await client.post(
                f"{RAG_URL}/v1/search", json={"query": query, "top_k": RAG_TOP_K}
            )
            resp.raise_for_status()
            results = resp.json().get("results", [])
            return "\n\n".join(r.get("text", "") for r in results)
    except Exception:
        return ""


@app.get("/health")
@app.get("/v1/health")
async def health() -> dict[str, str]:
    return {"status": "ok", "mode": APP_MODE}


@app.get("/v1/prompts")
async def list_prompts() -> list[PromptInfo]:
    return list(_PROMPTS.values())


@app.get("/v1/prompts/{prompt_id}")
async def get_prompt(prompt_id: str) -> PromptInfo:
    if prompt_id not in _PROMPTS:
        raise HTTPException(status_code=404, detail="prompt not found")
    return _PROMPTS[prompt_id]


@app.post("/v1/chat", response_model=ChatResponse)
async def chat(req: ChatRequest) -> ChatResponse:
    if req.prompt_id not in _PROMPTS:
        raise HTTPException(status_code=404, detail="unknown prompt_id")

    user_message = str(req.variables.get("user_message", "")).strip()
    should_escalate = any(k in user_message.lower() for k in ESCALATION_KEYWORDS)

    kb_context = req.variables.get("kb_context") or await fetch_kb_context(user_message)
    model_used = select_model(req.model)

    # TODO(E1): реальный вызов провайдера LLM (OpenAI/Anthropic) с промптом и контекстом.
    if kb_context:
        text = f"(черновой ответ по базе знаний) {kb_context[:280]}"
        confidence = 0.8
    else:
        text = (
            "Спасибо за обращение! Сейчас уточню информацию и вернусь с ответом."
            if not should_escalate
            else "Передаю ваш вопрос менеджеру — он скоро свяжется с вами."
        )
        confidence = 0.4

    return ChatResponse(
        text=text,
        model_used=model_used,
        confidence=confidence,
        should_escalate=should_escalate or confidence < 0.5,
        usage=Usage(prompt_tokens=len(user_message.split()), completion_tokens=len(text.split())),
        trace_id=str(uuid.uuid4()),
    )


@app.post("/v1/classify", response_model=ClassifyResponse)
async def classify(req: ClassifyRequest) -> ClassifyResponse:
    text = req.text.lower()
    # TODO(E1): заменить эвристику на LLM-классификацию намерения и lead_score.
    if any(w in text for w in ("купить", "цена", "стоит", "заказать", "buy", "price")):
        intent, score = "purchase_intent", 0.8
    elif any(w in text for w in ("привет", "здравствуйте", "hello", "hi")):
        intent, score = "greeting", 0.2
    else:
        intent, score = "question", 0.5
    return ClassifyResponse(intent=intent, lead_score=score, trace_id=str(uuid.uuid4()))
