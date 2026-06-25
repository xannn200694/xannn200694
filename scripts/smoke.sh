#!/usr/bin/env bash
# Smoke-проверка живого стека (E7): опрос /health всех сервисов.
# Запуск: после `docker compose up`  ->  bash scripts/smoke.sh
set -euo pipefail

declare -A ENDPOINTS=(
  [llm-gateway]="http://localhost:8001/health"
  [rag]="http://localhost:8002/health"
  [channel-gateway]="http://localhost:8003/health"
  [crm-connector]="http://localhost:8004/health"
  [mock-llm]="http://localhost:9001/health"
  [mock-rag]="http://localhost:9002/health"
  [mock-crm]="http://localhost:9003/health"
)

fail=0
for name in "${!ENDPOINTS[@]}"; do
  url="${ENDPOINTS[$name]}"
  code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 5 "$url" || echo "000")
  if [[ "$code" == "200" ]]; then
    echo "OK    $name ($url)"
  else
    echo "FAIL  $name ($url) -> $code"
    fail=1
  fi
done

exit $fail
