# Tasks: Каббер — регистрация, аутентификация и выход (v1)

**Input**: Design documents from `specs/003-cabber-auth/`

**Prerequisites**: plan.md, spec.md (User Stories 1–3), research.md (R-01…R-11), data-model.md (§1–§8), contracts/auth.proto, quickstart.md

**Tests**: test tasks are MANDATORY here, not optional. Матрица проверок зафиксирована в plan.md §Constitution Check (принцип V) и data-model.md §8; спецификация требует измеримого подтверждения каждой Success Criteria (SC-001…SC-011). Плюс правило AGENTS.md: эндпоинт не готов, пока `make test`, `make test-race`, `make vet` в затронутых модулях не зелёные.

**Organization**: фазы 3–5 = User Stories из spec.md в порядке приоритета (P1 → P3). Каждая фаза даёт инкремент, который можно проверить и демонировать независимо.

**Paths**: все пути — от корня репозитория (`backend/…`, `deploy/…`, `specs/…`), как в plan.md §Project Structure. Go-тесты лежат рядом с кодом в том же пакете (`_test.go`); отдельных `tests/` каталогов в проекте нет.

**Внешний контракт публикуется один раз**: `api/openapi.yaml` — один файл на все три операции, `info.version` правится один раз (1.0.0 → 1.1.0), и AGENTS.md требует, чтобы обе копии контракта и Bruno-коллекция менялись в одном наборе изменений. Поэтому задачи контракта стоят в Phase 2, а не дублируются в каждом стори: дубль дал бы три minor-бампа и три правки одного файла.

**Пара допущений по разбивке**, зафиксированных, потому что они ломают наивную схему «модель → сервис → эндпоинт» для каждого стори:

1. Проверка доступа (`Verify` из T038) — механизм User Story 2, но без него не работает выход из User Story 3, а выход — единственная операция, требующая подтверждения личности в v1 (FR-017). Поэтому механизм ушёл в Phase 2, а в Phase 4 остались только его применение, форма отказа и дашборд.
2. Обе таблицы создаются одной миграцией `0001_cabber` (data-model §3), поэтому `cabber_session` появляется в Phase 2, хотя первая запись в неё попадает только во время User Story 1.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: два новых Go-модуля и их каркас, пока без runnable-логики

- [X] T001 [P] Create `backend/contracts/go.mod` with `module github.com/Keane81/Cabby/backend/contracts` and `go 1.26.1`
- [X] T002 [P] Create `backend/auth/go.mod` with `module github.com/Keane81/Cabby/backend/auth` and `go 1.26.1`
- [X] T003 [P] Add `backend/contracts/buf.yaml` and `backend/contracts/buf.gen.yaml` emitting `protoc-gen-go` and `protoc-gen-go-grpc` output into `backend/contracts/authpb/` (R-01, R-02)
- [X] T004 Copy the reference proto `specs/003-cabber-auth/contracts/auth.proto` to the canonical `backend/contracts/proto/auth/v1/auth.proto` byte-for-byte — the parity test in T006 needs both files before it can compare them
- [X] T005 [P] Add `backend/contracts/Makefile` with a `proto` target running `buf generate` and a `test` target running `go test ./...`
- [X] T006 [P] Write `backend/contracts/contract_parity_test.go` with `TestCanonicalProtoMatchesRepositoryReference` comparing `backend/contracts/proto/auth/v1/auth.proto` against `specs/003-cabber-auth/contracts/auth.proto` (R-02, quickstart §5)
- [X] T007 Add `backend/auth/Makefile` mirroring `backend/cabby-gateway/Makefile`: targets `run test test-race vet test-integration migrate`, where `test-integration` runs `go test -tags integration ./...` and every integration file additionally skips itself unless `CABBY_AUTH_DB_URL` is set, so `make test` stays runnable without PostgreSQL (plan.md §Testing: env-gate, no testcontainers)
- [X] T008 [P] Add `backend/auth/.env.example` (`CABBY_AUTH_DB_URL`, `CABBY_AUTH_GRPC_PORT=9093`) and `backend/auth/.dockerignore` copied from `backend/cabby-gateway/.dockerignore`
- [X] T009 [P] Add `backend/auth/Dockerfile`: multi-stage `golang:1.26.1-alpine` → `FROM scratch`, `USER 65532:65532`, `EXPOSE 9093 9094`, same shape as `backend/cabby-gateway/Dockerfile`
- [X] T010 [P] Create `backend/auth/cmd/auth/main.go` as a `package main` stub that only logs and exits, so `go build ./...` and `make vet` pass for the module from this point on

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: то, из чего нельзя вынуть ни одно стори: генерация контракта, схема и доступ к ней, криптография, проверка полей, серверная проверка доступа, транспорт с обеих сторон, публикация внешнего контракта и инфраструктура compose

**⚠️ CRITICAL**: ни одно User Story не может начаться, пока эта фаза не закрыта

### Codegen for the auth contract

