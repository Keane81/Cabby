# Quickstart: проверка внешнего контракта cabby-gateway

Руководство проверки после реализации [плана](plan.md). Контракты: [openapi.yaml](contracts/openapi.yaml) и [руководство](contracts/external-api.md); модель — [data-model.md](data-model.md). Здесь описаны проверяемые сценарии, а не код реализации.

## Предусловия

- Go 1.26.1 для локального запуска и тестов.
- `backend/cabby-gateway/.env` (скопировать из `.env.example`); локальный пример порта — 8082.
- `curl` для ручных проверок.
- Docker Compose — только если нужен полный стек мониторинга из спецификации 001. Внешний OpenAPI-линтер не требуется: структура, паритет и версия контракта проверяются Go-тестами (без npm-зависимостей).

## Подготовка

```sh
cp backend/cabby-gateway/.env.example backend/cabby-gateway/.env
```

## 1. Валидация машиночитаемого контракта

Канонический документ `backend/cabby-gateway/api/openapi.yaml` проверяется Go-тестами (без внешних npm-инструментов):

```sh
cd backend/cabby-gateway
make test
```

Ожидается: тесты подтверждают, что `info.version` равен `1.0.0` (валидный SemVer); описаны пути `/healthz` (GET) и `/openapi.yaml` (GET); путь `/metrics` отсутствует; канонический файл байт-в-байт совпадает с проектным эталоном `specs/002-gateway-external-contract/contracts/openapi.yaml`; каждый путь роутера определён в контракте.

## 2. Запуск сервиса

```sh
cd backend/cabby-gateway
make run
```

Сервис слушает публичный порт из `.env` (пример 8082) и внутренний порт метрик 9091. Далее команды используют `http://127.0.0.1:8082`; при другом порте замените его.

## 3. Discovery и версия контракта (US1, US3, FR-007, FR-011)

```sh
curl -i http://127.0.0.1:8082/openapi.yaml
```

Ожидается: HTTP 200, `Content-Type: text/yaml; charset=utf-8`, тело — OpenAPI-документ с `info.version: 1.0.0`, перечисляющий `/healthz` как единственную бизнес-операцию. Клиент может определить версию контракта из полученного документа.

## 4. Health-check соответствует контракту (US1, FR-003, FR-004)

```sh
curl -i http://127.0.0.1:8082/healthz
```

Ожидается: HTTP 200, `Content-Type: application/json`, тело `{"status":"ok"}` — соответствует схеме `HealthStatus` из контракта.

## 5. Единая модель ошибок (US2, FR-005, FR-012)

```sh
curl -i -X POST http://127.0.0.1:8082/healthz   # неверный метод
curl -i -I http://127.0.0.1:8082/healthz          # HEAD
curl -i http://127.0.0.1:8082/unknown             # неописанный путь
```

Ожидается:

- POST и HEAD на `/healthz` → HTTP 405, JSON-конверт `{"error":{"code":"method_not_allowed","message":"..."}}`;
- `/unknown` → HTTP 404, JSON-конверт `{"error":{"code":"unknown_operation","message":"..."}}`;
- тела ошибок не содержат секретов, внутренних сведений или персональных данных.

## 6. Метрики не входят во внешний контракт (FR-010)

```sh
curl -i http://127.0.0.1:8082/metrics   # публичный порт
```

Ожидается: HTTP 404 с `code=unknown_operation` на публичном порту. Метрики доступны только на внутреннем порту 9091 (`http://127.0.0.1:9091/metrics`) и не публикуются внешним клиентам.

## 7. Неготовность (503) — только через тест обработчика

Публичной команды переключения готовности нет (как в спецификации 001). Ветка 503 `{"status":"unavailable"}` воспроизводится тестом обработчика с подменённым внутренним состоянием готовности:

```sh
cd backend/cabby-gateway
make test
```

Ожидается: тест подтверждает 503 с `status=unavailable` и соответствие единой модели ошибок для 404/405.

## 8. Parity документа и реализации (SC-005)

Тест parity подтверждает, что документ, отданный по `GET /openapi.yaml`, байт-в-байт совпадает с закоммиченным каноническим файлом `backend/cabby-gateway/api/openapi.yaml`. Это исключает дрейф между машиночитаемым контрактом и сервисом.

```sh
cd backend/cabby-gateway
make test
```

## 9. Проверки проекта

```sh
cd backend/cabby-gateway
make test
make test-race
make vet
```

Все проверки должны проходить без ошибок.

## Сводка ожидаемых результатов

| Сценарий | Запрос | Ожидание |
|---|---|---|
| Discovery | `GET /openapi.yaml` | 200, text/yaml, `info.version=1.0.0`, перечислен `/healthz` |
| Health-check | `GET /healthz` | 200, `{"status":"ok"}` |
| Неверный метод | `POST`/`HEAD /healthz` | 405, Error `method_not_allowed` |
| Неописанный путь | `GET /unknown` | 404, Error `unknown_operation` |
| Метрики на публичном порту | `GET /metrics` | 404, Error `unknown_operation` |
| Неготовность | тест обработчика | 503, `{"status":"unavailable"}` |
| Parity | `make test` | отданный документ == канонический файл |
