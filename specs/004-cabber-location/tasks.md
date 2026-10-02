# Tasks: Каббер — публикация текущего местоположения (v1)

**Input**: Design documents from `specs/004-cabber-location/`

**Prerequisites**: plan.md, spec.md (User Story 1), research.md (R-01…R-09), data-model.md (§1–§6), contracts/location.proto, contracts/external-and-auth-changes.md, quickstart.md

**Tests**: test tasks MANDATORY. Принцип V конституции требует проверяемых критериев на каждое изменение; матрица проверок — data-model.md §6.

**Organization**: в спецификации одна история (US1, P1) после снятия P2. Фаза 2 — всё, что нужно до первой записи и не принадлежит истории (контракты, `auth.VerifyCabberSession`, каркас и схема `location`). Фаза 3 — сама история: сервис, хранилище, gRPC, gateway, публичный контракт, Bruno.

**Paths**: все пути — от корня репозитория, как в plan.md §Project Structure. Go-тесты лежат рядом с кодом (`_test.go`); интеграционные — под тегом `integration`, как у `auth`.

**Внешний контракт публикуется один раз**: `backend/cabby-gateway/api/openapi.yaml` и эталон `specs/002-gateway-external-contract/contracts/openapi.yaml` правятся в одном наборе изменений (AGENTS.md), `info.version` 1.1.0 → 1.2.0. Эталон `specs/003-cabber-auth/contracts/auth.proto` правится вместе с каноническим.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: можно параллельно (разные файлы, нет зависимости от незавершённых задач)
- **[US1]**: принадлежит User Story 1

---

## Phase 1: Setup

**Purpose**: каркас нового модуля `location` и подключение к монорепозиторию, пока без логики

- [X] T001 [P] Create `backend/location/go.mod` with `module github.com/Keane81/Cabby/backend/location`, `go 1.26.1`, and `replace` directives for `../contracts` and `../lifecycle` (same shape as `backend/auth/go.mod`)
- [X] T002 [P] Add `backend/location/Makefile` mirroring `backend/auth/Makefile`: targets `run test test-race vet test-integration migrate`, where `test-integration` takes its DSN only from `CABBY_LOCATION_DB_URL` passed to make and never from `.env`
- [X] T003 [P] Add `backend/location/.env.example` (`CABBY_LOCATION_DB_PASSWORD=change-me`, `CABBY_LOCATION_DB_URL=postgres://location:change-me@location-db:5432/location?sslmode=disable`, `CABBY_LOCATION_GRPC_PORT=9095`) with the same explanatory comments as `backend/auth/.env.example`
- [X] T004 [P] Add `backend/location/Dockerfile`: multi-stage `golang:1.26.1-alpine` → `FROM scratch`, `USER 65532:65532`, `EXPOSE 9095 9096`, build context is the repository root and copies `backend/contracts`, `backend/lifecycle`, `backend/location` as in `backend/auth/Dockerfile`
- [X] T005 [P] Create `backend/location/cmd/location/main.go` as a `package main` stub that only logs and exits, so `go build ./...` and `make vet` pass for the module from now on
- [X] T006 Add `location` to `MODULES` in `backend/Makefile` so `make check` covers the new module

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: контракты и сгенерированный код, проверка доступа в `auth`, схема и миграции `location`, конфигурация. US1 не может начаться, пока фаза не закрыта.

**⚠️ CRITICAL**: ни одна задача фазы 3 не стартует до закрытия фазы 2

### Contracts and codegen

- [X] T007 Copy `specs/004-cabber-location/contracts/location.proto` byte-for-byte to the canonical `backend/contracts/proto/location/v1/location.proto` — the parity test in T008 needs both files
- [X] T008 Extend `backend/contracts/contract_parity_test.go` with `TestCanonicalLocationProtoMatchesRepositoryReference` comparing `backend/contracts/proto/location/v1/location.proto` with `specs/004-cabber-location/contracts/location.proto`
- [X] T009 Add `VerifyCabberSession`, `VerifyCabberSessionRequest` and `VerifyCabberSessionResponse` to `backend/contracts/proto/auth/v1/auth.proto` exactly as in `specs/004-cabber-location/contracts/external-and-auth-changes.md` §2, and apply the same edit to the reference `specs/003-cabber-auth/contracts/auth.proto` so `TestCanonicalProtoMatchesRepositoryReference` stays green
- [X] T010 Extend `backend/contracts/buf.gen.yaml` (and `buf.yaml` if modules are listed) to emit `backend/contracts/locationpb/`, then run `make -C backend/contracts proto` and commit the regenerated `backend/contracts/authpb/*.pb.go` and the new `backend/contracts/locationpb/location.pb.go` and `location_grpc.pb.go`
- [X] T011 Run `go mod tidy` in `backend/contracts` and confirm `make -C backend/contracts test` passes (both parity tests)

