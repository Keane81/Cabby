---
description: "Implementation tasks for cabby-gateway health-check and RPS dashboard"
---

# Tasks: cabby-gateway health-check и график RPS

**Input**: Design documents from `specs/001-gateway-health-monitoring/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Включены проверки HTTP-контракта, учёта вызовов и сценариев отказа: их требует конституция проекта и план. Тесты сценария пишутся до соответствующей реализации.

**Organization**: Задачи сгруппированы по двум пользовательским историям. `[P]` означает, что задачу можно выполнять параллельно с соседними задачами после завершения её входных зависимостей.

## Format: `[ID] [P?] [Story] Description`

- `[P]`: разные файлы и нет зависимости от незавершённой соседней задачи.
- `[US1]` и `[US2]`: история из [spec.md](spec.md).
- Пути в задачах указаны от корня репозитория.

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Подготовить минимальный Go-модуль, контейнерную сборку и локальную конфигурацию.

- [X] T001 Создать `backend/cabby-gateway/go.mod` с модулем `github.com/Keane81/Cabby/backend/cabby-gateway`, директивой `go 1.26.1` и закреплённой совместимой версией `github.com/rs/zerolog`.
- [X] T002 [P] Создать `backend/cabby-gateway/Dockerfile`: многоэтапная сборка на Go 1.26.1, закреплённые теги образов, непривилегированный пользователь и запуск бинарника gateway.
- [X] T003 [P] Создать `backend/cabby-gateway/.env.example` с `CABBY_GATEWAY_PORT` и `deploy/monitoring/.env.example` с именем `GRAFANA_ADMIN_PASSWORD` без рабочего пароля; убедиться, что `.gitignore` исключает локальные `.env`.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Подготовить запуск и остановку HTTP-сервиса до реализации пользовательских сценариев.

- [X] T004 Создать `backend/cabby-gateway/cmd/cabby-gateway/main.go` с запуском публичного HTTP-сервера на порту 8080, ограничениями времени чтения/записи, явным `zerolog.Logger` для JSON-логов с уровнем и временем и корректным завершением по сигналу; пока отдавать 404 для всех путей, затем сформировать `backend/cabby-gateway/go.sum`.

**Checkpoint**: Go-приложение собирается; после этого можно реализовать US1.

---

## Phase 3: User Story 1 — Проверить доступность сервиса (Priority: P1) 🎯 MVP

**Goal**: Одна публичная команда `GET /healthz` сообщает о способности сервиса обрабатывать запросы; остальные пути и методы отвергаются.

**Independent Test**: Запустить только gateway, проверить 200/404/405 по [контракту](contracts/healthcheck.md), воспроизвести 503 подменой готовности в тесте обработчика, затем остановить gateway и убедиться в отсутствии ложного успешного ответа.

### Tests for User Story 1

- [X] T005 [P] [US1] Написать в `backend/cabby-gateway/internal/server/health_test.go` тесты 200 и JSON `status=ok`, подмены готовности с 503, записи ошибки логгером с временем, операцией и классом ошибки без тела запроса, игнорирования лишнего тела, анонимного доступа, 404 для другого пути и 405 для POST/HEAD; до реализации убедиться, что проверки не проходят.

### Implementation for User Story 1

- [X] T006 [P] [US1] Создать первый вариант `compose.yaml` только с gateway: сборка из `backend/cabby-gateway/Dockerfile`, публикация порта из `CABBY_GATEWAY_PORT` только на loopback, без Docker healthcheck, который создавал бы фоновый RPS.
- [X] T007 [US1] Реализовать в `backend/cabby-gateway/internal/server/health.go` обработчик `/healthz` с явной проверкой GET (включая запрет HEAD), JSON 200/503, внедряемым состоянием готовности и явным `zerolog.Logger` для безопасного сообщения об ошибке без побочных действий.
- [X] T008 [US1] Подключить обработчик из `backend/cabby-gateway/internal/server/health.go` к публичному серверу в `backend/cabby-gateway/cmd/cabby-gateway/main.go`; при сигнале остановки снять готовность до закрытия слушателя, не регистрировать другие публичные команды.
- [X] T009 [US1] Проверить `backend/cabby-gateway/internal/server/health_test.go` командами `go test ./...` и `go vet ./...`, затем выполнить разделы «Подготовка» и «MVP: только gateway» из `specs/001-gateway-health-monitoring/quickstart.md` через `compose.yaml`, без Prometheus и Grafana.

**Checkpoint**: US1 работает и проверяется отдельно; это минимальный результат, который можно показать без графика.

---

## Phase 4: User Story 2 — Наблюдать частоту проверок (Priority: P2)

**Goal**: Учитывать исход каждой достигшей gateway проверки и показывать общий, успешный и неуспешный RPS в Grafana; пропавшие данные отображать разрывом.

**Independent Test**: На работающем gateway отправить известную частоту вызовов и сравнить её с графиком; проверить ноль без трафика и разрыв после остановки gateway/Prometheus.

### Tests for User Story 2

- [X] T010 [US2] Добавить закреплённую совместимую версию `prometheus/client_golang` в `backend/cabby-gateway/go.mod`; контрольную сумму зависимости сформировать после появления импортов в T016.
- [X] T011 [P] [US2] Написать в `backend/cabby-gateway/internal/server/metrics_test.go` тесты существования обеих нулевых серий, последовательности запросов при готовности и моделируемой неготовности с ровно одним инкрементом соответствующего исхода на запрос, отсутствия инкремента для 404/405 и технического `GET /metrics`; до реализации убедиться, что проверки не проходят.

### Implementation for User Story 2

- [X] T012 [P] [US2] Создать `deploy/monitoring/prometheus.yml` с заданием `cabby-gateway`, внутренней целью `cabby-gateway:9091`, путём `/metrics` и интервалом сбора 15 секунд.
- [X] T013 [P] [US2] Создать `deploy/monitoring/grafana/provisioning/datasources/prometheus.yml` с источником Prometheus по внутреннему имени контейнера и стабильным UID.
- [X] T014 [P] [US2] Создать `deploy/monitoring/grafana/provisioning/dashboards/dashboards.yml` для загрузки dashboard из файла при старте Grafana.
- [X] T015 [P] [US2] Создать `deploy/monitoring/grafana/dashboards/cabby-gateway.json` с UID `cabby-gateway-health`, тремя линиями RPS из [контракта метрик](contracts/metrics.md), шагом 1 минута, обновлением 15 секунд и разрывом при отсутствии данных.
- [X] T016 [US2] Реализовать в `backend/cabby-gateway/internal/server/metrics.go` частный реестр, предсозданные серии `cabby_gateway_health_checks_total{outcome="success|failure"}` и обработчик `GET /metrics`; обновить `backend/cabby-gateway/go.sum`.
- [X] T017 [US2] Связать результат `backend/cabby-gateway/internal/server/health.go` со счётчиком: один вызов увеличивает ровно один исход; сохранить существующее логирование ошибки через zerolog без повторной записи и без тела запроса, секретов или персональных данных.
- [X] T018 [US2] Запустить внутренний HTTP-сервер метрик на порту 9091 в `backend/cabby-gateway/cmd/cabby-gateway/main.go` с совместным корректным завершением обоих серверов; результат health-check не должен зависеть от Prometheus или Grafana.
- [X] T019 [US2] Дополнить `compose.yaml` сервисами Prometheus и Grafana, закреплёнными тегами образов, volumes и конфигурациями из `deploy/monitoring/`; публиковать 9090/3000 только на loopback, не публиковать 9091 и передавать пароль Grafana из `deploy/monitoring/.env`.
- [X] T020 [US2] Выполнить `go test ./...` и `go vet ./...` в `backend/cabby-gateway/`, проверить `docker compose --env-file backend/cabby-gateway/.env --env-file deploy/monitoring/.env config` и сценарии `specs/001-gateway-health-monitoring/quickstart.md`: три линии, ноль `failure` при штатной работе, тестовый ненулевой счётчик `failure` при подмене готовности, ноль общего RPS без запросов, разрыв при остановке gateway или Prometheus и восстановление после полного окна сбора.

**Checkpoint**: US2 проверяется на готовом US1; метрики и dashboard добавляют наблюдение, не меняя публичный контракт команды.

---

## Phase 5: Polish & Cross-Cutting Concerns

**Purpose**: Проверить критерии качества и оставить воспроизводимый запуск.

- [X] T021 Провести пятиминутный тест 100 запросов/с по `specs/001-gateway-health-monitoring/quickstart.md`; подтвердить SC-002 (99% ответов до 1 секунды), SC-003 (95–105 RPS после первой минуты) и SC-004 (появление точки до 2 минут) на `deploy/monitoring/grafana/dashboards/cabby-gateway.json`.
- [X] T022 Сверить фактические команды, адреса и ожидаемые результаты с `specs/001-gateway-health-monitoring/quickstart.md`; исправить расхождения и проверить, что локальные `.env` исключены в `.gitignore`.
- [X] T023 Выполнить финальные `go test -race ./...`, `go vet ./...` для `backend/cabby-gateway/` и `docker compose --env-file backend/cabby-gateway/.env --env-file deploy/monitoring/.env config` для `compose.yaml`; сверить Go 1.26.1 с предупреждением об опубликованных исправлениях в `specs/001-gateway-health-monitoring/research.md`, не меняя указанную пользователем версию без нового решения.

---

## Dependencies & Execution Order

### Phase Dependencies

1. Phase 1 (T001–T003) → Phase 2 (T004).
2. Phase 2 → US1 (T005–T009). Внутри US1 тест T005 предшествует T007–T008; T006 может выполняться параллельно с тестом/кодом после Setup.
3. US1 → US2 (T010–T020). Метрики подключаются к готовому обработчику. После T010 тест T011 и конфигурационные T012–T015 независимы друг от друга; T016–T019 интегрируют их результаты.
4. Обе истории → Phase 5 (T021–T023).

### User Story Dependencies

- **US1 (P1)**: зависит только от Setup и Foundational. Демонстрируется самостоятельно по HTTP-контракту.
- **US2 (P2)**: использует вызовы US1 как источник событий. Её наблюдаемость можно отдельно проверить на работающем gateway, но завершение US2 зависит от реализации US1.

### Parallel Opportunities

- T002 и T003 могут выполняться одновременно после T001: они редактируют разные файлы и используют уже выбранную структуру.
- T005 и T006 могут выполняться одновременно после T004.
- После T010 задачи T011–T015 могут выполняться одновременно: тест, конфигурация Prometheus, provision Grafana и dashboard находятся в разных файлах.
- T016–T019 выполняются после нужных им тестов и конфигураций; они не помечены `[P]`, поскольку разделяют код или интеграционные зависимости.

## Parallel Example: User Story 1

```text
T005: написать контрактные проверки в backend/cabby-gateway/internal/server/health_test.go
T006: подготовить самостоятельный запуск gateway в compose.yaml
```

## Parallel Example: User Story 2

```text
T011: написать проверки счётчика в backend/cabby-gateway/internal/server/metrics_test.go
T012: задать scrape в deploy/monitoring/prometheus.yml
T013: настроить источник данных в deploy/monitoring/grafana/provisioning/datasources/prometheus.yml
T014: настроить загрузку dashboard в deploy/monitoring/grafana/provisioning/dashboards/dashboards.yml
T015: описать график в deploy/monitoring/grafana/dashboards/cabby-gateway.json
```

## Implementation Strategy

### MVP First (US1)

Выполнить T001–T009 и проверить `GET /healthz`, 503 при моделируемой неготовности, отказ на другие команды и отсутствие успешного ответа после остановки. Это отдельный рабочий сервис.

### Incremental Delivery

После MVP выполнить T010–T020: добавить экспорт счётчика и готовый график без изменения публичной команды. Затем T021–T023 подтвердят нагрузку, точность, отсутствие ложного нуля и воспроизводимость запуска.

## Notes

- Не добавлять Docker healthcheck, вызывающий `/healthz`: он создаст постоянный трафик и не позволит проверить нулевой RPS.
- Тесты для каждой истории должны сначала продемонстрировать отсутствие функции, затем проходить после её реализации.
- Изменения web, iOS и Android не требуются; связанные с ними проверки неприменимы.
- Коммиты не входят в этот список: их создание не запрошено.
