---

description: "Task list for the DB viewer (spec 006)"
---

# Tasks: Веб-просмотрщик баз данных auth и location

**Input**: Design documents from `/specs/006-db-viewer/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Включены: конституция (принцип V) требует проверок критичных сценариев, а read-only и маскирование секретов — критичные свойства безопасности. Интеграционные тесты идут под тегом `integration` (как в `backend/location`).

**Organization**: задачи сгруппированы по пользовательским историям; каждая история проверяется независимо.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: можно выполнять параллельно (разные файлы, нет зависимости от незавершённых задач)
- **[Story]**: US1…US4 из spec.md
- Все пути — от корня репозитория; модуль — `backend/dbviewer/`

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: каркас модуля по образцу `backend/location`

- [X] T001 Создать модуль `backend/dbviewer/go.mod` (module `github.com/Keane81/Cabby/backend/dbviewer`, go 1.26.1, зависимости `pgx/v5`, `zerolog`, `replace` на `../lifecycle`) и пустые пакеты `cmd/dbviewer`, `internal/{config,catalog,query,store,httpapi,ui}`
- [X] T002 [P] Создать `backend/dbviewer/Makefile` (цели `run`, `test`, `test-race`, `vet`, `test-integration` по образцу `backend/location/Makefile`) и `backend/dbviewer/.env.example` (`CABBY_DBVIEWER_AUTH_DB_URL`, `CABBY_DBVIEWER_LOCATION_DB_URL`, `CABBY_DBVIEWER_HTTP_PORT=8090`, DSN с хостами `auth-db`/`location-db`, пояснение про порты как в `backend/location/.env.example`)
- [X] T003 [P] Добавить `dbviewer` в `MODULES` в `backend/Makefile`
- [X] T004 [P] Создать `backend/dbviewer/Dockerfile` по образцу `backend/location/Dockerfile` (контекст — корень; копировать `go.mod` модулей, указанных в `replace`, и `internal/ui`; образ `scratch`, `EXPOSE 8090`)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: конфигурация, каталог схемы, безопасный доступ к БД, HTTP-каркас. Блокирует все истории.

**⚠️ CRITICAL**: пока фаза не завершена, истории не начинать

- [X] T005 [P] Реализовать `Load()` в `backend/dbviewer/internal/config/config.go`: чтение трёх env-переменных, ошибка при отсутствии любого DSN, порт по умолчанию 8090; тест `config_test.go` на отсутствующий DSN и порт по умолчанию
- [X] T006 [P] Реализовать классификацию столбцов в `backend/dbviewer/internal/catalog/classify.go`: тип → `text|number|timestamp|uuid|bool|other`; `sensitive` (имя содержит `hash|secret|token|password` или тип `bytea`), `searchable`, `filterable`, `sortable` по [data-model.md](data-model.md); табличный тест `classify_test.go` (включая `password_hash`, `token_hash`, `bytea`, неизвестное имя `new_secret_value`)
- [X] T007 Реализовать интроспекцию схемы `public` в `backend/dbviewer/internal/catalog/catalog.go`: таблицы, столбцы, первичный ключ из `pg_catalog`, кэш с TTL 30 с и принудительным сбросом; типы `Database`, `Table`, `Column` из [data-model.md](data-model.md) (зависит от T006)
- [X] T008 Реализовать `backend/dbviewer/internal/store/store.go`: пул pgx на каждую базу (≤ 4 соединений), метод выполнения запроса в транзакции `READ ONLY` с `SET LOCAL statement_timeout = '5s'`, классификация ошибок (`database_unavailable`, `query_timeout`) без текста исходной ошибки (R-05)
- [X] T009 [P] Реализовать маппинг ошибок и JSON-ответов в `backend/dbviewer/internal/httpapi/errors.go` (коды и фиксированные сообщения из [contracts/viewer-api.md](contracts/viewer-api.md), `Cache-Control: no-store`, `405` на любой метод кроме `GET`)
- [X] T010 Реализовать HTTP-каркас в `backend/dbviewer/internal/httpapi/server.go`: роутер, `GET /healthz`, раздача статики из `internal/ui` через `embed`, middleware с `request_id` и строкой лога (`operation`, `table`, `duration_ms`, `error_class`; без значений данных и условий поиска — FR-019) (зависит от T009)
- [X] T011 Реализовать `backend/dbviewer/cmd/dbviewer/main.go`: логгер `zerolog.New(os.Stdout).With().Timestamp().Logger()`, сборка зависимостей, запуск через `lifecycle.Run`, сообщения `"dbviewer listening"`, `"dbviewer stopped"`, `"dbviewer stopped with error"` (зависит от T005, T007, T008, T010)
- [X] T012 Добавить сервис `dbviewer` в `compose.yaml`: сборка из `backend/dbviewer/Dockerfile`, `env_file ./backend/dbviewer/.env`, `depends_on` `auth-db` и `location-db`, `ports: 127.0.0.1:${CABBY_DBVIEWER_HTTP_PORT:-8090}:8090`; добавить `backend/dbviewer/.env` в `COMPOSE_FILES` корневого `Makefile`
- [X] T013 [P] Каркас UI: `backend/dbviewer/internal/ui/index.html`, `style.css`, `app.js` (ES-модуль: роутинг по хэшу `#/db/table`, функция запроса к `/api/*`, общий показ ошибок на русском, светлая/тёмная тема через `prefers-color-scheme`)