### auth: expose the session check (R-01)

- [X] T012 Implement `VerifyCabberSession` in `backend/auth/internal/grpcserver/server.go` on top of the existing `service.Verify` (`backend/auth/internal/service/session.go`): `ErrInvalidSession` → `UNAUTHENTICATED` with the same status and message as `DeleteCabberSession`, `ErrDependency` → `UNAVAILABLE`, success → `cabber_id`; no new layers, no revocation
- [X] T013 [P] Write `backend/auth/internal/grpcserver/verify_session_test.go`: valid token returns the owner, unknown/revoked/expired/idle token gives the identical `UNAUTHENTICATED` answer as `DeleteCabberSession`, storage failure gives `UNAVAILABLE`, the token never appears in logs or the error text (extend the existing leak test in `backend/auth/internal/grpcserver/leak_test.go`)
- [X] T014 [P] Count the new method in the auth interceptors/metrics in `backend/auth/internal/grpcserver/metrics.go` the same way as the existing three, and extend `backend/auth/internal/grpcserver/server_test.go` if it enumerates methods

### location: schema, migrations, config

- [X] T015 Create `backend/location/migrations/0001_cabber_location.up.sql` with the DDL from data-model.md §1: table `cabber_location` (`id bigint generated always as identity` PK, `cabber_id uuid not null`, `latitude`/`longitude double precision not null` with `check` ranges, `received_at timestamptz not null`) and index `cabber_location_cabber_received (cabber_id, received_at desc, id desc)`
- [X] T016 [P] Create `backend/location/migrations/0001_cabber_location.down.sql` dropping the index and the table
- [X] T017 [P] Create `backend/location/migrations/embed.go` exposing the SQL files through `//go:embed *.sql`
- [X] T018 Copy the embedded migration runner from `backend/auth/internal/migrate/migrate.go` to `backend/location/internal/migrate/migrate.go` (R-04: advisory lock, version table, `Load`, `Up`, `UpWaiting`), changing only the package-visible names that mention `auth`, and add a comment pointing at R-04 as the trigger for extracting a shared module
- [X] T019 [P] Write `backend/location/internal/migrate/migrate_test.go`: first run, second run as a no-op, down-migration restoring an empty schema (same cases as in `backend/auth/internal/migrate/migrate_test.go`)
- [X] T020 Implement `backend/location/internal/config/config.go`: `Load() (Config, error)` reading `CABBY_LOCATION_DB_URL` and `CABBY_LOCATION_GRPC_PORT` (default `9095`), metrics address `:9096` as a code constant (as R-11 in 003), validation at start, error text that never echoes the DSN
- [X] T021 [P] Write `backend/location/internal/config/config_test.go`: defaults, missing DSN, invalid port, DSN password absent from the error text

**Checkpoint**: `make check` is green; contracts generate; `auth` answers `VerifyCabberSession`; `location` schema applies and rolls back.

---

## Phase 3: User Story 1 — Каббер оставляет свои текущие координаты (Priority: P1) 🎯 MVP

**Goal**: аутентифицированный каббер отправляет широту и долготу; каждое принятое сообщение сохраняется отдельной неизменяемой записью с моментом приёма; некорректные и неаутентифицированные запросы ничего не пишут.

**Independent Test**: quickstart §3–§5 — зарегистрироваться, войти, отправить две корректные пары (две записи в БД), некорректные (400, строк не прибавилось), без доступа и после выхода (401), перезапустить БД (записи на месте).

### Tests for User Story 1 (write first, they must fail before the implementation)

