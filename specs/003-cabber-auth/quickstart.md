# Quickstart: Каббер — регистрация, аутентификация и выход (v1)

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Data model**: [data-model.md](./data-model.md)

Как поднять стек с `auth`, прогнать проверки и увидеть операции на дашбордах.

## 1. Предусловия

- Docker с Compose v2 и Go 1.26.1 (локальный тулчейн 1.25.0 добирает нужную версию через `GOTOOLCHAIN=auto`).
- Для регенерации protobuf — `buf` (`go install github.com/bufbuild/buf/cmd/buf@latest`). Не обязателен: сгенерированный код закоммичен в `backend/contracts/authpb/`.
- Порты 8082, 3000, 9090 не заняты. gRPC-порт сервиса (9093) и порты метрик (9091, 9094) наружу не публикуются.

## 2. Конфигурация

Каждый сервис читает только собственный локальный `.env`, скопированный из закоммиченного `.env.example`: `compose.yaml` (`env_file`), `make`-цели и интерполяция корневого `Makefile` ссылаются на `.env`, но не на пример. Файлы обязательны и должны содержать все значения:

```bash
cp backend/cabby-gateway/.env.example backend/cabby-gateway/.env
cp backend/auth/.env.example backend/auth/.env
cp deploy/monitoring/.env.example deploy/monitoring/.env
# заполнить GRAFANA_ADMIN_PASSWORD — в примере он пустой; без значения пароль администратора Grafana остаётся значением Grafana по умолчанию
```

Без `backend/cabby-gateway/.env` compose не запустится: `env_file` обязателен. Копия примера годится для compose как есть (`CABBY_GATEWAY_PORT=8082`, `CABBY_AUTH_ADDR=auth:9093`). Для запуска gateway с хоста (`make -C backend/cabby-gateway run`) адрес `auth:9093` вне сети compose не разрешается: на время такого запуска замените его в `.env` на `CABBY_AUTH_ADDR=127.0.0.1:<порт gRPC>` и верните `auth:9093` перед `make docker-up`, потому что контейнер читает тот же файл.

`CABBY_AUTH_DB_URL` из `backend/auth/.env.example` указывает на `postgres:5432` — имя сервиса внутри сети compose. Для `make -C backend/auth run` или миграций с хоста задайте в локальном `.env` адрес своей PostgreSQL (см. §3, «auth с хоста»). Пароль БД в примере — `change-me`; его же получает контейнер `postgres`, поэтому менять его обязательности нет (порт БД наружу не публикуется). Если меняете `CABBY_AUTH_DB_PASSWORD`, меняйте в том же файле и `CABBY_AUTH_DB_URL`, иначе `auth` придёт в `postgres` со старым паролем.

`.env` уже игнорируется корневым `.gitignore`; в image он не попадает: `.dockerignore` один на весь монорепозиторий и лежит в корне — контекст сборки image тоже корневой, потому что `go.mod` сервиса заменяет модуль `contracts` путём внутри репозитория (`replace … => ../contracts`).

## 3. Запуск

```bash
make docker-up          # gateway + auth + postgres + prometheus + grafana
```

Что должно быть зелёным:

```bash
docker compose ps                       # 5 контейнеров, auth healthy не требуется
curl -fsS http://127.0.0.1:8082/openapi.yaml | grep '^  version:'
# ожидаем info.version 1.1.0
docker compose logs auth | grep -E 'migrations|listen'
# ожидаем две строки: "migrations applied" и "auth listening"
```

Миграция `0001_cabber` применяется сама при старте сервиса; PostgreSQL может быть готов позже — старт ждёт его с ограниченной паузой (R-06).

### auth с хоста (без Docker)

Нужен, когда сервис правят и отлаживают под редактором: пересборки image на каждый цикл нет. Отличия от контейнерного прогона — три:

1. **Своя PostgreSQL.** `CABBY_AUTH_DB_URL` из примера указывает на `postgres:5432` — имя сервиса внутри сети compose, с хоста оно не разрешается. Годится тот же scratch-контейнер, что для интеграционных тестов (§5): он на `127.0.0.1:5433`, тогда как `cabby-postgres` наружу порт не публикует (R-05), а на `5432` может стоять чужой сервер. Пароль этого контейнера — тот, что передан в `POSTGRES_PASSWORD` при `docker run`; он не связан с `CABBY_AUTH_DB_PASSWORD` из примера (тот кормит только контейнер `postgres` из compose). Расхождение DSN и пароля роли видно по `failed SASL auth … password authentication failed` (SQLSTATE 28P01) — это не проблема сети; привести роль к значению из `.env` можно на месте: `docker exec -it cabby-auth-pg psql -U auth -d auth -c "alter user auth password '<из .env>'"`.
2. **Свободные 9093 и 9094.** Порт метрик — константа `metricsAddress` в `internal/config/config.go`, переопределить его нельзя, поэтому второй экземпляр `auth` на хосте не поднимается: процесс из забытого прошлого прогона даёт новому `bind: address already in use` уже после строки `migrations applied`. Проверить и освободить:

   ```bash
   lsof -nP -iTCP:9093 -sTCP:LISTEN   # и то же для 9094; остановить — kill -TERM <pid>
   ```

3. **Вне поля зрения стека.** Контейнерный `auth` при этом продолжает работать, и gateway адресует именно его (`auth:9093`): хостовый инстанс в сценариях §4 не участвует, пока gateway тоже не запущен с хоста (`CABBY_AUTH_ADDR=127.0.0.1:9093` и свой `CABBY_GATEWAY_PORT` в `backend/cabby-gateway/.env`). compose-Prometheus метрик хост-процессов (`9091`/`9094`) не собирает — джобы ходят по именам сервисов, так что дашборды §6 этот прогон не покажут.

```bash
# DSN — локальным .env (слой поверх примера, §2) или аргументом make:
make -C backend/auth run CABBY_AUTH_DB_URL='postgres://auth:<свой пароль>@127.0.0.1:5433/auth?sslmode=disable'
# миграции без сервиса: make -C backend/auth migrate ; остановка — Ctrl+C (graceful по SIGTERM)
```

Зелёное состояние — те же две строки в выводе (`migrations applied`, `auth listening`) и непустой ответ метрик; вызывать операции без gateway можно напрямую по gRPC. Server reflection в сервисе не зарегистрирована, поэтому описание берётся из proto-файла, а `int64` в JSON приходит строкой (`expiresAtUnix`):

```bash
cd backend/contracts && grpcurl -plaintext -import-path . -proto proto/auth/v1/auth.proto \
  -d '{"name":"Иван","email":"ivan@example.com","password":"1234"}' \
  127.0.0.1:9093 auth.v1.AuthService/RegisterCabber
curl -s 127.0.0.1:9094/metrics | grep '^cabby_auth_'
```

## 4. Проверка сценариев

```bash
# 1. Регистрация → 201, доступ не выдаётся (FR-010)
curl -i -X POST http://127.0.0.1:8082/cabbers \
  -H 'Content-Type: application/json' \
  -d '{"name":"Иван","email":"Ivan@Example.com","password":"1234"}'

# 2. Тот же email повторно → 409 email_taken (FR-009)
curl -s -X POST http://127.0.0.1:8082/cabbers \
  -H 'Content-Type: application/json' \
  -d '{"name":"Друг","email":"ivan@example.com","password":"abcd"}' | jq

# 3. Пароль из 3 символов → 400 invalid_request с field=password (FR-006, SC-005)
curl -s -X POST http://127.0.0.1:8082/cabbers \
  -H 'Content-Type: application/json' \
  -d '{"name":"Пётр","email":"petr@example.com","password":"abc"}' | jq

# 3b. Пароль из 17 символов → тот же 400 с field=password (FR-006, верхняя граница 16)
curl -s -X POST http://127.0.0.1:8082/cabbers \
  -H 'Content-Type: application/json' \
  -d '{"name":"Пётр","email":"petr@example.com","password":"abcdefghijklmnopq"}' | jq

# 4. Вход с неверным паролем и с незарегистрированным email → одинаковый 401 (FR-012, SC-006)
for e in ivan@example.com nobody@example.com; do
  curl -s -o /dev/null -w '%{http_code}\n' -X POST http://127.0.0.1:8082/cabber/session \
    -H 'Content-Type: application/json' -d "{\"email\":\"$e\",\"password\":\"wrong\"}"
done

# 5. Успешный вход → 201, access_token и expires_at не позже чем через 96 часов (FR-011, FR-013)
TOKEN=$(curl -s -X POST http://127.0.0.1:8082/cabber/session \
  -H 'Content-Type: application/json' \
  -d '{"email":"ivan@example.com","password":"1234"}' | jq -r .access_token)

# 6. Выход → 204; повторный выход с тем же токеном → 401 unauthorized (FR-019, FR-021, SC-007)
curl -i -X DELETE http://127.0.0.1:8082/cabber/session -H "Authorization: Bearer $TOKEN"
curl -i -X DELETE http://127.0.0.1:8082/cabber/session -H "Authorization: Bearer $TOKEN"

# 7. Операций восстановления пароля нет → 404 unknown_operation (FR-026, SC-011)
curl -i -X POST http://127.0.0.1:8082/cabbers/password/recovery

# 8. Отказ зависимостей → 503 service_unavailable, а не ложный успех
docker compose stop postgres
curl -i -X POST http://127.0.0.1:8082/cabber/session \
  -H 'Content-Type: application/json' \
  -d '{"email":"ivan@example.com","password":"1234"}'
docker compose start postgres
```