**Checkpoint**: сервис собирается и стартует, `/healthz` отвечает, статика отдаётся

---

## Phase 3: User Story 1 - Просмотр таблиц обеих баз (Priority: P1) 🎯 MVP

**Goal**: увидеть обе базы, таблицы и постраничное содержимое, включая большие таблицы.

**Independent Test**: на стеке с данными открыть каждую таблицу каждой базы и пролистать страницы; данные совпадают с БД; остановка одной БД не ломает другую.

### Tests for User Story 1

- [X] T014 [P] [US1] Юнит-тест построителя запроса страницы (без поиска/фильтров) в `backend/dbviewer/internal/query/page_test.go`: стабильная сортировка «столбец, затем первичный ключ», `LIMIT/OFFSET`, идентификаторы только из каталога, sensitive-столбцы заменяются константой-маркером и не попадают в SELECT
- [X] T015 [P] [US1] Интеграционный тест `backend/dbviewer/internal/store/store_integration_test.go` (тег `integration`, DSN из `CABBY_DBVIEWER_*_DB_URL`): страницы без пропусков и повторов, пустая таблица, большая таблица (генерация ≥ 200 000 строк), недоступная база не влияет на вторую, запись (`INSERT`) в read-only транзакции отвергается
- [X] T016 [P] [US1] HTTP-тест `backend/dbviewer/internal/httpapi/rows_test.go` на фейковом store: `404 unknown_table`, `400 invalid_page` (page < 1, pageSize > 200), `405` на `POST`, фиксированные сообщения ошибок

### Implementation for User Story 1

- [X] T017 [US1] Реализовать построитель `SELECT` страницы в `backend/dbviewer/internal/query/page.go` (использует каталог, `pgx.Identifier.Sanitize`, sensitive → маркер)
- [X] T018 [US1] Реализовать подсчёт строк в `backend/dbviewer/internal/query/count.go` по R-06: без условий — `reltuples` (kind `estimate`) при > 100 000, иначе `count(*)` (`exact`)
- [X] T019 [US1] Реализовать `GET /api/databases` (без размеров пока): базы, `available`/`error`, таблицы, столбцы, `estimatedRows` в `backend/dbviewer/internal/httpapi/databases.go`; недоступность одной базы не роняет ответ (FR-014)
- [X] T020 [US1] Реализовать `GET /api/databases/{db}/tables/{table}/rows` (страница, `page`, `pageSize`, `total`) в `backend/dbviewer/internal/httpapi/rows.go`; значения `NULL` → `null`, маскированные → `{"masked": true}`
- [X] T021 [US1] UI: главный экран со списком баз/таблиц и числом строк (метка «≈» для оценок) в `backend/dbviewer/internal/ui/app.js` и `index.html`
- [X] T022 [US1] UI: таблица данных с заголовками, пагинацией (вперёд/назад/номер), выбором размера страницы, состояниями «нет данных» и «база недоступна», отличием `NULL` от пустой строки, ограничением длинных значений с раскрытием в `backend/dbviewer/internal/ui/app.js`, `style.css`

**Checkpoint**: US1 работает и проверяема отдельно (MVP)

---

## Phase 4: User Story 2 - Поиск, фильтрация и сортировка (Priority: P1)

**Goal**: поиск по тексту, фильтры по столбцам и сортировка по всей таблице, комбинируемые.

**Independent Test**: найти каббера по части email, отфильтровать координаты одного каббера за интервал, отсортировать по `received_at` ↓ — результат совпадает с ожидаемым.

### Tests for User Story 2

- [X] T023 [P] [US2] Юнит-тесты `backend/dbviewer/internal/query/filter_test.go`: разбор `column:op:value`, совместимость оператора с типом, приведение значения к типу, экранирование `%`, `_`, `\` в поиске, отказ на sensitive/неизвестном столбце, значения всегда параметры (проверка отсутствия значений в тексте SQL, в т.ч. для `'; drop table cabber;--`)
- [X] T024 [P] [US2] Интеграционные тесты в `backend/dbviewer/internal/store/search_integration_test.go` (тег `integration`): поиск по части email, фильтр «И» по нескольким столбцам, диапазон дат, сортировка по всей таблице в обоих направлениях, сохранение результата при смене страницы, превышение таймаута на искусственно тяжёлом запросе → `query_timeout`
- [X] T025 [P] [US2] HTTP-тест `backend/dbviewer/internal/httpapi/filters_test.go`: `400 invalid_filter` для `password_hash:eq:x`, нечислового значения числового столбца, неизвестного оператора; `400 invalid_sort` для sensitive столбца; `q` длиннее 200 символов

