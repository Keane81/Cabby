# Quickstart: Каббер — публикация текущего местоположения (v1)

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Data model**: [data-model.md](./data-model.md)

Как поднять стек с `location`, оставить координаты и проверить, что записи накапливаются. Общие предусловия (Docker Compose v2, Go 1.26.1, свободные порты 8082, 3000, 9090) — как в [003 quickstart](../003-cabber-auth/quickstart.md). Команды ниже появятся после реализации; это сценарий приёмки.

## 1. Конфигурация

```bash
cp backend/location/.env.example backend/location/.env
```

В `backend/cabby-gateway/.env` добавляется `CABBY_LOCATION_ADDR=location:9095` (в `.env.example` значение уже есть). `CABBY_LOCATION_DB_URL` указывает на `location-db:5432`; пароль в примере — `change-me`, менять его в `CABBY_LOCATION_DB_PASSWORD` и в DSN нужно одновременно. Порты `9095` (gRPC) и `9096` (метрики) наружу не публикуются.

## 2. Запуск

Команды `docker compose …` из корня репозитория без `--env-file` печатают предупреждения вида «variable is not set»: интерполяцию пароля читает только `make docker-up` (корневой `Makefile` подставляет `.env` сервисов). На работу сервисов предупреждения не влияют; чтобы их не видеть, используйте `make docker-ps` или `docker compose --env-file backend/location/.env --env-file backend/auth/.env …`.

```bash
make docker-up          # gateway + auth + auth-db + location + location-db + prometheus + grafana
curl -fsS http://127.0.0.1:8082/openapi.yaml | grep '^  version:'   # ожидаем 1.2.0
docker compose logs location | grep -E 'migrations|listen'          # "migrations applied", "location listening"
```

## 3. Сценарий приёмки (REST)

```bash
HOST=http://127.0.0.1:8082
curl -fsS -XPOST $HOST/cabbers -H 'content-type: application/json' \
  -d '{"name":"Иван","email":"ivan@example.com","password":"1234"}'
TOKEN=$(curl -fsS -XPOST $HOST/cabber/session -H 'content-type: application/json' \
  -d '{"email":"ivan@example.com","password":"1234"}' | sed -E 's/.*"access_token":"([^"]+)".*/\1/')

# US1-1, US1-2: две отправки — две записи
curl -i -XPOST $HOST/cabber/location -H "Authorization: Bearer $TOKEN" -H 'content-type: application/json' \
  -d '{"latitude":55.7558,"longitude":37.6173}'          # 201, {"received_at":...}
curl -i -XPOST $HOST/cabber/location -H "Authorization: Bearer $TOKEN" -H 'content-type: application/json' \
  -d '{"latitude":59.9343,"longitude":30.3351}'          # 201

# US1-3, US1-4: некорректное — 400 с полем, ничего не пишется
curl -i -XPOST $HOST/cabber/location -H "Authorization: Bearer $TOKEN" -H 'content-type: application/json' \
  -d '{"latitude":91,"longitude":0}'                      # 400 invalid_request, field=latitude
curl -i -XPOST $HOST/cabber/location -H "Authorization: Bearer $TOKEN" -H 'content-type: application/json' \
  -d '{"latitude":"55","longitude":0}'                    # 400 invalid_request, field=latitude

# US1-5: без доступа — 401 тем же телом, что у выхода
curl -i -XPOST $HOST/cabber/location -H 'content-type: application/json' \
  -d '{"latitude":1,"longitude":1}'                       # 401 unauthorized
curl -i -XDELETE $HOST/cabber/session -H "Authorization: Bearer $TOKEN"                # 204
curl -i -XPOST $HOST/cabber/location -H "Authorization: Bearer $TOKEN" -H 'content-type: application/json' \
  -d '{"latitude":1,"longitude":1}'                       # 401, запись не создана
```

Ожидаемо: 201, 201, 400, 400, 401, 204, 401.

## 4. Проверка записей

Чтения через API нет (вне объёма v1), поэтому записи смотрим в БД `location`:

```bash
docker compose exec location-db psql -U location -d location -c \
  "select cabber_id, latitude, longitude, received_at from cabber_location order by received_at, id"
# ожидаем ровно две строки: 55.7558/37.6173 и 59.9343/30.3351; отклонённые запросы строк не добавили
```

## 5. Сохранность (FR-010, SC-005)

```bash
docker compose restart location-db location
docker compose exec location-db psql -U location -d location -tc "select count(*) from cabber_location"   # то же число
```

## 6. Нагрузка и рост (принцип III)

Допущения плана (R-02) подлежат проверке: ≈ 200 rps устойчиво, пик 600 rps, p95 ≤ 300 мс при 200 rps. Замер на ноутбуке уже сделан и записан в R-02 (закрытый цикл, без ограничения скорости); на целевом окружении его нужно повторить.

```bash
# закрытый цикл: ab есть в macOS; «Failed requests» по длине ответа — не отказы, смотрите на Non-2xx
echo '{"latitude":55.7558,"longitude":37.6173}' > /tmp/body.json
ab -q -k -n 40000 -c 64 -p /tmp/body.json -T application/json \
  -H "Authorization: Bearer $TOKEN" $HOST/cabber/location
# равномерные 200 rps в течение 5 минут: hey -z 300s -q 200 -m POST … (hey ставится отдельно)
```

Смотрим на дашборде `location` (Grafana): доля 5xx, p95, число строк в сутки. Порог-триггер пересмотра: p95 > 300 мс при 200 rps, либо рост таблицы выше 50 млн строк (R-03), либо появление первого злоупотреблении без лимита (Complexity Tracking). Результат замера записать в `research.md` R-02 вместо допущения.

## 7. Проверки репозитория

```bash
make check                                              # test, test-race, vet по всем модулям
docker run -d --rm --name cabby-location-pg -e POSTGRES_USER=location -e POSTGRES_PASSWORD=test -e POSTGRES_DB=location -p 127.0.0.1:5434:5432 postgres:18-alpine
make -C backend/location test-integration CABBY_LOCATION_DB_URL="postgres://location:test@127.0.0.1:5434/location?sslmode=disable"
docker stop cabby-location-pg
```

Интеграционные тесты идут только против DSN, переданного make, как у `auth` (003 quickstart §5). Зелёным считается: все модули `test`/`test-race`/`vet`, контрактовые тесты gateway (обе копии `openapi.yaml` равны, роутер совпадает с контрактом), parity-тест `contracts`, интеграционные тесты репозитория, шаги §3–§5 с ожидаемыми кодами.
