# Implementation Plan: Веб-просмотрщик баз данных auth и location

**Branch**: `feature/hl_beginning` | **Date**: 2026-10-03 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/006-db-viewer/spec.md`

## Summary

Локальный инструмент разработчика: один небольшой Go-процесс `dbviewer` отдаёт встроенную статическую веб-страницу и read-only JSON API; API читает таблицы баз `auth` и `location` напрямую (минуя gateway и сервисы), как просил владелец. Браузер не умеет говорить с PostgreSQL, поэтому тонкий серверный слой неизбежен; он же обеспечивает режим «только чтение», маскирование секретов, таймауты и параметризацию запросов. Схема читается из каталога PostgreSQL во время работы, поэтому новые таблицы и столбцы подхватываются без доработок. Размеры берутся из функций PostgreSQL (`pg_database_size`, `pg_total_relation_size`). Интерфейс — без шага сборки (ванильный JS, встроен в бинарник через `embed`). Запускается в `compose`, порт опубликован только на `127.0.0.1`.

## Technical Context

**Language/Version**: Go 1.26.1 (как остальные модули); фронтенд — HTML/CSS/ES-модули без сборщика

**Primary Dependencies**: `github.com/jackc/pgx/v5` (уже используется), `github.com/rs/zerolog`, `backend/lifecycle` (запуск/остановка); сторонних JS-библиотек нет

**Storage**: две существующие PostgreSQL 18 (`auth-db`, `location-db`), только чтение; собственной БД нет

**Testing**: `go test` (юнит: построитель запросов, маскирование, валидация параметров), `-tags integration` против реального PostgreSQL (пагинация, поиск, фильтры, сортировка, размеры, read-only), ручной сценарий по [quickstart.md](quickstart.md)

**Target Platform**: локальный docker compose на машине разработчика (macOS/Linux), браузер с поддержкой ES-модулей

**Project Type**: web-инструмент (Go-бэкенд + встроенная статика), не gRPC-сервис

**Performance Goals**: первая страница ≤ 2 с при ≤ 1 млн строк; поиск/фильтр/сортировка ≤ 5 с (SC-002, SC-003); один пользователь, единицы запросов в секунду

**Constraints**: строго read-only; `statement_timeout` 5 с; размер страницы ≤ 200; секреты не отдаются; публикация только на `127.0.0.1`; значения данных и условия поиска не логируются

**Scale/Scope**: 2 базы, 3 таблицы сейчас (`cabber`, `cabber_session`, `cabber_location`); до миллионов строк в `cabber_location` после прогона эмулятора на 50 000 кабберов

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Принцип | Оценка | Обоснование |
|---------|--------|-------------|
| I. Явные границы | **Отступление (обосновано)** | Чтение хранилищ сервисов в обход их контрактов — прямое требование владельца. Смягчение: read-only транзакции, только локальный стенд, нет записи, сервисы не меняются. См. Complexity Tracking. |
| II. Совместимые контракты | Пройдено | Публичные контракты gateway и gRPC не затрагиваются. Новый HTTP API просмотрщика внутренний, описан в [contracts/openapi.yaml](contracts/openapi.yaml). |
| III. Устойчивость под нагрузкой | Пройдено | Нагрузка — один пользователь; точки отказа: тяжёлый запрос (таймаут 5 с, лимит страницы), недоступная БД (ошибка по каждой базе отдельно). Влияние на основные БД ограничено пулом ≤ 4 соединений на базу и таймаутом. Метрики Prometheus не заводятся — инструмент не входит в продуктовый путь; структурные логи есть (отступление от шаблона сервиса, см. ниже). |
| IV. Безопасность и данные | Пройдено | Секреты (`*hash*`, `*token*`, `*secret*`, `*password*`, `bytea`) маскируются на сервере и исключаются из поиска/фильтра/сортировки; значения запросов не логируются; доступ только с `127.0.0.1`; DSN только из окружения. |
| V. Проверяемые изменения | Пройдено | Затронуто: backend (новый модуль `backend/dbviewer`, включая встроенный web-UI), `compose`/`Makefile`. iOS/Android/gateway не затронуты. Проверки: `make check`, `make -C backend/dbviewer test-integration`, ручной quickstart. |

**Post-design re-check**: после Phase 1 вывод не изменился; единственное отступление зафиксировано ниже.

## Project Structure

### Documentation (this feature)

```text
specs/006-db-viewer/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── openapi.yaml     # внутренний HTTP API просмотрщика
│   └── viewer-api.md    # человекочитаемое описание и правила
├── checklists/requirements.md
└── tasks.md             # создаёт /speckit-tasks
```

### Source Code (repository root)

```text
backend/dbviewer/
├── go.mod                         # replace на lifecycle; contracts/platform не нужны
├── Dockerfile, Makefile (run, test, test-race, vet, test-integration), .env.example
├── cmd/dbviewer/main.go           # сборка зависимостей, lifecycle.Run
├── internal/
│   ├── config/                    # Load(): DSN обеих БД, порт; падает при отсутствии DSN
│   ├── catalog/                   # интроспекция схемы, классификация столбцов (sensitive/searchable/…)
│   ├── query/                    # построитель SELECT/COUNT из (table, search, filters, sort, page): идентификаторы только из каталога, значения — параметры
│   ├── store/                     # пулы pgx, read-only транзакции, statement_timeout, размеры
│   ├── httpapi/                   # маршруты /api/*, валидация, маппинг ошибок, раздача статики
│   └── ui/                        # embed: index.html, app.js, style.css
└── (тесты рядом: *_test.go, *_integration_test.go)