- [X] T011 Run `make -C backend/contracts proto` and commit the generated `backend/contracts/authpb/auth.pb.go` and `backend/contracts/authpb/auth_grpc.pb.go` (R-01: generated code is committed, so `buf` is not required on every machine)
- [X] T012 Run `go mod tidy` in `backend/contracts` so `backend/contracts/go.mod` and `go.sum` require `google.golang.org/protobuf` and `google.golang.org/grpc`, and confirm the `go_package` option `github.com/Keane81/Cabby/backend/contracts/authpb;authpb` resolves to `backend/contracts/authpb/`

### Storage: schema and migrations (R-05, R-06)

- [X] T013 Create `backend/auth/migrations/0001_cabber.up.sql` with the DDL from data-model.md §3 verbatim: tables `cabber` and `cabber_session`, unique index `cabber_email_key`, partial index `cabber_session_active`, index `cabber_session_expiry`, and `on delete cascade` on `cabber_id` kept for the future deregistration specification
- [X] T014 [P] Create `backend/auth/migrations/0001_cabber.down.sql` dropping both tables in the reverse order
- [X] T015 [P] Create `backend/auth/migrations/embed.go` exposing the SQL files through `//go:embed *.sql`
- [X] T016 Implement the embedded migration runner in `backend/auth/internal/migrate/migrate.go`: table `schema_migration(version, applied_at)`, each migration applied in a transaction under `pg_advisory_lock`, idempotent re-run, and a `migrate` entry point reachable from `make -C backend/auth migrate` (R-06)
- [X] T017 [P] Write `backend/auth/internal/migrate/migrate_test.go` covering first run, second run as a no-op, and `0001_cabber` down-migration restoring an empty schema

### Configuration

- [X] T018 Implement `backend/auth/internal/config/config.go`: `Load() (Config, error)` reading `CABBY_AUTH_DB_URL` and `CABBY_AUTH_GRPC_PORT` (default `9093`), the metrics listener address `:9094` as a code constant rather than an environment value (R-11), and a fast failure when the DSN is missing
- [X] T019 [P] Write `backend/auth/internal/config/config_test.go` for the default gRPC port, the missing-DSN failure and the fixed metrics port

### Password and token primitives (R-04, R-07)

- [X] T020 Implement `backend/auth/internal/password/password.go`: `Params`, `Hash`, `Verify` over `golang.org/x/crypto/argon2` (argon2id, `time=2`, `memory=19 MiB`, `threads=1`, `keyLen=32`, `salt` 16 bytes from `crypto/rand`), PHC output `$argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>`, package defaults overridable by a `Params` argument, and a semaphore admitting at most 2 concurrent hashes (plan.md §Constraints)
- [X] T021 [P] Write `backend/auth/internal/password/password_test.go`: round-trip with test-sized `Params`, wrong password rejected, tampered PHC string rejected, algorithm and parameters read back from the stored string (the rotation path of R-07), and the 2-concurrent-hashes limit
- [X] T022 Implement `backend/auth/internal/token/token.go`: `New()` returning 256 bits from `crypto/rand` as base64url without padding, `Digest(plain)` returning the SHA-256 bytes stored in `token_hash`, and a constant-time comparison helper (R-04)
- [X] T023 [P] Write `backend/auth/internal/token/token_test.go`: length and alphabet of the token, digest determinism, distinct tokens per call, and that the API never returns the digest alongside the plaintext
- [X] T024 [P] Write `backend/auth/internal/password/leak_test.go` (unit): открытое значение пароля не встречается ни в PHC-строке, ни в возвращаемых значениях, ни в отказе `Verify`, а два вызова `Hash` с одним паролем дают разные строки и обе проходят проверку (FR-001, FR-004; утечку через логи, метрики и тела ответов проверяет T086)

### Repository layer (user rule: database access stays behind a repository)

- [X] T025 Define the storage ports in `backend/auth/internal/repo/repo.go`: `CabberRepository` (`Create`, `FindByEmail`), `SessionRepository` (`Create`, `GetByDigest`, `Touch`, `Revoke`), the row structs and the sentinel errors `ErrEmailTaken` and `ErrCabberNotFound`
- [X] T026 Implement `backend/auth/internal/repo/cabber.go` with `pgxpool`: insert returning `id`, and a unique violation on `cabber_email_key` mapped to `ErrEmailTaken` (data-model §7: the index, not the application, resolves two racing registrations)
- [X] T027 Implement `backend/auth/internal/repo/session.go` with `pgxpool`: `Revoke` written as a conditional `update … where revoked_at is null returning id` so a second revoke reports "nothing revoked" instead of a false success (FR-021), and `Touch` updating `last_seen_at` only when the stored value is older than 60 seconds (R-10)
- [X] T028 [P] Write `backend/auth/internal/repo/cabber_integration_test.go` (tag `integration`, skipped unless `CABBY_AUTH_DB_URL` is set): email uniqueness, re-registering an address whose owner can no longer sign in is still rejected (FR-027, SC-011), the unicode-aware `name` length constraint, and that `password_hash` never stores a value equal to the input password
- [X] T029 [P] Write `backend/auth/internal/repo/session_integration_test.go` (same gate): two active sessions for one cabber, revoking one leaves the other readable, a repeated `Revoke` changes nothing, and `Touch` within the 60-second window performs no write

