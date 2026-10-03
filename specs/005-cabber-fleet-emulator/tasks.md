# Tasks: Эмулятор парка кабберов для нагрузочного прогона

**Input**: Design documents from `specs/005-cabber-fleet-emulator/`

**Prerequisites**: plan.md, spec.md (User Stories 1–3), research.md (R-01…R-10), data-model.md, contracts/cli.md, quickstart.md

**Tests**: test tasks MANDATORY. Принцип V конституции требует проверяемых критериев на каждое изменение, а `make check` (`test`, `test-race`, `vet`) обязан быть зелёным. Логику проверяем на фейковом gateway (`httptest`), нагрузку — сквозным прогоном по quickstart.

**Organization**: фаза 1 — каркас модуля; фаза 2 — то, без чего не стартует ни одна история (профиль и его проверка, клиент gateway, счётчики и гистограмма); фазы 3–5 — три истории спецификации по приоритету; фаза 6 — сквозные проверки и замеры.

**Paths**: все пути — от корня репозитория, как в plan.md §Project Structure. Go-тесты лежат рядом с кодом (`_test.go`). Модуль — `github.com/Keane81/Cabby/backend/emulator`, `go 1.26.1`. Ни `contracts`, ни `platform`, ни `lifecycle` модуль не подключает (plan.md, Complexity Tracking). Публичный контракт gateway не меняется, поэтому OpenAPI, Bruno и smoke-коллекция не правятся.

**Правило запуска**: агент не запускает эмулятор более чем с одним каббером ни на живом стеке, ни на заглушке. Запуски с `-cabbers` больше 1 выполняет только пользователь; задачи с пометкой **[MANUAL]** агент не выполняет, а готовит команду и описывает, что записать по результату. Проверки на живом стеке самим агентом — только с `-cabbers 1` (T024). Нагрузочный тест `scale_test.go` (T030) вынесен под build-тег `load` и не входит в `make check`.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: можно параллельно (разные файлы, нет зависимости от незавершённых задач)
- **[US1]/[US2]/[US3]**: принадлежит соответствующей User Story
- **[MANUAL]**: выполняет пользователь, агент не запускает

---

## Phase 1: Setup

**Purpose**: пустой каркас модуля и подключение к `make check`

- [X] T001 [P] Create `backend/emulator/go.mod` with `module github.com/Keane81/Cabby/backend/emulator`, `go 1.26.1` and the single dependency `github.com/rs/zerolog` (same version as `backend/cabby-gateway/go.mod`); no `replace` directives
- [X] T002 [P] Add `backend/emulator/Makefile` with targets `run test test-race vet`; `run` forwards `ARGS` to `go run ./cmd/emulator $(ARGS)` (no `.env`, no `migrate`, no `test-integration`: the module has no database)
- [X] T003 [P] Create `backend/emulator/cmd/emulator/main.go` as a `package main` stub that only exits 0, so `go build ./...` and `make vet` pass from now on
- [X] T004 Add `emulator` to `MODULES` in `backend/Makefile` and verify `make -C backend check` runs the new module

---

## Phase 2: Foundational (blocks all user stories)

**Purpose**: профиль с проверкой, клиент публичного REST-контракта и показатели — общий фундамент всех историй