### Implementation for User Story 2

- [X] T026 [US2] Реализовать фильтры и поиск в `backend/dbviewer/internal/query/filter.go`: операторы `eq`, `contains`, `gte`, `lte`, `is_null`; поиск `ILIKE` по searchable-столбцам (uuid через `::text`); все значения — параметры
- [X] T027 [US2] Реализовать сортировку в `backend/dbviewer/internal/query/sort.go` и подключить к `page.go` (сортируемый столбец + первичный ключ как тайбрейк)
- [X] T028 [US2] Подсчёт с условиями в `backend/dbviewer/internal/query/count.go`: `count(*)` над подзапросом с `LIMIT 10001`, kind `atLeast` при достижении предела
- [X] T029 [US2] Разбор и валидация `q`, `filter`, `sort`, `dir` в `backend/dbviewer/internal/httpapi/rows.go` с кодами `invalid_filter`/`invalid_sort`; `query_timeout` → `504`
- [X] T030 [US2] UI: поле поиска, панель фильтров (подбор оператора по типу столбца, добавление/удаление условий), сортировка по клику на заголовок с индикатором направления в `backend/dbviewer/internal/ui/app.js`, `style.css`
- [X] T031 [US2] UI: хранение условий в адресной строке (`#/db/table?q=…`), сохранение при переходе по страницам и сброс при смене таблицы; сообщения «ничего не найдено» со сбросом, подсказки при некорректном значении, показ «>10 000», сообщение о таймауте с советом сузить условия в `backend/dbviewer/internal/ui/app.js`

**Checkpoint**: US1 и US2 работают независимо и вместе

---

## Phase 5: User Story 3 - Размер каждой базы на диске (Priority: P2)

**Goal**: показывать размер баз и таблиц, обновляемый по запросу.

**Independent Test**: запомнить размер `location`, выполнить прогон эмулятора, обновить — размер вырос и совпадает с `pg_database_size` в пределах 5%.

### Tests for User Story 3

- [X] T032 [P] [US3] Интеграционный тест `backend/dbviewer/internal/store/sizes_integration_test.go` (тег `integration`): размеры базы и таблиц равны независимому `select pg_database_size(...)`/`pg_total_relation_size(...)` (допуск 5%), рост после вставки данных
- [X] T033 [P] [US3] Вынести форматирование размеров (двоичные КБ/МБ/ГБ) в `backend/dbviewer/internal/ui/format.js` и покрыть тестом `backend/dbviewer/internal/ui/format.test.mjs` (`node --test`); добавить вызов в цель `test` файла `backend/dbviewer/Makefile`, пропуская шаг с понятным сообщением, если `node` не установлен

### Implementation for User Story 3

- [X] T034 [US3] Реализовать запросы размеров в `backend/dbviewer/internal/store/sizes.go`: `pg_database_size(current_database())`, по таблицам `pg_relation_size`, `pg_indexes_size`, `pg_total_relation_size`
- [X] T035 [US3] Добавить размеры и `refreshedAt` в `GET /api/databases` (`backend/dbviewer/internal/httpapi/databases.go`); параметр `refresh=1` сбрасывает кэш каталога
- [X] T036 [US3] UI: размер базы на главном экране, страница базы с размерами таблиц (данные/индексы/всего) и сортировкой по размеру, кнопка «Обновить» и время последнего обновления в `backend/dbviewer/internal/ui/app.js`, `format.js`

**Checkpoint**: US3 работает независимо

---

## Phase 6: User Story 4 - Просмотр записи и скрытие секретов (Priority: P3)

**Goal**: карточка записи целиком; секреты скрыты во всех представлениях.

**Independent Test**: открыть записи каббера и сессии — поля хэшей скрыты, остальные видны полностью; поиск/фильтры/сортировка по хэшам недоступны.

### Tests for User Story 4

- [X] T037 [P] [US4] Интеграционный тест `backend/dbviewer/internal/httpapi/masking_integration_test.go` (тег `integration`): по всем таблицам обеих баз ни в одном ответе нет значений `password_hash`/`token_hash` (засеять известные значения и искать их в сыром теле ответа), в `columns` помечены `sensitive`
- [X] T038 [P] [US4] Интеграционный тест «данные не меняются» в `backend/dbviewer/internal/store/readonly_integration_test.go` (тег `integration`): снимок контрольных сумм таблиц до и после прогона всех операций API неизменен (SC-006)