### Field validation shared by registration and login (FR-006, FR-007, R-08)

- [X] T030 Create `backend/auth/internal/service/service.go`: the `Service` struct holding both repositories and a zerolog-логгер, plus an injected `Now func() time.Time` clock that every time-dependent rule reads (R-10: boundaries are tested with a substituted clock), and the `Limits` constants — `name` 1..64, `password` 4..16, `email` ≤ 254
- [X] T031 Create `backend/auth/internal/service/errors.go`: domain error values for validation, `ErrEmailTaken`, `ErrInvalidAccess`, the `Field`/`Reason` pairs (`name`, `email`, `password` × `empty`, `too_long`, `invalid_format`, `too_short`) and the fixed first-defect order `name → email → password` used for `ErrorField` (contracts/auth.proto)
- [X] T032 Create `backend/auth/internal/service/validate.go`: `CanonicalEmail` (trim + lower case, FR-003) and `ValidateRegistration` enforcing lengths by Unicode characters and the minimal email form — exactly one `@` with non-empty sides (FR-006)
- [X] T033 [P] Write `backend/auth/internal/service/validate_test.go` as a table test over `ValidateRegistration`: `a@b`, `ivan@localhost` and `??@!!` accepted (SC-010), email without `@` or with an empty side rejected, passwords of 4 and 16 characters accepted with no weakness signal of any kind (SC-005, FR-007), passwords of 3 and 17 characters rejected with `field=password`, a 65-character `name` rejected, and lengths counted in characters not bytes

### gRPC transport skeleton on the auth side

- [X] T034 Implement `backend/auth/internal/grpcserver/metrics.go`: `cabby_auth_requests_total{method,outcome}`, `cabby_auth_request_duration_seconds{method}` and `cabby_auth_repository_query_total{query,outcome}` on a private `prometheus.Registry` with the fixed label values of R-11, plus the `/metrics` handler for the `:9094` listener
- [X] T035 Implement `backend/auth/internal/grpcserver/interceptors.go`: recover, zerolog request logging in the gateway's field shape (`operation`, `error_class`), and the metrics interceptor from T034
- [X] T036 Implement `backend/auth/internal/grpcserver/server.go`: `Server` embedding `authpb.UnimplementedAuthServiceServer` and holding the `Service`, with `RegisterCabber`, `CreateCabberSession` and `DeleteCabberSession` returning `codes.Unimplemented` until their story fills them in
- [X] T037 [P] Write `backend/auth/internal/grpcserver/server_test.go` over `bufconn` asserting the `UNIMPLEMENTED` baseline for the three methods, so later regressions in method wiring are visible

### Access verification shared by every protected operation

- [X] T038 Implement `Verify` in `backend/auth/internal/service/session.go`: digest the presented token, read the row, apply the invariant `revoked_at is null and expires_at > now() and last_seen_at + 24h > now()` (data-model §2), refresh `last_seen_at` through the 60-second rule, and return the owning `cabber_id` (FR-013, FR-015)
- [X] T039 [P] Write `backend/auth/internal/service/session_test.go` with the injected clock: accepted at 95:59:59 and rejected at 96:00:01 after creation, accepted at 23:59 and rejected at 24:01 of inactivity, revoked token rejected, and every rejection carrying the same `ErrInvalidAccess` so the caller cannot distinguish the causes (SC-009, SC-003)

### Gateway transport and error envelope

- [X] T040 Add `backend/cabby-gateway/internal/authclient/authclient.go`: the `Operations` port (`RegisterCabber`, `CreateCabberSession`, `DeleteCabberSession`), a gRPC client dialing `CABBY_AUTH_ADDR`, a 2-second deadline per call and no retries (R-09), translating gRPC status and `ErrorField` details into the domain results the server package maps
- [X] T041 Extend `backend/cabby-gateway/internal/server/errors.go`: new `ErrorCode` values `invalid_request`, `unauthorized`, `email_taken`, `service_unavailable`, and the new optional `field` in `errorDetail` serialized only when set (data-model §5, §6)
- [X] T042 [P] Write `backend/cabby-gateway/internal/server/errors_mapping_test.go`: every gRPC code maps to exactly the HTTP status, `error.code` and `error.field` of the data-model §6 table, `INTERNAL` yields a fixed 500 with no details, and no response echoes request data (extends the `TestErrorDoesNotLeakRequestData` guarantee, FR-004, FR-016)
- [X] T043 [P] Write `backend/cabby-gateway/internal/authclient/authclient_test.go` with a `bufconn` fake: a 2-second deadline and an unavailable service both surface as the domain "dependency down" result, and the client never retries a `RegisterCabber` (R-09)
- [X] T044 Add `CABBY_AUTH_ADDR=auth:9093` to `backend/cabby-gateway/.env.example`
- [X] T045 Extend `backend/cabby-gateway/cmd/cabby-gateway/main.go`: read `CABBY_AUTH_ADDR`, build the `authclient`, pass it into `server.NewRouter`, and keep the graceful-shutdown ordering; `/healthz` must stay independent of the auth service and of PostgreSQL (plan.md §Constraints, semantics of spec 001)