Тот же набор есть в Bruno-коллекции `backend/bruno/cabby-gateway/` (`registration.bru`, `login.bru`, `logout.bru`, `env: LOCAL`) — так операции проверяются в TEST и PROD окружениях.

После `docker compose start postgres` возвращать сервис не нужно: пул `pgx` переподключается сам, первый же вход снова даёт `201`. Ожидать этого не обязан `healthz` — он независим от БД по спецификации 001 (R-09).

Доступы проверяются на сервере по БД, поэтому шаг 5–6 стоит повторить после `docker compose restart auth`: перезапуск сервиса не должен отзывать доступы (состояние живёт в PostgreSQL, а не в памяти процесса).

## 5. Проверки кода

```bash
make check   # из корня: test, test-race и vet по всем модулям backend, включая контрактовые тесты
# или по одному модулю: cd backend/auth && make test test-race vet
cd backend/contracts      && go test ./...             # байт-в-байт равенство proto
```

Что должно упасть при неполной реализации (порядок правок зафиксирован в AGENTS.md):

- `TestCanonicalContractMatchesRepositoryReference` — если обновлён только один из двух `openapi.yaml`.
- `TestRouterPathsAreDefinedInContract` — если путь добавлен в роутер, но не в контракт; список путей в тесте ведётся вручную, новые константы нужно дописать.
- `TestContractVersionIsSemver` — содержит захардкоженную `1.1.0`; следующая смена версии контракта правит и ожидаемое значение в этом же наборе изменений.
- `TestCabberOperationsAreNotInContract` — новый тест: в контракте нет операций смены и восстановления пароля (FR-026).
- `TestCanonicalProtoMatchesRepositoryReference` — новый тест в `contracts`: копия proto в `specs/003-cabber-auth/contracts/` совпадает с канонической.

Интеграционные тесты репозитория выполняются только при заданном DSN и не входят в `make test`:

```bash
# PostgreSQL нужен свой: контейнер `postgres` из compose наружу порт не публикует (R-05),
# а тесты поднимают отдельную схему в рамках одного прогона.
docker run -d --name cabby-auth-pg -p 127.0.0.1:5433:5432 \
  -e POSTGRES_USER=auth -e POSTGRES_DB=auth -e POSTGRES_PASSWORD='<свой пароль>' \
  postgres:18-alpine

# DSN принимает только этот target — из аргумента make или из окружения, но не из `.env`:
# адрес `postgres` из `.env` разрешается лишь внутри сети compose. Без DSN размеченные файлы
# пропускают себя, поэтому `make test` остаётся зелёным и без PostgreSQL.
make -C backend/auth test-integration \
  CABBY_AUTH_DB_URL=postgres://auth:<свой пароль>@127.0.0.1:5433/auth?sslmode=disable
```

## 6. Дашборды