- [X] T005 [P] Implement the `Profile` type, defaults and validation in `backend/emulator/internal/config/profile.go` per data-model.md §Profile: bounds for `Cabbers` (1…50 000), `Interval` (≥ 100 ms), `RampUp` default `max(10s, Cabbers×25ms)`, `Area` (ranges, both sides ≥ 1 km), `Instances/Instance`; a validation error names the field and the reason and the profile is rejected as a whole (FR-008, FR-009)
- [X] T006 [P] Implement target-address safety in `backend/emulator/internal/config/target.go`: `Target` is accepted without `-allow-remote` only for `127.0.0.0/8`, `::1`, `localhost` and `*.localhost`; any other host without the flag is an error before the first request (FR-016, R-08)
- [X] T007 Implement flag parsing in `backend/emulator/internal/config/flags.go`: every parameter from contracts/cli.md with its default, `-h` text with defaults and an example (FR-017), random `Seed` and `RunID` printed at start; parse errors map to exit code 2 (depends on T005, T006)
- [X] T008 [P] Add tests in `backend/emulator/internal/config/profile_test.go` and `target_test.go`: defaults, each boundary (0, negative, above maximum, zero interval, degenerate area), derived default ramp-up for 1 000 and 50 000 cabbers, loopback accepted, `example.com` and `10.0.0.5` refused without the flag and accepted with it
- [X] T009 [P] Implement the gateway client in `backend/emulator/internal/gateway/client.go`: `Register`, `Login`, `RecordLocation`, `Logout` over one shared `http.Transport` (HTTP/1.1, `MaxConnsPerHost = Profile.MaxConns`, keep-alive, no compression), request timeouts 10 s for register/login/logout and 5 s for location (R-02, R-04); bodies match openapi.yaml 1.2.0 (`name/email/password`, `latitude/longitude`), unknown response fields are ignored (principle II)
- [X] T010 Implement outcome classification in `backend/emulator/internal/gateway/outcome.go`: map a response or error to `ok`, `unauthorized` (401), `invalid_request` (400), `conflict` (409 on register), `unavailable` (5xx), `timeout`, `network`; the `Retryable` rule is `unavailable|timeout|network` only; no response body, token or coordinate is ever copied into an error value (FR-015) (depends on T009)
- [X] T011 [P] Add tests in `backend/emulator/internal/gateway/client_test.go` on an `httptest` server: success paths of all four operations, each status → outcome mapping, timeout classification, extra response fields tolerated, request bodies contain exactly the contract fields
- [X] T012 [P] Implement counters in `backend/emulator/internal/stats/counters.go` (atomic: registered, logged_in, register_failed, login_failed, relogin, active, sent_ok, sent_failed by kind, skipped, logged_out, logout_failed) and the fixed-bucket latency histogram in `backend/emulator/internal/stats/histogram.go` (64 log buckets 1 ms…30 s, p50/p95/p99/max, no allocation on record) per R-06
- [X] T013 [P] Add tests in `backend/emulator/internal/stats/stats_test.go`: percentiles on known samples within one bucket of error, overflow bucket, concurrent recording under `-race`, snapshot is internally consistent
- [X] T014 Wire a `zerolog` logger created in `main` as `zerolog.New(os.Stdout).With().Timestamp().Logger()` (passed by value, no globals) and write the start/stop messages `"emulator starting"`, `"emulator stopped"`, `"emulator stopped with error"` in `backend/emulator/cmd/emulator/main.go`; start message carries the profile without secrets, `run_id` and `seed`

**Checkpoint**: профиль проверяется, клиент общается с gateway по контракту, показатели считаются — можно писать истории.

---

## Phase 3: User Story 1 — Один эмулируемый каббер проходит рабочий цикл (P1)

**Goal**: один каббер регистрируется, входит, отправляет правдоподобно меняющиеся координаты и выходит по остановке или по истечении длительности.

**Independent Test**: `make emulate ARGS="-cabbers 1 -interval 1s -duration 1m"` против чистого стека: в `location` ≈ 60 строк, в сводке один запущенный каббер, ноль ошибок, один выход (quickstart §2).

### Tests for User Story 1 (MANDATORY)

- [X] T015 [P] [US1] Add tests in `backend/emulator/internal/route/walk_test.go`: all points stay inside `Area` after 100 000 steps, the distance between neighbours never exceeds `speed × interval` (with the projection tolerance), same seed gives the same sequence and different indices give different ones, the border reflects the heading instead of sticking to it (FR-005)
- [X] T016 [P] [US1] Add tests in `backend/emulator/internal/cabber/cabber_test.go` on a fake gateway: the happy path `Registering → LoggingIn → Active → Done`, coordinates are sent after the login only, the interval is respected with an injected clock, cancellation stops sending at once and logs out, the identity (email, name, password) follows data-model.md and is deterministic by `seed/run-id/index`
- [X] T017 [P] [US1] Add tests in `backend/emulator/internal/cabber/leak_test.go`: run a cabber at debug level into a buffer and assert no password, token, email or coordinate value appears in any log line (FR-015)

### Implementation for User Story 1