### External contract publication (FR-023, one atomic changeset per AGENTS.md)

- [X] T046 Edit `backend/cabby-gateway/api/openapi.yaml`: `info.version` → `1.1.0`, paths `POST /cabbers`, `POST /cabber/session`, `DELETE /cabber/session` with the schemas of data-model §5 (`CabberRegistrationRequest`, `Cabber`, `CabberSessionRequest`, `CabberSession`), the extended `ErrorCode` enum and the `400/401/405/409/503` responses
- [X] T047 Copy the updated document to `specs/002-gateway-external-contract/contracts/openapi.yaml` byte-for-byte so `TestCanonicalContractMatchesRepositoryReference` passes
- [X] T048 Update `specs/002-gateway-external-contract/contracts/external-api.md`: the three operations, the new error codes including the optional `error.field`, and a 1.1.0 entry in the version history
- [X] T049 Extend `backend/cabby-gateway/internal/server/contract_test.go`: add the `pathCabbers = "/cabbers"` and `pathCabberSession = "/cabber/session"` constants to the `TestRouterPathsAreDefinedInContract` list, and add the new `TestCabberOperationsAreNotInContract` asserting the contract carries no password change or recovery operation (FR-026, SC-011)
- [X] T050 [P] Update `backend/cabby-gateway/internal/server/contract_version_test.go` to expect `1.1.0` (quickstart §5: the expected value is hardcoded and must change in the same changeset)
- [X] T051 [P] Add `backend/bruno/cabby-gateway/registration.bru` — `meta { name: registration, type: http, seq: 3 }` and `post { url: {{host}}/cabbers }` with a JSON body, following `health-check.bru`
- [X] T052 [P] Add `backend/bruno/cabby-gateway/login.bru` — `post`, `url: {{host}}/cabber/session`, `seq: 4`
- [X] T053 [P] Add `backend/bruno/cabby-gateway/logout.bru` — `delete`, `url: {{host}}/cabber/session`, `seq: 5`, bearer auth

### Compose and monitoring wiring

- [X] T054 Add the `postgres` service to `compose.yaml`: `postgres:18-alpine`, role `auth` and database `auth` created on init, named volume `auth-data`, no published port so it stays inside the compose network (R-05)
- [X] T055 Add the `auth` service to `compose.yaml`: `build: ./backend/auth`, `env_file: ./backend/auth/.env`, `depends_on: postgres`, ports `9093`/`9094` internal only and no `healthcheck` (quickstart §3)
- [X] T056 Add `CABBY_AUTH_ADDR: auth:9093` to the environment of the `cabby-gateway` service in `compose.yaml`
- [X] T057 Add `--env-file backend/auth/.env` to the `COMPOSE` variable in the root `Makefile`
- [X] T058 [P] Add the scrape job `auth` with target `auth:9094` to `deploy/monitoring/prometheus.yml` (R-11)

**Checkpoint**: фундамент готов — модули собираются и vet'ятся, внешний контракт опубликован и тесты gateway зелёные, `make docker-up` поднимает стек и применяет миграцию `0001_cabber` при старте сервиса. Дальше — три стори, каждое проверяется независимо.

---

## Phase 3: User Story 1 - Каббер заводит учётную запись и входит в систему (Priority: P1) 🎯 MVP

**Goal**: каббер регистрируется по `name`/`email`/`password`, учётная запись активна сразу, доступ при этом не выдаётся (FR-010); отдельным входом по email и паролю каббер получает подтверждённый доступ (FR-011).

**Independent Test**: взять нового каббера и по внешнему контракту выполнить регистрацию, затем вход; вход с неверным паролем и вход по незарегистрированному email дают неразличимый отказ (spec.md §US1 Independent Test, сценарии 1–8). Отзыв доступа в этой проверке не участвует: он относится к US3.

### Tests for User Story 1

