---
description: "Task list for feature implementation"
---

# Tasks: Опубликованный внешний контракт cabby-gateway

**Input**: Design documents from `specs/002-gateway-external-contract/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/ (openapi.yaml, external-api.md), quickstart.md

**Tests**: Включены. Конституция Cabby (принцип V) требует проверяемых критериев приёмки и покрытия критичных сценариев тестами; существующий сервис уже имеет `health_test.go` и `metrics_test.go`. Внутри каждой истории тесты пишутся до реализации и должны упасть до неё.

**Organization**: Задачи сгруппированы по пользовательским историям из spec.md (US1 P1, US2 P2, US3 P3). Каждая история независимо реализуема и проверяема.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: можно выполнять параллельно (разные файлы, нет незавершённых зависимостей)
- **[Story]**: US1 / US2 / US3
- Пути указаны относительно корня репозитория; код сервиса — в `backend/cabby-gateway/`

## Path Conventions

- Backend-сервис: `backend/cabby-gateway/` (Go 1.26.1, модуль `github.com/Keane81/Cabby/backend/cabby-gateway`)
- Канонический контракт: `backend/cabby-gateway/api/openapi.yaml`
- Публичный обработчик: `backend/cabby-gateway/internal/server/`
- Точки входа: `backend/cabby-gateway/cmd/cabby-gateway/main.go`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Материализовать канонический контракт и подготовить dev-инструмент проверки.

- [X] T001 [P] Создать канонический файл контракта `backend/cabby-gateway/api/openapi.yaml`, идентичный проектному эталону `specs/002-gateway-external-contract/contracts/openapi.yaml` (OpenAPI 3.1, `info.version: 1.0.0`, пути `/healthz` и `/openapi.yaml`, схемы `HealthStatus`, `Error`, `ErrorCode`).
- [X] T002 [P] Обеспечить валидацию `backend/cabby-gateway/api/openapi.yaml` силами Go-тестов (структура, parity с эталоном, SemVer-версия, отсутствие `/metrics`) без внешних npm-инструментов. Внешний OpenAPI-линтер (`npx @stoplightio/spectral-cli`) не подключается: он тянет зависимость от недоступного реестра `artifactory.tutu.ru`.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Встраивание контракта и расширяемый маршрутизатор публичного порта — основа для US1 и US2.

**⚠️ CRITICAL**: US1 и US2 не начинаются до завершения этой фазы.

- [X] T003 [P] Создать `backend/cabby-gateway/api/contract.go` (package `api`) с директивой `//go:embed openapi.yaml`, экспортирующей `OpenAPIDocument []byte` (go:embed требует, чтобы файл находился в каталоге пакета; зависит от T001).
- [X] T004 [P] Выполнить рефакторинг публичной обработки запросов: добавить `backend/cabby-gateway/internal/server/router.go` с явным маршрутизатором по пути/методу, сохраняющим текущее поведение `/healthz` (200 `{"status":"ok"}` / 503 `{"status":"unavailable"}`, учёт метрик, явный запрет HEAD) и текущие plain-text 404/405; `NewPublicHandler` в `backend/cabby-gateway/internal/server/health.go` делегирует маршрутизатору. Сборка `cmd/cabby-gateway/main.go` остаётся без изменений.

**Checkpoint**: Основа готова — можно реализовывать пользовательские истории.

---

## Phase 3: User Story 1 - Интеграция по опубликованному контракту (Priority: P1) 🎯 MVP

**Goal**: Внешний клиент получает опубликованный контракт и вызывает health-check строго по нему; контракт обнаруживается в рантайме.

**Independent Test**: `make run`; `curl -i http://127.0.0.1:8082/openapi.yaml` → 200, `text/yaml`, `info.version: 1.0.0`, перечислен `/healthz`; `curl -i http://127.0.0.1:8082/healthz` → 200 `{"status":"ok"}`; parity-тест подтверждает совпадение отданного документа с каноническим файлом.

### Tests for User Story 1 ⚠️ (писать первыми, должны упасть до реализации)

- [X] T005 [P] [US1] Написать тесты discovery и parity в `backend/cabby-gateway/internal/server/contract_test.go`: GET `/openapi.yaml` → 200 и `Content-Type: text/yaml; charset=utf-8`; тело байт-в-байт равно `api.OpenAPIDocument`; документ парсится как YAML, `openapi` начинается с `3.1`, `info.version` == `1.0.0` (валидный SemVer), среди путей есть `/healthz` и нет `/metrics`; GET не увеличивает счётчик health-check.
- [X] T006 [P] [US1] Добавить проверки соответствия health-check в `backend/cabby-gateway/internal/server/health_test.go`: тело 200 `{"status":"ok"}` и тело 503 `{"status":"unavailable"}` (при подменённой готовности) соответствуют схеме `HealthStatus` — единственное поле `status`, без дополнительных полей.

### Implementation for User Story 1

