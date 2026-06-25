# LLM Gateway (E1)

Единая точка доступа к LLM: управление промптами, тиринг моделей (решение D4), сборка контекста из RAG,
эвристика эскалации, учёт стоимости. Контракт — `docs/03-interfaces.md` §2.

## Запуск (локально)
```bash
pip install -r requirements.txt
uvicorn app.main:app --reload --port 8001
```

## Тесты
```bash
python -m pytest -q
```

## Эндпоинты
- `GET /health`
- `POST /v1/chat`
- `POST /v1/classify`
- `GET /v1/prompts`, `GET /v1/prompts/{id}`

## TODO (эпик E1)
- Реальные адаптеры OpenAI и Anthropic за общим интерфейсом + фолбэк.
- Чтение/версионирование промптов из таблицы `prompts` (PostgreSQL).
- LLM-классификация намерения и lead_score вместо эвристики.
- Запись события `llm_call` (usage/cost) в таблицу `events`.