- [X] T022 [P] [US1] Write `backend/location/internal/service/validate_test.go`: table of boundaries ±90/±180 accepted, just beyond rejected with `out_of_range`, NaN/±Inf rejected with `not_a_number`, `0, 0` accepted, latitude reported before longitude when both are wrong, rounding to 7 decimals (data-model §3, spec edge cases)
- [X] T023 [P] [US1] Write `backend/location/internal/service/record_test.go` with a fake repository and an injected clock: accepted request stores one record with `received_at` equal to the clock value (FR-006); rejected request calls the repository zero times (FR-004); two calls produce two records and the first is untouched (FR-005); no call returns a "too frequent" error (FR-007); repository failure becomes `ErrDependency`
- [X] T024 [P] [US1] Write `backend/location/internal/repo/location_integration_test.go` (tag `integration`, skips without `CABBY_LOCATION_DB_URL`): N inserts give N rows and none is changed; rows are ordered by `(received_at, id)`; the `check` constraints reject out-of-range values at the database level; two concurrent inserts for one cabber leave both rows
- [X] T025 [P] [US1] Write `backend/location/internal/grpcserver/server_test.go`: `INVALID_ARGUMENT` carries `ErrorField` with `latitude`/`longitude` and the reason, storage failure maps to `UNAVAILABLE`, an empty or non-UUID `cabber_id` maps to `INTERNAL`, `request_id` from `x-request-id` is reused when well-formed
- [X] T026 [P] [US1] Write `backend/location/internal/grpcserver/leak_test.go`: after an accepted and a rejected call with distinctive coordinates, neither log output, metrics text nor error messages contain the coordinate strings (FR-011, SC-006), as in `backend/auth/internal/grpcserver/leak_test.go`
- [X] T027 [P] [US1] Write `backend/cabby-gateway/internal/server/location_test.go` with fakes for both ports: no/invalid header → 401 and neither port called for `location`; `auth` unavailable → 503 and `location` not called; non-JSON, non-object, body over 1 KiB, missing field, string/`null` value → 400 `invalid_request` with `field`; `location` rejection and `location` unavailable map per data-model §4; success → 201 with `received_at`; a `cabber_id` in the body is an unknown property and rejects the body with `400 invalid_request`, and `location` is called with the id returned by `VerifyCabberSession` (FR-009); `GET /cabber/location` → 405; the 401 body is byte-identical to that of `DELETE /cabber/session`
- [X] T028 [P] [US1] Write `backend/cabby-gateway/internal/locationclient/locationclient_test.go`: status translation (`INVALID_ARGUMENT`+`ErrorField` → `Invalid`, `UNAVAILABLE`/`DEADLINE_EXCEEDED` → `ErrUnavailable`, other → `ErrInternal`), call timeout 2 s, `x-request-id` propagated, no retry
- [X] T029 [P] [US1] Extend `backend/cabby-gateway/internal/authclient/authclient_test.go` for `VerifyCabberSession`: success returns the cabber id, `UNAUTHENTICATED` → `ErrUnauthorized`, `UNAVAILABLE` → `ErrUnavailable`, the token never appears in an error text

### Implementation: location service

- [X] T030 [US1] Implement `backend/location/internal/service/validate.go`: finite-number and range checks, fixed order latitude → longitude, reasons `not_a_number`/`out_of_range`, rounding to 7 decimals (R-05); no knowledge of HTTP or gRPC
- [X] T031 [US1] Implement `backend/location/internal/repo/location.go`: `LocationRepository` interface and its pgx implementation with a single `insert … returning` of one row (`cabber_id`, `latitude`, `longitude`, `received_at` from the argument, not from `now()`), no update or delete methods (FR-005); pool limited to 16 connections
- [X] T032 [US1] Implement `backend/location/internal/service/record.go`: `Service.Record(ctx, cabberID, lat, lon)` with the injected `Now`, validation, then insert; errors `ErrInvalid{Field, Reason}` and `ErrDependency`; no logging of coordinates (R-08); constructor in `backend/location/internal/service/service.go`
- [X] T033 [US1] Implement `backend/location/internal/grpcserver/server.go` for `location.v1.LocationService.RecordCabberLocation` per `specs/004-cabber-location/contracts/location.proto`, with `ErrorField` details and the status mapping of T025
- [X] T034 [P] [US1] Add `backend/location/internal/grpcserver/interceptors.go` and `metrics.go`: logging with `request_id`, `cabber_id`, outcome and duration only; recover; metrics `cabby_location_requests_total{outcome}`, a record-duration histogram and a counter of inserted rows, labels never carry coordinates (R-08)
- [X] T035 [US1] Wire `backend/location/cmd/location/main.go`: config, pool (DSN never echoed on failure), `migrate.UpWaiting` with 30 attempts × 1 s, log line `migrations applied`, the `-migrate` flag, then gRPC on `CABBY_LOCATION_GRPC_PORT` and metrics on `:9096` through `lifecycle.Run`, stop timeout 5 s, log line `location listening`

### Implementation: gateway

