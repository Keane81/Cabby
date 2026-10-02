# Implementation Plan: Каббер — публикация текущего местоположения (v1)

**Branch**: `004-cabber-location` (рабочая ветка `feature/coordinates`) | **Date**: 2026-10-01 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/004-cabber-location/spec.md`

## Summary

Одна новая публичная операция REST — `POST /cabber/location` — принимает `cabby-gateway`. Он подтверждает личность по Bearer-токену через `auth` (новый gRPC-метод `VerifyCabberSession`, поверх уже существующего `service.Verify`) и передаёт `cabber_id` с координатами новому сервису `location` по gRPC. `location` владеет записями местоположения, валидирует координаты, проставляет момент приёма своими часами и добавляет неизменяемую строку в собственную PostgreSQL (контейнер `location-db`, база `location`). Ни замены, ни удаления записей нет, лимита частоты нет; защищённость от перегрузки — ограниченный пул, дедлайны и ограничение размера тела. Чтения записей в v1 нет, поэтому история проверяется тестами хранилища и SQL-запросом в quickstart, а не через API. Контракт REST растёт до 1.2.0 (аддитивно).

## Technical Context

**Language/Version**: Go 1.26.1 (как в остальных модулях)

**Primary Dependencies**: без новых внешних библиотек — те же `google.golang.org/grpc`, `protobuf`, `pgx/v5`, `prometheus/client_golang`, `zerolog`; общий `lifecycle`; новый пакет `location.v1` в `contracts`. Раннер миграций у `auth` внутренний (`internal/migrate`); см. R-04 про его повторное использование.

**Storage**: PostgreSQL 18 — отдельный контейнер `location-db`, база и роль `location`, том `location-data`. Одна таблица `cabber_location`. Граница данных по принципу I: `location` не читает БД `auth`, `auth` не читает БД `location`.

**Testing**: `go test` во всех модулях (`make check`), интеграционные тесты репозитория — под тегом `integration` и env-gate по DSN, как у `auth`. Контрактовые тесты gateway (`TestCanonicalContractMatchesRepositoryReference`, `TestRouterPathsAreDefinedInContract`) и parity-тест proto.

**Target Platform**: Linux-контейнеры в Docker Compose; наружу публикуется только HTTP gateway. gRPC `location` (9095) и метрики (9096) остаются внутри сети compose.

**Project Type**: backend-монорепозиторий, микросервисы; четвёртый Go-модуль сервиса (`location`) плюс изменения в `contracts`, `auth`, `cabby-gateway`.

**Performance Goals** (допущения; замер на ноутбуке в R-02, замер на целевом окружении остаётся долгом): расчётная нагрузка 1 000 активных кабберов × 1 сообщение/5 с ≈ 200 rps устойчиво, пик до 600 rps; p95 подтверждения приёма ≤ 300 мс при 200 rps (SC-001 даёт запас до 1 с); дедлайн каждого межсервисного вызова — 2 с. Каждая запись добавляет вызов `VerifyCabberSession` в `auth`, поэтому допущение 003 «≤ 5 rps» для `auth` в этой фиче заменено: проверка одной строки по уникальному индексу при 200–600 rps (R-02).

**Constraints**: координаты не попадают в логи, метрики, сообщения об ошибках и labels (FR-011, SC-006) — проверяется тестом-«утечкой», как в `auth`; `/healthz` gateway по-прежнему не зависит от `auth`, `location` и их БД; пул соединений `location` — до 16, RSS < 128 MiB; тело запроса не более 1 КиБ.

**Scale/Scope**: 4 модуля (`cabby-gateway`, `auth`, `location`, `contracts`), 2 новых контейнера (`location`, `location-db`) и изменённый compose, 1 REST-операция, 1 новый gRPC-метод у `auth`, 1 новый gRPC-сервис с 1 методом, 1 таблица. Оценка роста данных: при 200 rps ≈ 17 млн строк в сутки; замер дал ≈ 175 байт на строку с индексом, то есть ≈ 3 ГБ/сутки (R-02).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Принцип | Статус | Обоснование |
|---|---|---|
| I. Явные границы и ответственность | PASS | Записи местоположения — отдельная бизнес-область с собственным владельцем: сервис `location` и его БД. Личность подтверждает `auth` (он один владеет сессиями) через опубликованный метод контракта `auth.v1`; `location` не знает про токены и принимает `cabber_id` только от доверенного gateway во внутренней сети. gateway не ходит ни в одну из БД. `contracts` получает пакет `location.v1`, владелец — `location`. |
| II. Совместимые контракты | PASS | REST: новая операция, `info.version` 1.1.0 → 1.2.0, существующие пути и коды не меняются; новых кодов ошибок нет — используются `invalid_request`, `unauthorized`, `service_unavailable`, `internal_error`. gRPC: `auth.v1` получает метод `VerifyCabberSession` (аддитивно, minor); существующие клиенты не затронуты. Порядок развёртывания и откат — ниже. Клиенты (web/iOS/Android) — новые потребители, существующих версий они не ломают. |
| III. Устойчивость под нагрузкой | **DEVIATION** | Нагрузка, точки отказа и способ проверки заданы (Performance Goals, R-02, quickstart §6). Дедлайны 2 с на оба вызова; повторов нет (R-06): повтор запроса записи создал бы дубль записи, а клиент и так присылает следующую точку. Идемпотентность не вводится осознанно (R-06). **Отступления**: (1) ограничения частоты нет по решению пользователя (FR-007) — компенсируется ограниченным пулом, дедлайном, потолком тела и алертом по росту; перегрузка проявляется как `503`, а не как лимит; (2) сквозной трассировки по-прежнему нет — работает уже введённый `request_id`, он передаётся и в `location` (см. Complexity Tracking). Наблюдаемость: метрики операции и зависимостей у gateway, метрики `location`, дашборд Grafana. |
| IV. Безопасность и данные | **DEVIATION** | Доступ проверяется на сервере: токен подтверждает `auth`, `cabber_id` определяется только им (FR-009); `location` недоступен снаружи. Координаты не логируются (FR-011). **Отступление**: политика хранения и удаления записей не вводится (spec, Assumptions) — записи копятся неограниченно и содержат персональные данные; это блокирующий долг перед запуском на реальных водителях, как и право на удаление из 003 (Complexity Tracking). Шифрование «на диске» и «в канале» — как в остальном проекте (внутренняя сеть compose, без TLS между сервисами); при отсутствии требований принцип IV просит определить их до реализации — решение R-09 фиксирует «как в 003» и срок пересмотра. |
| V. Проверяемые изменения на всех платформах | PASS | Затронут только backend; web/iOS/Android — потребители, изменений в репозитории нет, у них появляется новая операция для подключения. Матрица тестов: unit-тесты валидации и сервиса с инъекцией часов и фейковым хранилищем, интеграционные тесты репозитория на PostgreSQL, unit-тесты gRPC-сервера, тесты роутера gateway с заглушками двух портов, контрактовые тесты и parity proto, тест-«утечка» координат, e2e в quickstart. Каждому сценарию и edge case спецификации сопоставлен проверяемый шаг (data-model §6). |

**Итог гейта**: два зафиксированных отступления (III — нет лимита частоты и трассировки; IV — нет политики хранения). Оба принимаются с причиной, риском и сроком пересмотра ниже. После Phase 1 состав отступлений не изменился.

## Project Structure

### Documentation (this feature)

```text
specs/004-cabber-location/
├── spec.md
├── plan.md                         # этот файл
├── research.md                     # Phase 0: решения R-01…R-09
├── data-model.md                   # Phase 1: таблица, инварианты, схемы, матрица ошибок, трассировка к спецификации
├── quickstart.md                   # Phase 1: как поднять, проверить и посмотреть записи
├── checklists/requirements.md
└── contracts/
    ├── location.proto              # эталонная копия; канонический файл — backend/contracts/proto/location/v1/location.proto
    └── external-and-auth-changes.md # REST-операция (фрагмент OpenAPI) и дельта auth.v1
