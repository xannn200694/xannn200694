"""Channel Gateway (E3) — скелет по контракту §1.

Приём вебхуков Telegram/WhatsApp, нормализация в Canonical Message, идемпотентность,
быстрый автоответ через LLM Gateway. WhatsApp реализован через абстракцию провайдера
с двумя режимами (cloud_api | web_bridge, решение D2). Реальная отправка и верификация
подписей подключаются в эпике E3.
"""
from __future__ import annotations

import datetime as dt
import os
import uuid

import httpx
from fastapi import FastAPI, Request, Response

from .models import CanonicalMessage, Contact, Content, SendResult

app = FastAPI(title="Channel Gateway", version="0.1.0")

APP_MODE = os.getenv("APP_MODE", "mock")
LLM_GATEWAY_URL = os.getenv("LLM_GATEWAY_URL", "http://mock-llm:8000")
WA_MODE = os.getenv("WA_MODE", "web_bridge")
WA_VERIFY_TOKEN = os.getenv("WA_CLOUD_VERIFY_TOKEN", "verify_me")

# Простейшая защита от дублей вебхуков (в проде — Redis/таблица).
_SEEN_MESSAGE_IDS: set[str] = set()


def _now() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat()


def _is_duplicate(message_id: str) -> bool:
    if message_id in _SEEN_MESSAGE_IDS:
        return True
    _SEEN_MESSAGE_IDS.add(message_id)
    return False


async def autoreply(msg: CanonicalMessage) -> str | None:
    """Быстрый автоответ через LLM Gateway (best-effort)."""
    try:
        async with httpx.AsyncClient(timeout=8.0) as client:
            resp = await client.post(
                f"{LLM_GATEWAY_URL}/v1/chat",
                json={
                    "conversation_id": msg.conversation_id,
                    "variables": {"user_message": msg.content.text or ""},
                },
            )
            resp.raise_for_status()
            return resp.json().get("text")
    except Exception:
        return None


def normalize_telegram(update: dict) -> CanonicalMessage | None:
    message = update.get("message") or update.get("edited_message")
    if not message:
        return None
    chat = message.get("chat", {})
    frm = message.get("from", {})
    return CanonicalMessage(
        message_id=f"tg-{message.get('message_id')}-{chat.get('id')}",
        channel="telegram",
        conversation_id=str(chat.get("id")),
        contact=Contact(
            external_id=str(frm.get("id")),
            username=frm.get("username"),
            display_name=frm.get("first_name"),
        ),
        content=Content(type="text", text=message.get("text")),
        timestamp=_now(),
    )


def normalize_whatsapp(body: dict) -> CanonicalMessage | None:
    """Нормализация payload WhatsApp Cloud API (упрощённо)."""
    try:
        value = body["entry"][0]["changes"][0]["value"]
        msg = value["messages"][0]
        return CanonicalMessage(
            message_id=f"wa-{msg['id']}",
            channel="whatsapp",
            conversation_id=msg["from"],
            contact=Contact(external_id=msg["from"], phone=msg["from"]),
            content=Content(type="text", text=msg.get("text", {}).get("body")),
            timestamp=_now(),
        )
    except (KeyError, IndexError):
        return None


@app.get("/health")
async def health() -> dict[str, str]:
    return {"status": "ok", "mode": APP_MODE, "wa_mode": WA_MODE}


@app.post("/webhooks/telegram")
async def telegram_webhook(request: Request) -> dict:
    update = await request.json()
    msg = normalize_telegram(update)
    if not msg or _is_duplicate(msg.message_id):
        return {"status": "ignored"}
    reply = await autoreply(msg)
    # TODO(E3): отправить reply в Telegram + переслать msg в n8n + записать events.
    return {"status": "accepted", "conversation_id": msg.conversation_id, "reply": reply}


@app.get("/webhooks/whatsapp")
async def whatsapp_verify(request: Request) -> Response:
    """Verification challenge для WhatsApp Cloud API."""
    params = request.query_params
    if params.get("hub.verify_token") == WA_VERIFY_TOKEN:
        return Response(content=params.get("hub.challenge", ""), media_type="text/plain")
    return Response(status_code=403)


@app.post("/webhooks/whatsapp")
async def whatsapp_webhook(request: Request) -> dict:
    body = await request.json()
    msg = normalize_whatsapp(body)
    if not msg or _is_duplicate(msg.message_id):
        return {"status": "ignored"}
    reply = await autoreply(msg)
    return {"status": "accepted", "conversation_id": msg.conversation_id, "reply": reply}


@app.post("/send", response_model=SendResult)
async def send(msg: CanonicalMessage) -> SendResult:
    # TODO(E3): реальная отправка через Telegram Bot API или WhatsApp-провайдера (WA_MODE).
    return SendResult(status="queued", provider_message_id=str(uuid.uuid4()))