- [X] T036 [US1] Add `VerifyCabberSession(ctx, accessToken) (cabberID string, err error)` to the `Operations` port and to the gRPC client in `backend/cabby-gateway/internal/authclient/authclient.go` (2 s timeout, request id, same status translation as `DeleteCabberSession`)
- [X] T037 [US1] Create `backend/cabby-gateway/internal/locationclient/locationclient.go`: port `Locations` with `RecordCabberLocation(ctx, cabberID string, latitude, longitude float64) (time.Time, error)`, gRPC adapter with `Dial`, `Close`, 2 s timeout, `x-request-id`, no retry (R-06), and the same typed errors pattern as `authclient`
- [X] T038 [US1] Add `CABBY_LOCATION_ADDR` to `backend/cabby-gateway/internal/config/config.go` (required, validated like `CABBY_AUTH_ADDR`) and to `backend/cabby-gateway/internal/config/config_test.go`
- [X] T039 [US1] Implement `backend/cabby-gateway/internal/server/location.go`: Bearer via `bearerToken` → `VerifyCabberSession` → body limited to 1 KiB and decoded strictly like the other cabber bodies (object with `latitude` and `longitude` as JSON numbers, presence checked on raw values, an unknown property rejects the body) → `RecordCabberLocation` → `201 {"received_at": RFC 3339 UTC}`; failures through `writeAuthFailure`-style mapping (extend `backend/cabby-gateway/internal/server/errors.go` for `locationclient` errors without adding an `ErrorCode`)
- [X] T040 [US1] Register the route in `backend/cabby-gateway/internal/server/router.go`: constant `pathCabberLocation = "/cabber/location"`, `rt.mux.HandleFunc(http.MethodPost+" "+pathCabberLocation, rt.counted(operationRecordLocation, rt.recordCabberLocation))`, and extend `NewRouter` and the `Router` struct with the `Locations` port; update every `NewRouter` call site and test helper
- [X] T041 [US1] Add the operation `record_location` to `backend/cabby-gateway/internal/server/metrics.go` (`cabberOperations`, `cabberRequests`, `cabberDependency` for both `auth` and `location` calls) and extend `backend/cabby-gateway/internal/server/metrics_test.go`
- [X] T042 [US1] Dial `location` and pass the port into `NewRouter` in `backend/cabby-gateway/cmd/cabby-gateway/main.go`; `/healthz` must stay independent of `auth`, `location` and their databases; close the client on graceful shutdown

### Published contract and manual checks (AGENTS.md)

- [X] T043 [US1] Add `POST /cabber/location` and the schemas `CabberLocationRequest` and `CabberLocationReceipt` to `backend/cabby-gateway/api/openapi.yaml` as in `specs/004-cabber-location/contracts/external-and-auth-changes.md` §1, extend the top-level `description`, set `info.version: 1.2.0`
- [X] T044 [US1] Copy `backend/cabby-gateway/api/openapi.yaml` byte-for-byte to `specs/002-gateway-external-contract/contracts/openapi.yaml`
- [X] T045 [US1] Update the human-readable guide `specs/002-gateway-external-contract/contracts/external-api.md`: section for `POST /cabber/location`, response table (201, 400, 401, 405, 500, 503), the version history entry 1.2.0 (additive, minor)
- [X] T046 [US1] Extend `backend/cabby-gateway/internal/server/contract_test.go` and `contract_version_test.go` with the new path and version 1.2.0, and confirm `TestCanonicalContractMatchesRepositoryReference` and `TestRouterPathsAreDefinedInContract` pass
- [X] T047 [P] [US1] Add `backend/bruno/cabby-gateway/location.bru` (`seq: 6`, `post`, `url: {{host}}/cabber/location`, `auth: bearer` with `{{accessToken}}`, body `{"latitude": 55.7558, "longitude": 37.6173}`) in the style of `logout.bru`

### Wiring and acceptance

- [X] T048 [US1] Add services `location` and `location-db` and the volume `location-data` to `compose.yaml`: `location-db` is `postgres:18-alpine` with `POSTGRES_USER`/`POSTGRES_DB` `location` and `POSTGRES_PASSWORD: ${CABBY_LOCATION_DB_PASSWORD}`, `location` builds from `backend/location/Dockerfile`, reads `./backend/location/.env`, exposes 9095 and 9096 without publishing, `depends_on: location-db`; `cabby-gateway` gets `depends_on` on `location`
- [X] T049 [P] [US1] Add `CABBY_LOCATION_ADDR=location:9095` to `backend/cabby-gateway/.env.example`
- [X] T050 [US1] Walk quickstart §2–§5 against `make docker-up`: expected codes 201, 201, 400, 400, 401, 204, 401, exactly two rows in `cabber_location`, row count unchanged after restart of `location-db`; fix any command in `specs/004-cabber-location/quickstart.md` that does not run as written (port of the test database in §7 included)

**Checkpoint**: User Story 1 works end to end and can be demonstrated on its own.

---