- [X] T059 [P] [US1] Write `backend/auth/internal/service/register_test.go`: успешная регистрация возвращает `cabber_id` и канонический email и не возвращает никаких данных доступа, `Ivan@Example.com ` и `ivan@example.com` — одна учётная запись, отказ по занятому email не изменяет сохранённую запись и не раскрывает её атрибутов (FR-009, FR-010), невалидный запрос не создаёт частичной записи (FR-008)
- [X] T060 [P] [US1] Write `backend/auth/internal/service/login_test.go` с инъекцией часов: исход `ErrInvalidAccess` для неизвестного email и для неверного пароля один и тот же (FR-012, SC-006), `expires_at` ровно `Now()+96h` (FR-013), два входа порождают два независимых токена (FR-014), и открытые пароль и токен не встречаются ни в одном возвращаемом значении отказа (FR-004)
- [X] T061 [P] [US1] Write `backend/auth/internal/grpcserver/auth_methods_test.go` поверх `bufconn`: `RegisterCabber` отдаёт `INVALID_ARGUMENT` с `ErrorField` первого недостатка в порядке `name → email → password` и `ALREADY_EXISTS` на занятом адресе, `CreateCabberSession` отдаёт `UNAUTHENTICATED` на обоих отказах входа, `NOT_FOUND` не используется ни для одного отказа, и `UNAVAILABLE` пробрасывается при недоступном хранилище (перечень кодов в contracts/auth.proto)
- [X] T062 [P] [US1] Write `backend/cabby-gateway/internal/server/cabber_test.go` с заглушкой `Operations`: `201` с телом `Cabber`, `400 invalid_request` с `error.field`, `409 email_taken`, единый `401 unauthorized`, `503 service_unavailable` при недоступном сервисе и отказ при неизвестных полях тела (`additionalProperties: false`) — при этом ни одно сообщение об ошибке не повторяет данные запроса (SC-002, SC-005)

### Implementation for User Story 1

- [X] T063 [US1] Реализовать `Register` в `backend/auth/internal/service/register.go`: канонизация email (`CanonicalEmail` из T032), проверка `ValidateRegistration`, хеширование через `internal/password`, вставка через `CabberRepository.Create`, `ErrEmailTaken` → доменный отказ «занято», ответ — `cabber_id` и канонический email; сессия не создаётся (FR-005, FR-010)
- [X] T064 [US1] Реализовать `Login` в `backend/auth/internal/service/login.go`: поиск по каноническому email; если каббера нет, всё равно выполнить проверку по фиксированной PHC-заглушке, чтобы время ответа не выдавало существование учётной записи (data-model §7, SC-006); при совпадении — `token.New()`, вставка `cabber_session` с `expires_at = Now()+96h`, ответ с открытым токеном и `expires_at` (FR-011, FR-013)
- [X] T065 [US1] Заменить `Unimplemented` на реальные `RegisterCabber` и `CreateCabberSession` в `backend/auth/internal/grpcserver/server.go`: вызов кейсов T063/T064, маппинг доменных отказов в `INVALID_ARGUMENT` + `ErrorField` в `status.Details()`, `ALREADY_EXISTS` и `UNAUTHENTICATED` (research R-01, contracts/auth.proto)
- [X] T066 [P] [US1] Реализовать методы `RegisterCabber` и `CreateCabberSession` порта `Operations` в `backend/cabby-gateway/internal/authclient/authclient.go`: вызовы с дедлайном 2 с без повторов и перенос `ErrorField` в доменный результат (R-09)
- [X] T067 [US1] Создать `backend/cabby-gateway/internal/server/cabber.go`: хендлеры `POST /cabbers` (`201 Cabber`) и `POST /cabber/session` (`201 CabberSession` c `expires_at` в формате date-time), лимит тела перед разбором JSON, маппинг доменных результатов через таблицу data-model §6 (FR-023, FR-024)
- [X] T068 [US1] Подключить хендлеры в `backend/cabby-gateway/internal/server/router.go`: константы `pathCabbers` и `pathCabberSession` из T049 в `switch`, новый параметр `Operations` в `NewRouter`, для неизвестного метода — существующий `writeMethodNotAllowed`, для неизвестного пути — `writeUnknownOperation` без изменений (поведение спецификации 001/002 сохраняется)
- [X] T069 [P] [US1] Добавить в `backend/cabby-gateway/internal/server/metrics.go` счётчик `cabby_gateway_cabber_requests_total{operation,outcome}` и гистограмму `cabby_gateway_cabber_dependency_duration_seconds{operation}` с фиксированными значениями меток из R-11 и засвеченными нулевыми сериями, как это уже сделано для health-check
- [X] T070 [P] [US1] Расширить `backend/cabby-gateway/internal/server/router_test.go`: `GET /cabbers` и `PUT /cabber/session` дают `405 method_not_allowed` с заголовком `Allow`, `/healthz` и `/openapi.yaml` ведут себя как раньше (SC-008)

**Checkpoint**: US1 работает автономно — регистрация и вход проверяются curl-шагами 1–5 quickstart §4 и `make test test-race vet` в трёх модулях. Подтверждённые критерии: SC-001, SC-005, SC-006, SC-010 и часть SC-002.

---

## Phase 4: User Story 2 - Доступ к своим операциям только после входа (Priority: P2)

**Goal**: личность, подтверждённая входом, становится правилом доступа: операция аккаунта исполняется только от имени владельца, а запрос без подтверждения, с истёкшим или чужим доступом отклоняется единым отказом на сервере (FR-015…FR-018).

**Independent Test**: собрать запросы к операции, требующей подтверждения личности, от аутентифицированного каббера, от неаутентифицированного клиента, с истёкшим доступом и с попыткой затронуть чужой аккаунт; успешными оказываются только запросы владельца (spec.md §US2 Independent Test). В v1 такой операция является выход (FR-017), поэтому проверка выполняется на нём, а сам выход реализуется в US3 — в этой фазе отделены механизм проверки, форма отказа и наблюдаемость.

