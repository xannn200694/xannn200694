-- ============================================================================
-- 00_apply.sql — применяет все витрины KPI эпика E6 по порядку.
-- ----------------------------------------------------------------------------
-- Запуск (из docker-compose окружения):
--   docker compose exec -T postgres \
--     psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
--     -f /path/in/container/00_apply.sql
--
-- Или локально, скопировав каталог sql/ на хост с psql:
--   psql "$DATABASE_URL" -f analytics/sql/00_apply.sql
--
-- \ir подключает файлы относительно расположения ЭТОГО скрипта,
-- поэтому запускать можно из любого рабочего каталога.
-- Все витрины создаются как CREATE OR REPLACE VIEW — повторный запуск безопасен.
-- ============================================================================

\echo 'Applying E6 analytics views...'

\ir ttfr.sql
\ir lead_capture.sql
\ir automation_rate.sql
\ir funnel.sql
\ir response_volume.sql

\echo 'Done. Views: kpi_ttfr_*, kpi_lead_capture_*, kpi_automation_*, kpi_funnel*, kpi_response_volume_*'
