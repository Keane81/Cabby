# Контракт командной строки: `emulator`

**Feature**: [spec.md](../spec.md) | **Plan**: [plan.md](../plan.md)

Эмулятор не добавляет публичных операций в gateway: ниже — контракт самого инструмента (параметры, коды выхода, форматы вывода). Операции gateway, которые он вызывает, описаны в [`backend/cabby-gateway/api/openapi.yaml`](../../../backend/cabby-gateway/api/openapi.yaml) (версия 1.2.0): `registerCabber`, `createCabberSession`, `recordCabberLocation`, `deleteCabberSession`.

## Запуск

```bash
make emulate ARGS="-cabbers 1000 -interval 5s -duration 10m"
# то же напрямую
cd backend/emulator && go run ./cmd/emulator -cabbers 1000 -interval 5s -duration 10m
```

`make emulate` подставляет `-target` из `host` окружения `LOCAL` Bruno-коллекции (как `make smoke`), если `-target` не указан в `ARGS`.

## Параметры

| Флаг | Значение | По умолчанию | Описание |
|---|---|---|---|
| `-cabbers` | целое | 100 | число кабберов |
| `-interval` | длительность | 5s | интервал между отправками одного каббера |
| `-duration` | длительность | 0 | длительность прогона; 0 — до сигнала |
| `-ramp-up` | длительность | `max(10s, cabbers×25ms)` | период, на который растягивается вход |
| `-area` | `lat1,lon1,lat2,lon2` | `55.60,37.40,55.90,37.85` | область движения |
| `-target` | URL | `http://127.0.0.1:8080` | базовый адрес gateway |
| `-allow-remote` | флаг | — | разрешить не-loopback цель |
| `-seed` | целое | случайное | зерно |
| `-run-id` | строка | случайная | идентификатор прогона (в email) |
| `-max-conns` | целое | 512 | потолок соединений к gateway |
| `-instances`, `-instance` | целые | 1, 0 | разбиение парка между процессами |
| `-report` | путь | `emulator-report-<run-id>.json` | файл итоговой сводки |

Справка (`-h`) выводит параметры, значения по умолчанию и пример (FR-017).

## Коды выхода

| Код | Значение |
|---|---|
| 0 | прогон завершён (по длительности или сигналу), сводка записана |
| 1 | цель недоступна при старте или ни один каббер не запустился |
| 2 | недопустимые параметры (до первого запроса) |
| 3 | прогон завершён, но самоконтроль показал недобор нагрузки (`generator_saturated`) |

## Вывод

**Старт** (stderr, zerolog JSON): `"emulator starting"` с полями профиля без секретов, `run_id`, `seed`; затем `"estimated database growth"` (строк и ГБ в час), а при приросте больше 10 ГБ — предупреждение `"database growth above 10 GB"`. Журнал и живая строка идут в stderr (отступление от сервисов, где логгер пишет в stdout: stdout здесь занят сводкой и должен оставаться пригодным для конвейера). В живой строке задержки — накопленные с начала прогона, частота и доля ошибок — за последние 5 секунд, виды ошибок перечисляются только ненулевые. **Каждые 5 секунд** (stderr):

```text
t=00:02:05 phase=steady active=1000/1000 sent=198.4/s target=200.0/s err=0.1% (unavailable=1) p50=8ms p95=21ms p99=40ms
```

**Завершение**: `"emulator stopped"` или `"emulator stopped with error"`; итоговая сводка — человекочитаемая в stdout и JSON в файле. В журналах нет паролей, токенов, email и координат.

## Сводка (JSON)

```json
{
  "run_id": "k3x9a2bd", "seed": 42,
  "profile": {"cabbers": 1000, "interval_ms": 5000, "duration_ms": 600000, "ramp_up_ms": 25000, "target": "http://127.0.0.1:8080", "instances": 1, "instance": 0},
  "started_at": "2026-10-03T10:00:00Z", "finished_at": "2026-10-03T10:10:30Z",
  "fleet": {"started": 1000, "failed": 0, "lost": 0, "time_to_full_ms": 27100, "on_schedule_ratio": 0.98},
  "sent": {"ok": 118000, "failed": {"unavailable": 3, "timeout": 0, "network": 0, "unauthorized": 0, "invalid_request": 0, "unexpected": 0}, "skipped": 12},
  "rate": {"target_per_s": 200.0, "actual_per_s": 197.6},
  "latency_ms": {"p50": 8, "p95": 21, "p99": 40, "max": 380},
  "dispatch_lag_ms": {"p50": 0, "p95": 1, "p99": 2, "max": 9},
  "shutdown": {"logged_out": 1000, "not_logged_out": 0, "took_ms": 1900},
  "estimate": {"rows": 118000, "gb": 0.02},
  "verdict": "ok"
}
```

`estimate.rows` — число принятых отправок (по одной строке в `location` на каждую); размер — оценка по 175 байт на строку. `rate.actual_per_s` считается по устойчивому состоянию, то есть после того, как парк стал полным; если парк полным не стал, — по всему прогону. Поля только добавляются; потребители игнорируют незнакомые ключи (чтобы сводки разных версий сравнивались).