### Tests for User Story 2

- [X] T071 [P] [US2] Write `backend/cabby-gateway/internal/server/bearer_test.go`: отсутствующий заголовок, чужая схема (`Basic`), пустой токен и токен из другой учётной записи дают тело `401 unauthorized`, побайтово совпадающее с отказом истёкшего доступа (FR-016, SC-003)
- [X] T072 [P] [US2] Write `backend/auth/internal/service/ownership_test.go`: доступ каббера A, предъявленный к операции каббера B, отклоняется тем же `ErrInvalidAccess`, и в отказе нет ни имени, ни `cabber_id` B (FR-018); идентификатор из токена — единственный источник личности, URI в авторизации не участвует (consequence R-03)
- [X] T073 [P] [US2] Write `backend/auth/internal/service/login_timing_test.go`: медианы времени ответа для «email не найден» и «пароль неверен» различаются не более чем на допуск теста, потому что отрыв проверяется на фиксированной заглушке `password.Verify` (SC-006)

### Implementation for User Story 2

- [X] T074 [US2] Создать `backend/cabby-gateway/internal/server/bearer.go`: разбор `Authorization: Bearer <token>` с отказом в единый `401 unauthorized` на отсутствующем заголовке, неизвестной схеме и пустом токене — до обращения к сервису, и передача токена в `DeleteCabberSessionRequest` (R-03, FR-016)
- [X] T075 [US2] Добавить `RequireCabber` в `backend/auth/internal/service/session.go`: обёртка над `Verify` (T038), возвращающая `cabber_id` владельца или `ErrInvalidAccess`, — единственная точка, через которую кейсы защищённых операций узнают личность (FR-015)
- [X] T076 [US2] Создать `deploy/monitoring/grafana/dashboards/auth.json`: RPS и доли отказов по `operation ∈ {register, login, logout}`, p50/p95 задержки `gateway → auth` и задержки репозитория, счётчик `unauthorized` как индикатор перебора и отражение недоступности через `up` по образцу дашборда 001 (R-11, quickstart §6)

**Checkpoint**: US1 и US2 работают независимо друг от друга; форма отказа доступа и её наблюдаемость проверены (SC-003, SC-006, SC-009 на уровне модульных тестов).

---

## Phase 5: User Story 3 - Каббер выходит из системы (Priority: P3)

**Goal**: выход отзывает ровно тот доступ, с которого он отправлен (FR-019); повторный выход не возвращает ложный успех, а прочие доступы каббера остаются действующими (FR-014, FR-021).

**Independent Test**: войти, выйти, попробовать прежний доступ, войти заново; затем проверить, что второй доступ того же каббера не затронут (spec.md §US3 Independent Test, сценарии 1–4).

### Tests for User Story 3

- [X] T077 [P] [US3] Write `backend/auth/internal/service/logout_test.go`: отзыв меняет состояние только предъявленного токена (FR-019), второй выход с ним же даёт `ErrInvalidAccess` и не изменяет `revoked_at` существующей записи (FR-021, SC-007), параллельный доступ того же каббера продолжает проходить `RequireCabber` (FR-014), новый вход после выхода выдаёт действующий токен (SC-004)
- [X] T078 [P] [US3] Write `backend/auth/internal/repo/logout_integration_test.go` (tag `integration` + DSN gate): два созданных доступа, отзыв одного через `Revoke`, второй читается действующим, повторный `Revoke` не возвращает строк — то есть ложного успеха нет на уровне SQL (data-model §8 «Независимость доступов»)
- [X] T079 [P] [US3] Расширить `backend/cabby-gateway/internal/server/cabber_test.go`: `DELETE /cabber/session` с валидным токеном даёт `204` без тела, отказ `Operations` даёт единый `401`, и значение заголовка `Authorization` не попадает ни в ответ, ни в перехваченный лог (FR-004, SC-002)

### Implementation for User Story 3

- [X] T080 [US3] Реализовать `Logout` в `backend/auth/internal/service/logout.go`: `RequireCabber` (T075) → `SessionRepository.Revoke`; если отзыв не затронул ни одной строки, вернуть `ErrInvalidAccess`, чтобы успех означал «отозван именно этим запросом» (FR-021)
- [X] T081 [US3] Заменить `Unimplemented` на `DeleteCabberSession` в `backend/auth/internal/grpcserver/server.go`: токен из тела запроса, вызов `Logout`, пустой `DeleteCabberSessionResponse` при успехе и `UNAUTHENTICATED` при любом отказе доступа (перечень кодов в contracts/auth.proto)
- [X] T082 [US3] Замкнуть путь выхода: метод `Logout` в `backend/cabby-gateway/internal/authclient/authclient.go`, хендлер `DELETE /cabber/session` в `backend/cabby-gateway/internal/server/cabber.go` (разбор заголовка через `bearer.go` из T074, ответы `204`/`401`/`503`) и ветка `http.MethodDelete` для `pathCabberSession` в `backend/cabby-gateway/internal/server/router.go`

