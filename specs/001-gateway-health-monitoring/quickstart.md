# Quickstart: проверка cabby-gateway и графика RPS

Этот документ описывает проверку после реализации [плана](plan.md). Контракты: [health-check](contracts/healthcheck.md) и [метрики](contracts/metrics.md).

## Предусловия

- Docker с Docker Compose для запуска полного стека; свободные локальные порты 8082 для gateway (значение из примера), 9090 для Prometheus и 3000 для Grafana. Порт gateway задаёт `CABBY_GATEWAY_PORT` в `backend/cabby-gateway/.env` — копии `backend/cabby-gateway/.env.example`; это значение используется и внутри контейнера, и для опубликованного порта. Порт Grafana можно изменить через `GRAFANA_PORT` в `deploy/monitoring/.env`; при занятом порте Prometheus задайте `PROMETHEUS_PORT` перед командами Compose.
- Для локальных проверок исходного кода — Go 1.26.1.
- Для пятиминутного нагрузочного сценария — установленный [Vegeta](https://github.com/tsenart/vegeta).
- Локальный пароль Grafana задан в `deploy/monitoring/.env` по образцу `deploy/monitoring/.env.example`; пароль не добавляется в Git.

## Подготовка

Из корня репозитория:

```sh
cp backend/cabby-gateway/.env.example backend/cabby-gateway/.env
cp backend/auth/.env.example backend/auth/.env
cp deploy/monitoring/.env.example deploy/monitoring/.env
# Задайте в deploy/monitoring/.env свой GRAFANA_ADMIN_PASSWORD.
```

Локальные `.env` сервисов (`backend/cabby-gateway/.env`, `backend/auth/.env`) обязательны: `env_file` в `compose.yaml` и `make`-цели читают только их, а не `.env.example`. Опубликованный на хосте порт берётся из того же `CABBY_GATEWAY_PORT`. Файл мониторинга нужен, чтобы задать пароль Grafana: в примере он пустой, и без своего значения Grafana оставит пароль администратора по умолчанию.

## Локальный запуск без Docker

Для одного gateway достаточно Go 1.26.1: значения берутся из `backend/cabby-gateway/.env` (см. «Подготовка»); Prometheus и Grafana не требуются:

```sh
cd backend/cabby-gateway
make run
```

Проверка с примером порта: `curl http://127.0.0.1:8082/healthz`. Метрики доступны локально на `http://127.0.0.1:9091/metrics`. Остановить сервис можно через Ctrl+C. Из каталога `backend/cabby-gateway` доступны `make test`, `make test-race`, `make vet`. Перед командами Compose ниже вернитесь в корень репозитория: `cd ../..`.

## MVP: только gateway

Этот раздел выполняется после US1 и не требует Prometheus или Grafana:

```sh
docker compose --env-file backend/cabby-gateway/.env --env-file deploy/monitoring/.env up --build -d cabby-gateway
curl -i http://127.0.0.1:8082/healthz
curl -i -X POST http://127.0.0.1:8082/healthz
curl -I http://127.0.0.1:8082/healthz
curl -i http://127.0.0.1:8082/unknown
```

Ожидаются соответственно 200 с `{"status":"ok"}`, 405, 405 и 404. Ответ 503 и безопасную запись об ошибке воспроизводит тест обработчика с подменённым внутренним состоянием готовности; публичной команды для переключения готовности нет.

```sh
docker compose --env-file backend/cabby-gateway/.env --env-file deploy/monitoring/.env stop cabby-gateway
curl -i --max-time 2 http://127.0.0.1:8082/healthz
```

После остановки положительного ответа нет. Во время корректной остановки запрос, успевший дойти до обработчика после снятия готовности, получает 503; после закрытия слушателя соединение завершается ошибкой или таймаутом.

## Полный стек и метрики

Из корня репозитория весь стек можно запустить командой `make docker-up`, проверить состояние через `make docker-ps` и остановить через `make docker-down`. Эти цели используют оба `.env` файла из раздела «Подготовка». Эквивалентные команды Compose:

```sh
docker compose --env-file backend/cabby-gateway/.env --env-file deploy/monitoring/.env config
docker compose --env-file backend/cabby-gateway/.env --env-file deploy/monitoring/.env up --build -d
docker compose --env-file backend/cabby-gateway/.env --env-file deploy/monitoring/.env ps
```

Ожидается три работающих сервиса: `cabby-gateway`, `prometheus`, `grafana`. Наличие Prometheus и Grafana не является условием положительного ответа gateway. Отдельный Docker healthcheck, вызывающий `/healthz`, не настраивается: он создавал бы постоянный фоновый RPS. Повторите `GET /healthz` и убедитесь, что счётчик `success` вырос; POST, HEAD и другой путь его не меняют.

```sh
curl -G -fsS http://127.0.0.1:9090/api/v1/query \
  --data-urlencode 'query=up{job="cabby-gateway"}'
curl -G -fsS http://127.0.0.1:9090/api/v1/query \
  --data-urlencode 'query=cabby_gateway_health_checks_total'
```

После первого scrape `up` равен 1; существуют серии `success` и `failure`. Порт 9091 не опубликован на хосте. Тест счётчика с подменённой готовностью подтверждает ненулевое значение `failure`; при штатной работе оно может оставаться нулевым.

Откройте `http://127.0.0.1:3000/d/cabby-gateway-health`, войдите под `admin` с локальным паролем. Должны быть видны линии общего, успешного и неуспешного RPS. После минуты без вызовов линия равна нулю. При отсутствии полного окна сразу после запуска допустим разрыв. Если volume Grafana уже существовал, изменение пароля в `deploy/monitoring/.env` не изменит сохранённый пароль администратора: его нужно отдельно сбросить через `grafana cli admin reset-admin-password` внутри контейнера.

## Нагрузка и точность

```sh
echo 'GET http://127.0.0.1:8082/healthz' |
  vegeta attack -rate=100/s -duration=5m -timeout=1s |
  vegeta report
```

Отчёт должен подтвердить не менее 99% ответов в пределах одной секунды. На графике после первой минуты в каждой из следующих трёх полных минут общий RPS должен оставаться в пределах 95–105. Проверка проводится без других источников вызовов health-check. Grafana должна показать соответствующие точки не позднее двух минут после интервала.

## Пропажа данных

```sh
docker compose --env-file backend/cabby-gateway/.env --env-file deploy/monitoring/.env stop cabby-gateway
curl -i --max-time 2 http://127.0.0.1:8082/healthz
```

Запрос больше не даёт положительный результат; Prometheus показывает `up=0`, а линия RPS разрывается, а не опускается к нулю. После `docker compose --env-file backend/cabby-gateway/.env --env-file deploy/monitoring/.env start cabby-gateway` график появляется снова после полного окна успешных опросов. При остановленном Prometheus Grafana также показывает отсутствие данных.

## Проверки проекта и завершение

```sh
cd backend/cabby-gateway
make test
make test-race
make vet
cd ../..
docker compose --env-file backend/cabby-gateway/.env --env-file deploy/monitoring/.env down
```

Тесты должны проходить без ошибок. `down` останавливает контейнеры; сохранённые volumes можно оставить для повторного запуска.