- [X] T018 [P] [US1] Implement the random walk in `backend/emulator/internal/route/walk.go` per R-05: per-cabber PRNG seeded from `seed + index`, speed 20–50 km/h constant per run, heading turn N(0, 20°), equirectangular distance conversion, reflection at the border, rounding to 7 decimals
- [X] T019 [US1] Implement deterministic identity in `backend/emulator/internal/cabber/identity.go`: email `emu-<run-id>-<index>@emulator.cabby.test`, name `emu-<index>`, 12-character password derived from `seed`, `run-id`, `index` (never printed) (depends on T005)
- [X] T020 [US1] Implement the single-cabber lifecycle in `backend/emulator/internal/cabber/cabber.go`: states and transitions from data-model.md (this story covers `Waiting → Registering → LoggingIn → Active → Stopped → LoggingOut → Done` and `Failed` without retries), a `Now` function and a `Sleep` seam injected for tests, stats updated on every outcome (depends on T009, T010, T012, T018, T019)
- [X] T021 [US1] Implement a minimal runner in `backend/emulator/internal/fleet/run.go` that starts the profile's cabbers (for this story, every cabber immediately), stops on context cancellation or after `Duration`, waits for logout and returns the stats snapshot (depends on T020)
- [X] T022 [US1] Wire signals and the final text summary in `backend/emulator/cmd/emulator/main.go`: `SIGINT/SIGTERM` cancel the run, a second signal exits at once, the summary prints started/failed counts, sent ok/failed, logged out (the full report is US3) (depends on T007, T014, T021)
- [X] T023 [US1] Add the root `Makefile` target `emulate`: take `-target` from the `host:` line of `backend/bruno/cabby-gateway/environments/LOCAL.bru` (same `sed` as `smoke`) unless `ARGS` already contains `-target`, then `cd backend/emulator && go run ./cmd/emulator $(ARGS)`; add `emulate` to `.PHONY`
- [X] T024 [US1] Verify against a live clean stack (`make docker-up-clean`, `make emulate ARGS="-cabbers 1 -interval 1s -duration 1m"`): ≈ 60 rows in `location`, the session is revoked at the end; record the observation in the PR description

**Checkpoint**: US1 работает самостоятельно — это MVP.

---

## Phase 4: User Story 2 — Эмуляция большого числа кабберов одновременно (P1)

**Goal**: до 50 000 кабберов входят по расписанию разгона и отправляют координаты параллельно; сбои не останавливают прогон; повторный запуск на заполненной базе проходит без конфликтов.

**Independent Test**: прогон на 1 000 кабберов за несколько минут: все активны к концу разгона, суммарная частота в пределах 10% от заданной (quickstart §3, §4, §6).

### Tests for User Story 2 (MANDATORY)

- [X] T025 [P] [US2] Add tests in `backend/emulator/internal/fleet/ramp_test.go` with an injected clock: cabber `i` becomes eligible at `i × ramp-up / N`, no more than 8 register/login requests are in flight at once, a ramp-up that `auth` cannot follow delays the cabbers instead of bursting and the on-schedule ratio is reported (R-03)
- [X] T026 [P] [US2] Add tests in `backend/emulator/internal/fleet/schedule_test.go`: send phases are spread evenly over `[0, interval)`, missed slots are dropped and counted as `skipped` rather than sent in a burst, the next send lands on the nearest future slot of the grid (R-04)
- [X] T027 [P] [US2] Add tests in `backend/emulator/internal/cabber/resilience_test.go` on a fake gateway: a failed location send is never retried and the next point goes on schedule (Clarifications); `401` on send leads to a new login and sending resumes (FR-012); register/login retry only on `unavailable|timeout|network` with 1/2/4 s jittered pauses and give up after three attempts as `Failed`; `409` on register retries once with a new suffix; three failed re-logins end in `Lost`
- [X] T028 [P] [US2] Add tests in `backend/emulator/internal/fleet/shutdown_test.go`: on a signal sending stops immediately, logout runs with concurrency 256 and a 10 s overall deadline, sessions that missed the deadline are counted as `not_logged_out`, a second signal returns at once
- [X] T029 [P] [US2] Add tests in `backend/emulator/internal/fleet/instances_test.go`: instance `j` of `k` owns exactly the indices `j, j+k, …` of `N`, all instances together cover every index once, email and password agree across instances for the same `run-id` and `seed`
- [X] T030 [P] [US2] Add a scale test in `backend/emulator/internal/fleet/scale_test.go` under the build tag `load` (not run by `make check`; the user starts it with `go test -tags load`): 5 000 cabbers against a local `httptest` server, steady state reaches the target rate within 10%, no goroutine leak after the run (`runtime.NumGoroutine` returns to the baseline), clean under `-race`

### Implementation for User Story 2