- [X] T007 [US1] Реализовать `backend/cabby-gateway/internal/server/contract.go`: HTTP-обработчик, отдающий `api.OpenAPIDocument` на GET `/openapi.yaml` с `Content-Type: text/yaml; charset=utf-8`; read-only, без побочных эффектов, не учитывается в метриках health-check (зависит от T003).
- [X] T008 [US1] Зарегистрировать маршрут контракта в `backend/cabby-gateway/internal/server/router.go`: GET `/openapi.yaml` обслуживается, прочие методы для этого пути сохраняют текущее поведение 405 (зависит от T004, T007).

**Checkpoint**: MVP — контракт опубликован и обнаруживаем, health-check соответствует контракту.

---

## Phase 4: User Story 2 - Предсказуемая обработка ошибок (Priority: P2)

**Goal**: Неподдерживаемые и некорректные запросы возвращают предсказуемый отказ в едином JSON-формате с машиночитаемой категорией, без утечки внутренних сведений.

**Independent Test**: `curl -i http://127.0.0.1:8082/unknown` → 404 `{"error":{"code":"unknown_operation",...}}`; `curl -i -X POST .../healthz` и `curl -i -I .../healthz` → 405 `{"error":{"code":"method_not_allowed",...}}`; `curl -i http://127.0.0.1:8082/metrics` → 404 `unknown_operation`; `message` не содержит тела запроса, секретов и внутренних сведений.

### Tests for User Story 2 ⚠️ (писать первыми, должны упасть до реализации)

- [X] T009 [P] [US2] Написать тесты единой модели ошибок в `backend/cabby-gateway/internal/server/errors_test.go`: неописанный путь → 404 с `code=unknown_operation`; POST и HEAD на `/healthz` → 405 с `code=method_not_allowed`; `/metrics` на публичном порту → 404 `unknown_operation`; ответ `application/json` формы `{"error":{"code","message"}}`; `message` не содержит тело запроса, секреты и сведения о внутреннем устройстве.

### Implementation for User Story 2

- [X] T010 [US2] Реализовать `backend/cabby-gateway/internal/server/errors.go`: запись единого конверта `Error` и стабильные коды (`unknown_operation`, `method_not_allowed`) с `Content-Type: application/json`; сообщения без секретов, внутренних и персональных данных.
- [X] T011 [US2] Переключить ветки 404 (по умолчанию) и 405 в `backend/cabby-gateway/internal/server/router.go` с plain-text `http.NotFound`/`http.Error` на конверт из `errors.go` (зависит от T010).
- [X] T012 [US2] Обновить ожидания 404/405 в `backend/cabby-gateway/internal/server/health_test.go` с plain-text на единый JSON-конверт (зависит от T011).

**Checkpoint**: US1 и US2 работают независимо; отказы предсказуемы и машиночитаемы.

---

## Phase 5: User Story 3 - Совместимость и развитие контракта (Priority: P3)

**Goal**: Клиент может определить версию контракта; изменения регулируются политикой совместимости.

**Independent Test**: Отданный контракт содержит `info.version` = валидный SemVer `1.0.0` (проверяется Go-тестом); политика совместимости описана в руководстве.

### Tests for User Story 3 ⚠️

- [X] T013 [P] [US3] Добавить тест версии и авторитетности в `backend/cabby-gateway/internal/server/contract_version_test.go`: `info.version` присутствует, является валидным SemVer и равен `1.0.0`; документ — авторитетный перечень операций (содержит `/healthz`, не содержит `/metrics`).

### Implementation for User Story 3

- [X] T014 [P] [US3] Убедиться, что Go-тесты (T002) валидируют `backend/cabby-gateway/api/openapi.yaml` без ошибок, и сверить раздел «Совместимость и эволюция» в `specs/002-gateway-external-contract/contracts/external-api.md` (аддитивные изменения — minor, ломающие — major + план миграции) с фактическим документом.

**Checkpoint**: Все три истории функциональны независимо.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Сквозные проверки и подтверждение соответствия quickstart.

- [X] T015 [P] Проверить, что Docker-сборка встраивает контракт: `.dockerignore` не исключает `backend/cabby-gateway/api/`, а образ, собранный `docker compose --env-file backend/cabby-gateway/.env --env-file deploy/monitoring/.env build cabby-gateway`, отдаёт документ на GET `/openapi.yaml` (Dockerfile использует `COPY . .`, изменений не ожидается — только проверка).
- [X] T016 [P] Прогнать проверки проекта в `backend/cabby-gateway`: `make test`, `make test-race`, `make vet` — все зелёные.
- [X] T017 Выполнить сценарии `specs/002-gateway-external-contract/quickstart.md` end-to-end на запущенном сервисе и сверить результаты со сводной таблицей (discovery, health-check, 404/405, метрики вне внешнего контракта, parity).

---

## Phase 7: Converge — устранение дрейфа артефактов и защита паритета

**Purpose**: Синхронизировать plan.md с фактическим решением (OpenAPI-линтер удалён из-за недоступности `artifactory.tutu.ru`) и закрыть SC-005/FR-011 автоматическими проверками дрейфа.