compose.yaml                        # + сервис dbviewer (depends_on auth-db, location-db; 127.0.0.1:8090)
Makefile (корень)                   # + цель `dbviewer` (открыть/запустить локально) при необходимости
backend/Makefile                    # MODULES += dbviewer
```

**Structure Decision**: отдельный Go-модуль `backend/dbviewer`, чтобы `make check` покрывал его вместе с остальными, а UI встроен в бинарник — один образ, одна команда запуска (FR-018), нет Node-инструментария в репозитории. Отдельный каталог `web/` не создаётся, пока не появится полноценный web-клиент; UI изолирован в `internal/ui` и при желании выносится без изменения API.

**Отступления от шаблона backend-сервиса (AGENTS.md)**: не gRPC и не владелец данных, поэтому нет `grpcserver/service/repo`, миграций, `platform/grpcobs` и серий Prometheus; слои `httpapi → store/query/catalog` направлены вниз, SQL только в `store` и `query`. Логгер — `zerolog` по правилам AGENTS (создаётся в `main`, передаётся значением), старт-сообщения `"dbviewer listening"`, `"dbviewer stopped"`, `"dbviewer stopped with error"`. Env: `CABBY_DBVIEWER_AUTH_DB_URL`, `CABBY_DBVIEWER_LOCATION_DB_URL`, `CABBY_DBVIEWER_HTTP_PORT` (по умолчанию 8090, не пересекается с занятыми).

## Complexity Tracking

| Нарушение | Зачем нужно | Почему проще не получилось |
|-----------|-------------|----------------------------|
| Принцип I: прямое чтение БД сервисов в обход контрактов | Прямое требование владельца; нужен просмотр сырых данных, которых нет в контрактах (в т.ч. размеры и служебные таблицы) | Добавление просмотровых RPC/эндпоинтов в сервисы и gateway раздуло бы публичные контракты ради dev-инструмента. Пересмотр: если появится общий (не локальный) стенд — инструмент там не разворачивается без отдельного решения. |
| Свой просмотрщик вместо готового (Adminer/pgAdmin) | Нужны маскирование секретов, ограничения read-only/таймауты по умолчанию, размеры в одном экране, единый поиск | Готовые клиенты показывают хэши и позволяют запись; настроить их под FR-009/FR-010 надёжно нельзя. |