- [X] T031 [US2] Implement the register/login limiter and the ramp-up scheduler in `backend/emulator/internal/fleet/ramp.go`: a semaphore of 8 (internal constant, not a flag), the linear eligibility schedule, and the measurements `time_to_full` and `on_schedule_ratio` (R-03) (depends on T021)
- [X] T032 [US2] Implement the send-phase scheduler in `backend/emulator/internal/fleet/schedule.go`: a random phase per cabber, a slot grid based on the previous slot (not on `now + interval`), skipped-slot accounting, `dispatch_lag` measured before the network call and recorded in a stats histogram (R-04, R-06)
- [X] T033 [US2] Extend `backend/emulator/internal/cabber/cabber.go` with the full state machine of data-model.md: login retries with jitter, one re-registration on `409`, re-login on `401` through the shared limiter, the `Failed` and `Lost` end states, no retry of location sends (depends on T031, T032)
- [X] T034 [US2] Implement the stop phase in `backend/emulator/internal/fleet/shutdown.go`: stop sending on the first signal, logout with concurrency 256 and a 10 s deadline, count `not_logged_out`, immediate return on the second signal (R-04)
- [X] T035 [US2] Implement instance partitioning in `backend/emulator/internal/fleet/instances.go` (indices `j, j+k, …`) and use it in `run.go`; the ramp-up schedule stays global by the cabber index (R-09)
- [X] T036 [US2] Raise `RLIMIT_NOFILE` at start and fail with a clear error if `max-conns + 64` descriptors are not available, in `backend/emulator/internal/config/rlimit_unix.go` (build tag `unix`; a no-op file for other platforms) (R-02)
- [X] T037 [US2] Replace the minimal runner in `backend/emulator/internal/fleet/run.go` with the full one: phases `ramp-up → steady → stopping`, a run context with `Duration`, and wiring of T031–T035
- [ ] T038 [US2] [MANUAL] Measure the password-hashing bottleneck on the live stack and record the numbers in `specs/005-cabber-fleet-emulator/research.md` R-03 (register+login per second at 100, 1 000, 5 000 cabbers); if the measured rate differs from 70–130 hashes/s by more than 2×, adjust the default ramp-up formula in `backend/emulator/internal/config/profile.go` and its test
- [ ] T039 [US2] [MANUAL] Measure the emulator's own footprint at 1 000, 10 000 and 50 000 cabbers against a local `httptest` stub (RSS, CPU, goroutines) and record them in research.md R-02; confirm or correct the ≤ 1 GiB / ≤ 1.5 cores target

**Checkpoint**: US2 работает: парк входит по расписанию, переживает сбои и останавливается чисто.

---

## Phase 5: User Story 3 — Управление профилем нагрузки и наблюдение за результатом (P2)

**Goal**: живые показатели во время прогона, итоговая сводка в JSON для сравнения прогонов, самоконтроль генератора, предупреждение об объёме данных.

**Independent Test**: два прогона с разными параметрами дают разные фактические частоты и сопоставимые сводки (quickstart §5, §7).

### Tests for User Story 3 (MANDATORY)

- [X] T040 [P] [US3] Add tests in `backend/emulator/internal/report/live_test.go`: the live line has the format from contracts/cli.md, the rate is computed over a sliding window of 5 s, error kinds are listed only when non-zero, the phase is shown
- [X] T041 [P] [US3] Add tests in `backend/emulator/internal/report/summary_test.go`: the JSON matches the schema of contracts/cli.md (fields from the example are present, no password, token, email or coordinate anywhere), the file is written to `-report`, and unknown keys are additive
- [X] T042 [P] [US3] Add tests in `backend/emulator/internal/report/verdict_test.go`: `generator_saturated` when `dispatch_lag` p99 > 250 ms or the actual rate is more than 10% below the target while system latency is low; `system_saturated` when latency or errors are high; `ok` otherwise (FR-014)
- [X] T043 [P] [US3] Add tests in `backend/emulator/internal/report/estimate_test.go`: rows per hour and GB for 200/s, 2 000/s and 10 000/s with 175 bytes per row; the warning triggers above 10 GB for the requested `Duration` (R-07)
- [X] T044 [P] [US3] Add tests in `backend/emulator/cmd/emulator/exit_test.go`: exit codes 0, 1, 2, 3 of contracts/cli.md (invalid profile → 2 and nothing sent to the target, unreachable target at start → 1, `generator_saturated` → 3)

### Implementation for User Story 3

