# RAG Service (E2)

Индексация базы знаний и семантический поиск. Контракт — `docs/03-interfaces.md` §3.

Реализация на **Go 1.26** (только стандартная библиотека, без внешних зависимостей).

## Архитектура

- **Embedder** (`embedder.go`):
  - `MockEmbedder` (APP_MODE=mock) — детерминированные векторы из хеша токенов, работает оффлайн.
  - `OpenAIEmbedder` (APP_MODE=real) — OpenAI-совместимый `POST /embeddings` (`OPENAI_BASE_URL` переопределяем).
- **Store** (`store.go`):
  - `MemoryStore` (mock / фолбэк) — in-memory индекс с лексическим скорингом.
  - `QdrantStore` (real) — Qdrant по HTTP REST: создание коллекции, upsert точек, векторный поиск.
- **Парсеры источников** (`parsers.go`, решение D5): веб-сайт, Word (.docx), PDF.

Режим выбирается переменной `APP_MODE` (`mock` по умолчанию).

## Запуск

```bash
go run .
go vet ./... && go test ./...
```

## Эндпоинты

- `POST /v1/ingest` — `{documents:[{doc_id,title,text,source,metadata}]}` → `{ingested,chunks}`
- `POST /v1/search` — `{query,top_k,filters}` → `{results:[{chunk_id,text,score,source,metadata}]}`
- `DELETE /v1/documents/{doc_id}`
- `GET /v1/health`
- `POST /v1/ingest/web` — `{urls:[...]}`, краулинг страниц, очистка HTML, индексация.
- `POST /v1/ingest/docx` — `multipart/form-data` поле `file` (.docx).
- `POST /v1/ingest/pdf` — `multipart/form-data` поле `file` (PDF, best-effort).

## Переменные окружения

| Переменная | Значение по умолчанию | Назначение |
|---|---|---|
| `APP_MODE` | `mock` | `mock` (оффлайн) / `real` (Qdrant + OpenAI) |
| `PORT` | `8000` | порт HTTP |
| `QDRANT_URL` | `http://qdrant:6333` | адрес Qdrant (real) |
| `RAG_COLLECTION` | `kivano_kb` | имя коллекции Qdrant |
| `RAG_TOP_K` | `5` | top_k по умолчанию (контракт) |
| `OPENAI_BASE_URL` | `https://api.openai.com/v1` | база для эмбеддингов (переопределяемая) |
| `OPENAI_API_KEY` | — | ключ OpenAI (real) |
| `EMBEDDINGS_MODEL` | `text-embedding-3-small` | модель эмбеддингов |

## Метаданные документа

Каждому чанку проставляются: `source`, `title`, `lang` (best-effort ru/en), `updated_at` (RFC3339),
плюс пользовательские поля из `metadata` и `source_type`/`url`/`filename` для парсеров.

## Оценочный набор (recall@k)

`eval/questions.json` — небольшая база знаний + вопросы (ожидаемый источник/ключевые слова).
Тест `TestRecallAtK` индексирует набор и проверяет `recall@3 ≥ 0.8` на MockEmbedder/MemoryStore.

## Ограничения

- **PDF** — извлечение текста best-effort на чистом stdlib: поддержаны потоки `FlateDecode`
  (через `compress/zlib`) и операторы `Tj`/`TJ` (текст в скобках). Сканы, шрифты с кастомной
  кодировкой/CID без `ToUnicode`, а также позиционирование текста не обрабатываются —
  для таких файлов возвращается `422`. Для полноценного PDF/OCR нужен внешний инструмент.
- **robots.txt** для веб-краулера пока не учитывается (планируется в рамках D5).