```

REST-контракт: канонический файл `backend/cabby-gateway/api/openapi.yaml` и эталон `specs/002-gateway-external-contract/contracts/openapi.yaml` правятся вместе при реализации и должны совпадать байт-в-байт; руководство `specs/002-gateway-external-contract/contracts/external-api.md` обновляется там же (AGENTS.md). Эталон `specs/003-cabber-auth/contracts/auth.proto` правится вместе с каноническим `auth.proto` (parity-тест).

### Source Code (repository root)

```text
backend/
├── contracts/
│   ├── proto/auth/v1/auth.proto          # + VerifyCabberSession (+ эталон specs/003-cabber-auth/contracts/auth.proto)
│   ├── proto/location/v1/location.proto  # новый канонический gRPC-контракт location
│   ├── authpb/                           # перегенерируется
│   ├── locationpb/                       # сгенерированный код (коммитится)
│   └── contract_parity_test.go           # + location.proto против specs/004-cabber-location/contracts/location.proto
│
├── location/                             # новый Go-модуль: владение записями местоположения
│   ├── go.mod                            # module github.com/Keane81/Cabby/backend/location
│   ├── Makefile                          # run test test-race vet test-integration migrate
│   ├── Dockerfile                        # multi-stage от корня репозитория: golang:1.26.1-alpine → scratch, USER 65532
│   ├── .env.example                      # CABBY_LOCATION_DB_PASSWORD, CABBY_LOCATION_DB_URL, CABBY_LOCATION_GRPC_PORT (9095)
│   ├── cmd/location/main.go              # конфиг, пул, миграции; gRPC и metrics запускает lifecycle.Run
│   ├── internal/
│   │   ├── config/                       # Load() — env и DSN, валидация на старте
│   │   ├── service/                      # Record: валидация, округление, Now инъекцией
│   │   ├── repo/                         # postgres-реализация LocationRepository (pgx)
│   │   ├── migrate/                      # раннер встроенных миграций (см. R-04)
│   │   └── grpcserver/                   # реализация location.v1.LocationService, interceptor-ы лога/метрик/recover
│   └── migrations/                       # 0001_cabber_location.up.sql / .down.sql
│
├── auth/                                 # меняется минимально
│   └── internal/grpcserver/              # + VerifyCabberSession поверх service.Verify; новых слоёв нет
│
├── cabby-gateway/                        # меняется
│   ├── internal/authclient/              # + VerifyCabberSession в порту Operations
│   ├── internal/locationclient/          # новый пакет: порт Locations (RecordCabberLocation) и gRPC-адаптер
│   ├── internal/server/
│   │   ├── router.go                     # + строка POST /cabber/location; NewRouter принимает порт Locations
│   │   ├── location.go                   # хендлер: Bearer → Verify → разбор тела → Record → 201
│   │   ├── metrics.go                    # + операция record_location в cabberRequests и cabberDependency
│   │   └── contract_test.go              # новый путь в списке
│   ├── internal/config/                  # + CABBY_LOCATION_ADDR
│   ├── cmd/cabby-gateway/main.go         # dial к location
│   ├── .env.example                      # + CABBY_LOCATION_ADDR (location:9095)
│   └── api/openapi.yaml                  # 1.2.0: path /cabber/location + схемы
│
├── bruno/cabby-gateway/                  # + location.bru (seq 6, auth: bearer {{accessToken}})
└── Makefile                              # MODULES += location