- Prometheus — <http://127.0.0.1:9090/targets>: джобы `cabby-gateway` и `auth` в состоянии `UP`.
- Grafana — <http://127.0.0.1:3000> (пароль из `deploy/monitoring/.env`), дашборд `cabby-auth accounts` (`deploy/monitoring/grafana/dashboards/auth.json`):
  - «Cabber operations RPS (gateway)» и «Rejection share by operation» — по `operation ∈ {register, login, logout}`;
  - «Refused accesses per second (brute-force indicator)» — счётчик `unauthorized` как индикатор перебора учётных данных (защиты от перебора в v1 нет, риск зафиксирован в Assumptions);
  - «Gateway to auth delay (p50, p95)» — задержка межсервисного вызова;
  - «Inside auth: request delay (p50, p95)» и «Storage queries failing per second» — где именно теряется время внутри сервиса: задержка самой обработки запроса (`cabby_auth_request_duration_seconds`) и доля отказов запросов к хранилищу (`cabby_auth_repository_query_total{outcome="failure"}` по `query`). Отдельной метрики задержки репозитория R-11 не публикует;
  - «Services reachable» — `up{job=~"cabby-gateway|auth"}`, тот же подход к недоступности, что в дашборде 001.
- Логи: `docker compose logs --no-log-prefix -f cabby-gateway auth | jq 'select(.request_id)'` — один `request_id` на обеих сторонах заменяет отсутствующую распределённую трассировку (Complexity Tracking). Без `--no-log-prefix` `jq` не видит JSON: compose ставит перед строкой префикс имени сервиса.

Проверить отсутствие утечек:

```bash
# Только приложения спецификации: postgres печатает в своём журнале текст SQL-оператора
# (`insert into cabber (name, email, password_hash) …`), а grafana — имя переменной окружения
# при старте. Ни то, ни другое не принадлежит коду 003, и обе строки живучи по своему месту.
docker compose logs cabby-gateway auth | grep -Eic 'password|access_token'   # ожидаем 0
for p in 9091 9093 9094; do nc -z -G 2 127.0.0.1 $p || echo "порт $p закрыт — как ожидается"; done
curl -sG 'http://127.0.0.1:9090/api/v1/series' \
  --data-urlencode 'match[]=cabby_auth_requests_total' | grep -Eic 'email|password'   # ожидаем 0
```

Образ `auth` собран `FROM scratch`, поэтому `docker compose exec ... wget` в нём нет — наружу метрики смотрим через Prometheus API, а недоступность портов 9091, 9093 и 9094 проверяем снаружи (`nc`). Значений `email` в метриках нет: лейблы принимают фиксированные значения `operation`, `outcome`, `method` и `query` (R-11). У проверки `series` есть граница: она взяла `cabby_auth_requests_total`, где лейбла `query` нет вовсе; в `cabby_auth_repository_query_total` значение `cabber_find_by_email` — это имя SQL-оператора, а не адрес каббера, и грепнуть его как утечку нельзя (тест `leak_test.go` в `auth` из-за этого запрещает в именах `password`, `access_token`, `token_hash`, `argon2`, но не слово `email`).

## 7. Ожидаемые значения под нагрузкой

Быстрая проверка допущений из plan.md (нагрузка ≤ 5 rps, p95 входа ≤ 250 мс, RSS < 256 MiB):

```bash
for i in $(seq 1 25); do
  curl -s -o /dev/null -w '%{time_total}\n' -X POST http://127.0.0.1:8082/cabber/session \
    -H 'Content-Type: application/json' \
    -d '{"email":"ivan@example.com","password":"1234"}' &
done; wait
docker stats --no-stream $(docker compose ps -q auth)   # имя сервиса docker stats не принимает — нужен id контейнера
```

Ожидание: p95 ≤ 250 мс при последовательных запросах; при 25 параллельных задержки растут линейно, потому что одновременных хеширования не больше двух (R-07) — это ожидаемое и принятое для v1 поведение, а не дефект. Отдельного инструмента нагрузочного тестирования в проект не добавляется.

## 8. Что осталось за пределами этой проверки

- Дерегистрация и удаление персональных данных — блокирующий долг, отдельная спецификация (`Dependencies` в spec.md).
- Смена и восстановление пароля — вне объёма v1 (FR-026).
- Распределённая трассировка, `go_*`-метрики стандартных сборщиков, TLS к PostgreSQL — отложены; причины в Complexity Tracking и research.md.