- [X] T018 [P] Убрать устаревшие ссылки на OpenAPI-линтер (Spectral) из `specs/002-gateway-external-contract/plan.md`: Primary Dependencies (строка ~15), Testing (строка ~19), Constitution Check V (строка ~41). Зафиксировать, что валидация контракта выполняется Go-тестами без внешних npm-инструментов.
- [X] T019 [P] Добавить тест паритета канонического файла и проектного эталона в `backend/cabby-gateway/internal/server/contract_test.go`: `api/openapi.yaml` байт-в-байт равен `specs/002-gateway-external-contract/contracts/openapi.yaml` (чтение эталона по относительному пути из теста пакета `server`).
- [X] T020 [P] Добавить тест соответствия маршрутов контракту в `backend/cabby-gateway/internal/server/contract_test.go`: каждый путь, обслуживаемый `Router` (`pathHealth`, `pathContract`), определён в `openapi.yaml`, усиливая FR-011 (контракт — авторитетный перечень операций).
- [X] T021 Прогнать `make test`, `make test-race`, `make vet` после T018–T020 — все зелёные.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: нет зависимостей — старт сразу.
- **Foundational (Phase 2)**: зависит от Setup (T003 ← T001); блокирует US1 и US2.
- **US1 (Phase 3)**: зависит от Foundational (T007 ← T003, T008 ← T004).
- **US2 (Phase 4)**: зависит от Foundational (T011 ← T004); не зависит от US1.
- **US3 (Phase 5)**: зависит от Setup (T013/T014 ← T001, T002); runtime-проверка версии опирается на документ, а не на маршрутизатор.
- **Polish (Phase 6)**: зависит от завершения нужных историй.

### User Story Dependencies

- **US1 (P1)**: после Foundational; независимо от US2/US3.
- **US2 (P2)**: после Foundational; независимо от US1 (общий файл `router.go` — выполнять последовательно при совмещении, а не параллельно).
- **US3 (P3)**: после Setup; валидация версии не требует US1, но полный сценарий discovery версии — через эндпоинт US1.

### Within Each User Story

- Тесты пишутся и падают до реализации.
- US1: T005/T006 (тесты) → T007 (обработчик) → T008 (маршрут).
- US2: T009 (тесты) → T010 (errors.go) → T011 (router) → T012 (обновление ожиданий).
- US3: T013 (тест) → T014 (валидация/политика).

### Parallel Opportunities

- Setup: T001 и T002 — разные файлы, параллельно.
- Foundational: T003 и T004 — разные пакеты, параллельно.
- US1: T005 и T006 — разные тестовые файлы, параллельно.
- US2: T009 — единственный параллелизуемый (один тестовый файл); реализация последовательна (общий `router.go`).
- US3: T013 и T014 — разные файлы, параллельно.
- Polish: T015 и T016 — разные проверки, параллельно.

> Примечание: `router.go` изменяется в T004 (основа), T008 (US1) и T011 (US2). Эти задачи не выполнять параллельно — только последовательно в порядке фаз.

---

## Parallel Example: User Story 1

```bash
# Тесты US1 параллельно (разные файлы):
Task: "Discovery/parity тесты в backend/cabby-gateway/internal/server/contract_test.go"   # T005
Task: "Conformance-проверки health-check в backend/cabby-gateway/internal/server/health_test.go"  # T006

# Затем реализация последовательно:
Task: "Обработчик контракта backend/cabby-gateway/internal/server/contract.go"            # T007
Task: "Регистрация маршрута в backend/cabby-gateway/internal/server/router.go"            # T008
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Phase 1: Setup (T001, T002).
2. Phase 2: Foundational (T003, T004).
3. Phase 3: US1 (T005–T008).
4. **STOP and VALIDATE**: `make run` + `curl /openapi.yaml` и `/healthz`; `make test`.
5. MVP готов: контракт опубликован и обнаруживаем, health-check соответствует.

### Incremental Delivery

1. Setup + Foundational → основа готова.
2. US1 → проверить независимо → MVP.
3. US2 → единая модель ошибок → проверить независимо.
4. US3 → версия и совместимость → проверить.
5. Polish → сквозные проверки и quickstart.

### Parallel Team Strategy

1. Команда выполняет Setup + Foundational.
2. После Foundational: разработчик A — US1, разработчик B — US2 (с учётом последовательного правки `router.go`), разработчик C — US3 (независимо, нужен только Setup).

---

## Notes

- [P] = разные файлы, нет незавершённых зависимостей.
- Семантика health-check (200/503) и счётчик метрик не изменяются; изменения аддитивны и обратно совместимы (выпущенных клиентов нет).
- Внутренний интерфейс метрик (порт 9091) не входит во внешний контракт; на публичном порту `/metrics` → 404 `unknown_operation`.
- Канонический источник истины — `backend/cabby-gateway/api/openapi.yaml`; parity-тест исключает дрейф между документом и сервисом.
- Коммит после каждой задачи или логической группы; сообщения — по Conventional Commits (см. AGENTS.md).