deploy/monitoring/
├── prometheus.yml                        # + job location (location:9096)
└── grafana/dashboards/location.json      # RPS, доли отказов, p95 задержки, рост числа строк

compose.yaml                              # + сервисы location и location-db, том location-data, depends_on у gateway
```

**Structure Decision**: отдельный сервис `location`, а не расширение `auth`. Принцип I требует, чтобы сервис владел своей бизнес-областью и данными: `auth` отвечает за учётные записи и доступ, а поток координат — другая область с другим профилем нагрузки (200 rps записи против ≤ 5 rps у `auth`), собственным ростом данных и собственной будущей политикой хранения. Положить таблицу в БД `auth` значило бы связать их масштабирование и права доступа, а будущее удаление данных по сроку — с чужим хранилищем. Цена решения — новый модуль, контейнер БД и дашборд; она принята, потому что тот же набор (`config`, `service`, `repo`, `migrate`, `grpcserver`) уже отработан в `auth`, и новый сервис повторяет его без новых идей. Отдельный контейнер БД, а не вторая база в `postgres` из 003: так «владелец данных» подтверждается физически (свой том, свой пароль, свой жизненный цикл), и `auth` не получает общей точки отказа. Разбиение на слои — как в `auth`; у gateway новой архитектуры нет: добавляется строка в роутер, хендлер и порт `Locations` в пакете `locationclient`.

## Поведение при отказах и развёртывание

- Нет заголовка, неверная схема, пустой токен, неизвестный/отозванный/просроченный/простаивающий доступ → единый `401 unauthorized`; `location` не вызывается и ничего не пишется.
- `auth` недоступен, дедлайн истёк → `503 service_unavailable`; `location` не вызывается.
- Тело не JSON, лишнего размера, нет `latitude`/`longitude`, значение не число, не конечное, вне диапазона → `400 invalid_request` с `field`; запись не создаётся (FR-004). При нескольких недостатках сообщается первый в порядке `latitude → longitude`.
- `location` или `location-db` недоступны, дедлайн истёк → `503 service_unavailable`. Ложного успеха нет: `201` возвращается только после подтверждённой записи (commit).
- Неизвестный путь/метод — поведение 002 без изменений; `GET /cabber/location` → `405 method_not_allowed` (чтения нет).
- Порядок развёртывания: (1) `contracts`; (2) `location-db` и `location` (миграция 0001, трафика ноль); (3) `auth` с методом `VerifyCabberSession`; (4) `cabby-gateway` с контрактом 1.2.0 и `CABBY_LOCATION_ADDR`; (5) дашборды. Откат — вернуть gateway на 1.1.0: путь становится `unknown_operation`, остальное не затрагивается; `location` и данные остаются на месте. Метод `auth` аддитивен — откат `auth` не требуется.

## Complexity Tracking

> Заполнено, потому что принципы III и IV требуют оправдания отступлений.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|---|---|---|
| Нет ограничения частоты при ~200 rps без квот (принцип III — защита от перегрузки) | Явное решение пользователя 2026-10-01 (FR-007) | Лимит на каббера или на IP: отклонён пользователем. Компенсации: аутентифицированный доступ обязателен, потолок тела 1 КиБ, пул БД 16 и дедлайн 2 с — перегрузка проявится как `503`, а не как рост очереди; метрика роста строк и алерт на скорость роста. Пересмотр: при превышении 600 rps или росте БД выше порога из quickstart §6, либо при первом злоупотреблении |
| Нет политики хранения и удаления записей (принцип IV) | Решение пользователя: хранить все записи; срок хранения и удаление учётной записи — вне объёма (spec, Assumptions) | Срок хранения «по умолчанию», например 90 дней: отклонён, потому что вводит удаление данных, которое пользователь не согласовывал; партиционирование и TTL требуют решения о сроке. Риск: координаты — персональные данные, объём ≈ 3 ГБ/сутки при расчётной нагрузке. **Блокирующий долг** до запуска на реальных водителях; пересмотр — отдельная спецификация и миграция (см. R-03 про партиционирование) |
| Нет сквозной трассировки (принцип III), как в 003 | Уже принятое отступление: введён `request_id` | OTel/Jaeger — два сервиса и десятки зависимостей. В этой фиче цепочка длиннее на один вызов, поэтому `request_id` передаётся и в `location` под тем же ключом `x-request-id`; пересмотр — вместе с 003 при втором межсервисном потребителе, не откладывается за этот срок |
| Новый сервис, модуль и контейнер БД вместо таблицы в `auth` | Принцип I: владелец области и данных; разный профиль нагрузки и будущая независимая политика хранения (Structure Decision) | Таблица в БД `auth`: отклонена, потому что связывает рост и масштабирование координат с учётными записями и протаскивает чужое хранилище в будущую политику удаления |

## Checklists After Plan

- [x] Каждая строка Technical Context заменена реальными значениями, `NEEDS CLARIFICATION` не осталось
- [x] Матрица соответствия принципам constitution заполнена, отступления вынесены в Complexity Tracking
- [x] Дерево исходников конкретное: реальные пути
- [x] Порядок развёртывания и отката описан (принцип II)
- [x] `research.md`, `data-model.md`, `quickstart.md`, `contracts/*` созданы как артефакты Phase 0/1