## Phase 4: Polish & Cross-Cutting Concerns

- [X] T051 [P] Add the Prometheus job `location` (target `location:9096`) to `deploy/monitoring/prometheus.yml`
- [X] T052 [P] Create the Grafana dashboard `deploy/monitoring/grafana/dashboards/location.json`: RPS of `record_location`, share of 4xx/5xx, p95 of the record duration, inserted rows per hour (the growth signal of Complexity Tracking). The JSON is valid and every PromQL expression was run against Prometheus; the dashboard was not opened in the Grafana UI because its admin password is not known to the session
- [X] T053 Run the load step of quickstart §6 and write the measured p95, error share and rows per day into `specs/004-cabber-location/research.md` R-02 in place of the assumptions. DONE PARTIALLY: a closed-loop peak run (`ab`, 16 and 64 connections, 80 000 records, one session) is recorded; the paced 200 rps run for 5 minutes was not made because `ab` cannot pace requests, and it remains to be repeated on the target environment
- [X] T054 [P] Add a test in `backend/cabby-gateway/internal/server/absent_operations_test.go` that `GET`, `PUT` and `DELETE /cabber/location` give `405 method_not_allowed` and that no read operation for locations exists in the contract (spec Assumptions)
- [X] T055 Run `make check` from the repository root (`test`, `test-race`, `vet` in `contracts`, `lifecycle`, `auth`, `location`, `cabby-gateway`) and `make -C backend/location test-integration CABBY_LOCATION_DB_URL=…` against a scratch PostgreSQL; both must be green
- [X] T057 [P] Add Prometheus alert rules `deploy/monitoring/alerts.yml` (rate above 600 rps, p95 above 300 ms, failed inserts), load them through `rule_files` in `deploy/monitoring/prometheus.yml` and mount the file in `compose.yaml` — the compensation for the absence of a frequency limit named in plan.md Complexity Tracking (analysis finding E1)
- [X] T058 Add `backend/location/.env` to `COMPOSE_FILES` in the root `Makefile` so `make docker-up` interpolates `CABBY_LOCATION_DB_PASSWORD`
- [X] T056 Record the open debts in the specification the next feature must pick up: add to `specs/004-cabber-location/spec.md` § Dependencies that a retention and deletion policy for location records is a blocking debt before launch on real drivers (plan.md Complexity Tracking)

---

## Dependencies & Execution Order

- **Phase 1** — без зависимостей, T001–T005 параллельны, T006 после T001.
- **Phase 2** — после Phase 1. Контрактный блок T007→T011 последовательный (T008 нужен T007; T010 нужны T007 и T009; T011 после T010). Блок `auth` T012–T014 после T010. Блок `location` T015–T021 после T001; T018 после T015–T017 по тестам, но не по коду.
- **Phase 3** — после закрытия Phase 2. Тесты T022–T029 пишутся первыми и должны падать; реализация T030→T032 (валидация → репозиторий → сервис), затем T033→T035; gateway T036–T042 после T010 и T012, независимо от сервиса `location` до T050; контракт T043→T046; compose T048 после T004 и T035; T050 — после всего.
- **Phase 4** — после T050; T053 требует поднятого стека.

### Parallel opportunities

- Setup: T001–T005 вместе.
- Foundational: T013 и T014 после T012; T016, T017, T019, T021 рядом со своими блоками.
- US1: все тесты T022–T029 параллельны (разные файлы); T034 рядом с T033; T047 и T049 рядом с любой задачей фазы.
- Polish: T051, T052, T054 параллельны.

```text
# Пример: все тесты истории вместе
T022 validate_test · T023 record_test · T024 location_integration_test · T025 server_test
T026 leak_test · T027 gateway location_test · T028 locationclient_test · T029 authclient_test
```

## Implementation Strategy

**MVP = User Story 1 целиком** — она единственная. Порядок поставки: (1) Phase 1–2 вливаются первыми: контракты и `VerifyCabberSession` аддитивны и безопасны для уже работающих клиентов; (2) Phase 3 в порядке «`location` → gateway → публичный контракт → compose»; (3) T050 как приёмка; (4) Phase 4 до выхода на реальных водителей, при этом T053 (замер нагрузки) и T056 (долг по хранению и удалению) — условия запуска, а не украшение.

Порядок развёртывания и откат — plan.md §«Поведение при отказах и развёртывание».

## Notes

- Каждая задача завершается компилирующимся модулем и зелёным `make vet`; коммиты по смыслу (`feat(backend): …`, `docs: …`) по правилам AGENTS.md.
- Не меняйте посторонние изменения в рабочем дереве.
