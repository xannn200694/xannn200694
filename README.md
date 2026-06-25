# Kivano — AI-платформа автоматизации отдела продаж

ИИ-ассистенты для WhatsApp/Telegram (ответы по базе знаний 24/7, RAG), автоматизация воронки продаж
в n8n, интеграция с CRM (amoCRM/Bitrix24) и сквозная аналитика. LLM (OpenAI/Claude) через единый Gateway.

Проект восстановлен из анализа вакансии «AI Automation Engineer / ИИ-Интегратор (Отдел продаж)» (Kivano)
и спроектирован под параллельную разработку несколькими агентами. План — в [`docs/`](docs/README.md).

## Структура репозитория

```
docs/                  План: анализ, архитектура, контракты, эпики, решения
services/
  llm-gateway/         E1 — единый доступ к LLM, промпты, тиринг моделей
  rag/                 E2 — индексация базы знаний и поиск (RAG)
  channel-gateway/     E3 — WhatsApp/Telegram, нормализация, автоответ
  crm-connector/       E5 — адаптеры amoCRM/Bitrix24
mocks/                 Моки границ (mock-llm/mock-rag/mock-crm)
infra/                 Схема БД (init.sql), reverse proxy (Caddy)
n8n/                   E4 — оркестрация воронки (воркфлоу)
analytics/             E6 — дашборды и витрины (Metabase)
docker-compose.yml     Поднимает весь стек
.env.example           Единый пример переменных окружения
```

## Быстрый старт (M0)

```bash
cp .env.example .env          # заполните секреты при необходимости
docker compose up --build     # поднимет инфраструктуру, сервисы и моки
```

Порты: LLM Gateway `8001`, RAG `8002`, Channel Gateway `8003`, CRM `8004`,
моки `9001–9003`, n8n `5678`, Metabase `3000`, Postgres `5432`, Qdrant `6333`, Caddy `80`.

## Тесты без Docker

```bash
make test     # прогон pytest по всем python-сервисам и мокам
```

## Статус

- ✅ M0 — фундамент: скелет, контракты, моки, схема БД, CI.
- ⏳ E1–E6 — разрабатываются параллельно поверх контрактов (`docs/03-interfaces.md`).

Документация и план: [`docs/README.md`](docs/README.md).