### Implementation for User Story 4

- [X] T039 [US4] UI: карточка записи по клику на строку (все поля целиком, кнопка копирования значения, замаскированные поля с пометкой «скрыто») в `backend/dbviewer/internal/ui/app.js`, `style.css`; запись загружается фильтром `eq` по первичному ключу через существующий эндпоинт строк
- [X] T040 [US4] UI: скрытые столбцы без элементов поиска/фильтра/сортировки (по флагам `sensitive`/`filterable`/`sortable` из `/api/databases`) в `backend/dbviewer/internal/ui/app.js`

**Checkpoint**: все истории работают

---

## Phase 7: Polish & Cross-Cutting Concerns

- [X] T041 [P] Привести контракт в соответствие реализации: [contracts/openapi.yaml](contracts/openapi.yaml) и [contracts/viewer-api.md](contracts/viewer-api.md) (параметр `refresh`, итоговые коды ошибок)
- [X] T042 [P] Добавить в корневой `Makefile` цель `dbviewer-open` или упоминание порта в комментариях рядом с `docker-up` и обновить `AGENTS.md`/README краткой строкой о просмотрщике (только если такие разделы уже существуют; иначе пропустить) — **пропущено**: в `AGENTS.md` и корне нет раздела, где перечисляются инструменты; порт и запуск описаны в `.env.example` и `quickstart.md`.
- [X] T043 Прогнать `make check` из корня (`test`, `test-race`, `vet` по всем модулям включая `dbviewer`) и `make -C backend/dbviewer test-integration` на поднятом стеке; исправить замечания
- [X] T044 Пройти ручной сценарий [quickstart.md](quickstart.md) п. 1–8 на `make docker-up-clean` + `make emulate`, зафиксировать результат; замерить SC-002 и SC-003 на таблице ≥ 1 млн строк — **выполнено частично**: п. 1–4, 7 и замеры (1 млн строк: первая страница 0,06 с, сортировка 0,08 с, поиск 0,3 с) пройдены на живом стеке; п. 5–6 (остановка `auth-db` и gateway) вживую не гонялись, чтобы не ронять рабочий стек — их покрывают интеграционный тест недоступной БД и отсутствие каких-либо вызовов gateway в коде; п. 8 подтверждён привязкой порта `127.0.0.1:8090`.
- [X] T045 Проверить логи: убедиться, что в выводе `dbviewer` нет значений данных, строк поиска и фильтров (FR-019)

---

## Dependencies & Execution Order

- **Setup (Ph.1)** → **Foundational (Ph.2)** блокирует все истории.
- **US1 (P1)** — MVP, зависит только от Ph.2.
- **US2 (P1)** строится на `query/page.go`, `rows.go`, UI-таблице из US1 (T017, T020, T022).
- **US3 (P2)** зависит от Ph.2 и T019 (расширяет `/api/databases`); от US2 не зависит.
- **US4 (P3)** зависит от US1 (строки) и UI-таблицы; маскирование на сервере уже обеспечено Ph.2 и T017.
- **Polish** — после нужных историй.

Внутри истории: тесты пишутся первыми и должны падать → затем реализация.

### Parallel Opportunities

- Setup: T002, T003, T004 параллельно после T001.
- Foundational: T005, T006, T009, T013 параллельно; T007 после T006; T010 после T009; T011 после T005/T007/T008/T010.
- US1: T014, T015, T016 параллельно; затем T017/T018 параллельно, T019 → T020, UI T021 → T022.
- US2: T023, T024, T025 параллельно; T026 и T027 параллельно.
- US3 можно вести параллельно с US2 другим исполнителем (разные файлы, кроме `databases.go` — делать после T019).
- US4: T037, T038 параллельно.

### Parallel Example: User Story 1

```text
T014 query/page_test.go          T015 store/store_integration_test.go          T016 httpapi/rows_test.go
```

## Implementation Strategy

1. **MVP**: Ph.1 → Ph.2 → US1; остановиться и проверить по Independent Test (просмотр данных обеих баз, ошибка недоступной базы).
2. Добавить US2 (поиск/фильтры/сортировка) — вместе с US1 закрывает основной запрос владельца.
3. Добавить US3 (размеры), затем US4 (карточка, UX секретов).
4. Polish: `make check`, интеграционные тесты, ручной quickstart, замеры SC-002/SC-003.

## Notes

- Коммиты по AGENTS.md: Conventional Commits на английском, отдельный коммит на связную часть (например, `feat(backend): add dbviewer module skeleton`, `feat(backend): browse tables in dbviewer`, `build: add dbviewer to compose`).
- Правила gateway-эндпоинтов (OpenAPI gateway, Bruno, smoke) здесь не применяются: API внутренний и не часть gateway.
