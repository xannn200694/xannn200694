# Эпик E3 — Каналы: Telegram + WhatsApp

**Цель:** приём и отправка сообщений в WhatsApp и Telegram, нормализация в Canonical Message, мгновенный автоответ 24/7.
**Можно стартовать:** Telegram — сразу; WhatsApp — после получения доступа к Business API.
**Milestones:** M3 (Telegram), M4 (WhatsApp).

## Задачи
1. Channel Gateway (FastAPI) по контракту §1 ([`../03-interfaces.md`](../03-interfaces.md)).
2. **Telegram:** бот через Bot API (webhook), приём сообщений, отправка ответов, медиа.
3. **WhatsApp:** интеграция с выбранным провайдером (Cloud API / 360dialog / Twilio / Wazzup),
   верификация вебхука, шаблоны сообщений (HSM) для исходящих вне 24-часового окна.
4. Нормализация входящих в Canonical Message, идемпотентность по `message_id`, верификация подписи.
5. Маршрутизация: вызов n8n webhook (или очередь) + быстрый автоответ через LLM Gateway.
6. Отправка исходящих `POST /send`, статусы доставки, обработка ошибок/ретраи.
7. Запись событий `message_received` / `message_sent` в `events`.
8. Тесты: симуляция вебхуков провайдеров, контрактные тесты, e2e в Telegram.

## Критерии приёмки
- **M3:** реальный диалог в Telegram, ответ из базы знаний < 5 c, эскалация работает.
- **M4:** реальный диалог в WhatsApp с тем же поведением.

## Зависимости / контракты
- Потребляет: LLM Gateway (E1), n8n (E4), таблицы `messages`/`events`.
- Производит: входящие Canonical Message + `POST /send`.

## Открытые вопросы
- Провайдер WhatsApp и наличие верифицированного бизнес-аккаунта (см. [`../05-open-questions.md`](../05-open-questions.md)).