- [X] T045 [P] [US3] Implement the live line printer in `backend/emulator/internal/report/live.go` (every 5 s to stderr, one line per tick)
- [X] T046 [P] [US3] Implement the database growth estimate in `backend/emulator/internal/report/estimate.go` and print it with the start message, with a warning above 10 GB (R-07)
- [X] T047 [US3] Implement the self-check verdict in `backend/emulator/internal/report/verdict.go` using the rate, the latency and the `dispatch_lag` histogram (R-06)
- [X] T048 [US3] Implement the final summary (text to stdout and JSON to `-report`, default `emulator-report-<run-id>.json`) in `backend/emulator/internal/report/summary.go` per contracts/cli.md; replace the temporary summary of T022 (depends on T047)
- [X] T049 [US3] Wire exit codes 0/1/2/3 and the start-up reachability check (`GET /healthz` of the target) in `backend/emulator/cmd/emulator/main.go`; an invalid profile exits with 2 before any request (FR-009) (depends on T048)
- [ ] T050 [US3] [MANUAL] Run two profiles with different `-cabbers` and `-interval` on the live stack and compare the JSON reports with the `jq` command of quickstart §7; record any gap between the documented and the actual output in the PR description

**Checkpoint**: все три истории работают независимо и вместе.

---

## Phase 6: Polish & Cross-Cutting

- [X] T051 [P] Add `backend/emulator/README.md` (≤ 40 lines): what the tool is, the one-line launch command, the link to `specs/005-cabber-fleet-emulator/quickstart.md` and `contracts/cli.md`
- [ ] T052 [MANUAL] Walk through every section of `quickstart.md` on a clean stack (`make docker-up-clean`, `make smoke`, then §2…§7) and fix the document where reality differs; record the stepped highload run (1 000 → 50 000 cabbers) and the result of `verdict` for each step in `research.md` as the first measured load profile of the system
- [ ] T053 [MANUAL] Decide SC-004 for large parks on the measured logout throughput (R-04 risk): if 50 000 logouts do not fit in 10 s, update SC-004 in `spec.md` and the shutdown description in `plan.md`/`research.md` to "sending stops within 10 s, logout is best-effort within the same deadline"
- [ ] T054 Run `make check` from the repository root (`test`, `test-race`, `vet` over every backend module including `emulator`) and `gofmt -l backend/emulator`; fix everything they report
- [X] T055 Final review against AGENTS.md: the Go module follows the allowed deviations of plan.md only (no gRPC/DB/metrics), logging follows the rules (zerolog by value, fixed messages, no secrets), commit messages are Conventional Commits in English split by meaning

---

## Dependencies & Execution Order

- **Phase 1 → Phase 2 → Phases 3–5 → Phase 6.** Phase 2 blocks every story.
- **US1 (P1)** — MVP, depends only on Phase 2. **US2 (P1)** extends the runner and the cabber of US1 (T020, T021 → T031–T037). **US3 (P2)** needs the stats from Phase 2 and the full run of US2 for meaningful figures, but its report code can be written against the counters alone.
- Inside a story: tests first (they fail), then implementation; `route`, `report/*`, `config/*` and `stats/*` have no cross-dependencies and run in parallel.
- Measurements T038, T039, T052 and the decision T053 are **[MANUAL]**: the user runs them on the live stack after the implementation they measure. The agent prepares the exact commands and the place in `research.md` where the numbers go, and does not start the emulator with more than one cabber.

### Parallel opportunities

- Phase 1: T001, T002, T003 together.
- Phase 2: T005, T006, T009, T012 together; their tests T008, T011, T013 in parallel after them.
- US1: T015, T016, T017 and T018 together.
- US2: T025–T030 (all test files differ) together; T034, T035, T036 together once T031–T033 are done.
- US3: T040–T044 together; T045 and T046 together.

## Implementation Strategy

1. **MVP**: Phases 1–3. One cabber through the full lifecycle with a summary: SC-001 passes and the real contract is proved end to end.
2. **Scale**: Phase 4. The code and its tests (including the fake-gateway tests of T025–T029) are written by the agent; the user raises the park (100 → 1 000 → 5 000) and stops at the first measurable limit; T038 and T039 record where the limits are before the 50 000 run.
3. **Observability**: Phase 5. The JSON reports turn the runs into comparable results; without them highload runs cannot be compared.
4. **Close**: Phase 6. Quickstart on a clean stack, SC-004 decision, `make check`.

Коммиты разбиваются по смыслу (AGENTS.md): каркас и `MODULES`; профиль и клиент gateway; маршрут и каббер (US1) вместе с целью `emulate`; парк и разгон (US2); отчётность (US3); документация. Сообщения — Conventional Commits на английском, область `backend`.