**Checkpoint**: все три стори работают независимо; quickstart §4 шаги 5–6 (вход, выход, повторный выход) и §3 проверки стека зелёные, SC-004 и SC-007 подтверждены.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: доработки, которые относятся сразу ко всем стори, и финальная сверка с quickstart

- [X] T083 [P] Создать `backend/auth/internal/service/cleanup.go`: фоновый тикер раз в час удаляет строки `cabber_session`, у которых `expires_at` или `revoked_at` старше 7 дней (R-10, инвариант data-model §2), и только их — записи `cabber` не удаляются ни при каких условиях (FR-028)
- [X] T084 [P] Write `backend/auth/internal/service/cleanup_test.go` с инъекцией часов: свежие истёкшие доступы не удаляются, старый отозванный удаляется, запись каббера остаётся
- [X] T085 Перенести `request_id` через границу сервисов: генерация на входе gateway и передача в gRPC-metadata из `backend/cabby-gateway/internal/authclient/authclient.go`, чтение и логирование в `backend/auth/internal/grpcserver/interceptors.go`; имя ключа метаданных зафиксировать константой в обоих модулях — это закрывает разрыв CHK034 чек-листа `naming-and-stack.md` (Complexity Tracking: замена трассировки)
- [X] T086 [P] Написать `backend/auth/internal/grpcserver/leak_test.go` и расширить `backend/cabby-gateway/internal/server/metrics_test.go`: в перехваченных логах, именах и значениях метрик и телах ответов нет подстрок `password`, `access_token` и значения `email` (SC-002, FR-004, FR-025)
- [X] T087 [P] Написать `backend/cabby-gateway/internal/server/absent_operations_test.go`: `POST /cabbers/password`, `POST /cabbers/password/recovery` и `POST /cabber/session/recover` дают `404 unknown_operation` без побочного эффекта (FR-026, SC-011)
- [X] T088 Прогнать `specs/003-cabber-auth/quickstart.md` целиком: §2 конфигурация, §3 поднятие стека и `info.version 1.1.0`, §4 шаги 1–8 включая `503 service_unavailable` при остановленном `postgres`, §5 проверки кода, §6 дашборды и grep на утечки, §7 ожидания под нагрузкой
- [X] T089 Запустить `make test test-race vet` в `backend/auth` и `backend/cabby-gateway` и `go test ./...` в `backend/contracts`; при заданном `CABBY_AUTH_DB_URL` — `make -C backend/auth test-integration` (plan.md §Testing, AGENTS.md §Правила для агентов)
- [X] T090 [P] Если реализация разошлась с артефактами, синхронизировать их: команды и пути в `specs/003-cabber-auth/quickstart.md`, текстовые описания портов и переменных в `specs/003-cabber-auth/plan.md`, руководство `specs/002-gateway-external-contract/contracts/external-api.md`
- [X] T091 [P] Пересверить `specs/003-cabber-auth/checklists/naming-and-stack.md` по готовому коду (пункты CHK014, CHK037, CHK043) и `checklists/implementation-readiness.md` (CHK004, CHK005, CHK015, CHK026): метки `[x]` выставляет ревьюер, агент их не меняет
- [X] T092 Заменить заглушку `backend/auth/cmd/auth/main.go` на настоящую сборку: `config.Load` → `pgxpool.New` → `migrate.Load`+`migrate.UpWaiting` → репозитории `repo.NewCabbers`/`repo.NewSessions`, обёрнутые в `Metrics.Cabbers`/`Metrics.Sessions` → `service.New` → `grpc.NewServer` с `metrics.UnaryInterceptor` и `Server.Register` на `config.GRPCAddress`, HTTP-сервер `metrics.Handler()` на `config.MetricsAddress`, корректная остановка по SIGINT/SIGTERM и флаг `-migrate` (применить миграции и выйти). T010 создаёт файл как заглушку, а compose (`auth.build`) и quickstart §3 требуют слушающие порты 9093/9094 — ни одна задача Phase 1…5 этого не касалась

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: стартует сразу; T004 предшествует T006 (тесту нужны оба файла), остальное параллельно
- **Foundational (Phase 2)**: зависит от Setup; блокирует все стори. Внутри — порядок: контракты (T011/T012) → схема и миграции (T013…T017) → конфиг (T018/T019) → криптография (T020…T024) → репозиторий (T025…T029) → валидация (T030…T033) → gRPC-каркас (T034…T037) → проверка доступа (T038/T039) → транспорт gateway (T040…T045) → публикация контракта (T046…T053) → compose и мониторинг (T054…T058)
- **User Stories (Phases 3–5)**: каждое зависит только от Phase 2. US2 и US3 опираются на вход из US1 как на источник токена, но по коду независимы: их можно собирать в любом порядке и проверять на заглушке `Operations`
- **Polish (Phase 6)**: после тех стори, которые должны быть выпущены

### User Story Dependencies

- **US1 (P1)**: не зависит от других стори — это MVP
- **US2 (P2)**: механизм `Verify` уже в Phase 2 (см. допущение 1 в шапке); фаза добавляет форму отказа, применение в защищённых кейсах и дашборд
- **US3 (P3)**: требует `RequireCabber` из US2 и `token`/`cabber_session` из Phase 2; самостоятельная ценность — отзыв доступа

### Within Each User Story

- Тесты стори пишутся до реализации и должны падать (plan.md §Constitution Check, принцип V)
- Кейс сервиса → метод gRPC → клиент gateway → хендлер → роутер
- Задачи, трогающие один файл, не помечены `[P]` и идут последовательно

### Parallel Opportunities

- Внутри Phase 1: T001, T002, T003, T005, T006, T008, T009, T010
- Внутри Phase 2: криптография и токены (T020…T024) параллельно миграциям (T013…T017); интеграционные тесты репозитория (T028/T029) параллельно валидации (T033); задачи Bruno (T051…T053) — три независимых файла
- После Phase 2 стори US1/US2/US3 могут идти параллельно разными разработчиками, кроме точек пересечения: `cabber.go`, `router.go`, `server.go` (auth-сервис), `authclient.go` и `session.go` правятся последовательно
- Внутри фаз: тестовые файлы каждой фазы (`*_test.go`) параллельны друг другу

---

## Parallel Example: User Story 1

```bash
# Тесты US1 пишем первыми, все четыре — разные файлы:
Task: "backend/auth/internal/service/register_test.go"
Task: "backend/auth/internal/service/login_test.go"
Task: "backend/auth/internal/grpcserver/auth_methods_test.go"
Task: "backend/cabby-gateway/internal/server/cabber_test.go"

# После кейсов сервиса реализация идёт тремя параллельными ветками:
Task: "backend/cabby-gateway/internal/server/metrics.go (T069)"
Task: "backend/cabby-gateway/internal/authclient/authclient.go (T066)"
Task: "backend/cabby-gateway/internal/server/router_test.go (T070)"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Phase 1 → Phase 2 (фундамент обязателен: без контракта, схемы и криптографии ни одна операция не собирается)
2. Phase 3: регистрация и вход
3. **Остановиться и проверить**: quickstart §3 и §4 шаги 1–5, SC-001, SC-005, SC-006, SC-010
4. Готово — каббер существует в системе и доказывает личность; наружу опубликован контракт 1.1.0

### Incremental Delivery

1. Setup + Foundational → стек поднимается, контракт опубликован, тесты зелёные
2. US1 → регистрация и вход работают, проверка на curl и в Bruno (seq 3, 4) → релиз
3. US2 → единая форма отказа доступа и дашборд → релиз; существующие операции не затронуты (SC-008)
4. US3 → выход и его отзыв (quickstart §4 шаг 6, Bruno seq 5) → релиз
5. Phase 6 → клинер, `request_id`, тесты на утечки и на отсутствие операций восстановления

### Rollback

Откат — вернуть `cabby-gateway` на контракт 1.0.0: новые пути становятся `unknown_operation`, сервис `auth` и схема `cabber`/`cabber_session` остаются без изменений (plan.md §«Порядок развёртывания и откат»). Оба шага аддитивны, миграции данных нет, поэтому SC-008 выполняется и после отката.

### Parallel Team Strategy

1. Один разработчик ведёт Phase 1 + Phase 2 (контракт и схема — общая точка согласования)
2. После Phase 2: разработчик A — US1, B — US2, C — US3; пересекающиеся файлы (`router.go`, `cabber.go`, `server.go`, `authclient.go`) — за A, чтобы избежать конфликтов, либо последовательно по одному PR
3. Phase 6 распределяется после слияния стори

---

## Notes

- `[P]` = разные файлы и нет незавершённых зависимостей; задачи без `[P]` в одной фазе могут требовать порядка из-за общего файла
- Каждая задача называет точный путь; ни одна не требует дополнительного контекста сверх перечисленных в шапке документов
- Обязательные гейты перед `$speckit-implement`: `checklists/requirements.md` — все пункты закрыты; `checklists/implementation-readiness.md` — открыты CHK004, CHK005, CHK015, CHK026; `checklists/naming-and-stack.md` — из `(pre-tasks)` открыты CHK014, CHK037, CHK043, CHK046 (CHK046 перечисляет незакрытые решения CHK010, CHK033, CHK034; CHK034 закрывает T085)
- Решения, которые чек-листы оставляют открытыми к моменту `implement`: код ошибки для `500` (CHK010) — нужен в T041; bucket'ы гистограмм (CHK033) — нужны в T034 на стороне `auth` и в T069 на стороне gateway; имя metadata-ключа `request_id` (CHK034) — нужно в T085. Фиксировать их до правки кода, а не выдумывать в реализации
- Коммитить по задаче или по логической группе, сообщения — Conventional Commits на английском, область `backend` или `contracts` (AGENTS.md §Сообщения коммитов)
- Избегать соблазна «улучшить по пути»: дерегистрация, смена и восстановление пароля, подтверждение email и распределённая трассировка вне объёма (FR-026…FR-028, Complexity Tracking)
