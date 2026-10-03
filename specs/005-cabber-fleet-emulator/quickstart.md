# Quickstart: Эмулятор парка кабберов

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **CLI**: [contracts/cli.md](./contracts/cli.md)

Как запустить эмулятор против локального стека и проверить критерии приёмки. Команды появятся после реализации; это сценарий приёмки. Прогоны с `-cabbers` больше 1 запускает пользователь вручную, агент запускает эмулятор только с одним каббером (§2). Предусловия — как в [004 quickstart](../004-cabber-location/quickstart.md): Docker Compose v2, Go 1.26.1, собранные `.env` сервисов.

## 1. Поднять чистый стек

```bash
make docker-up-clean
make smoke                 # стек отвечает
```

## 2. Один каббер (SC-001, User Story 1)

```bash
make emulate ARGS="-cabbers 1 -interval 1s -duration 1m"
```

Ожидается: `phase=steady active=1/1`, отправки идут раз в секунду, по окончании сводка с `fleet.started=1`, `sent.failed` пустой, `shutdown.logged_out=1`. Проверка данных в БД:

```bash
docker compose exec location-db psql -U location -c "select count(*) from cabber_location"   # ≈ 60
```

## 3. Сотня кабберов (SC-007, User Story 2)

```bash
make emulate ARGS="-cabbers 100 -interval 5s -duration 2m"
```

Ожидается: за ≈ 10 с разгона все 100 в `active`, `rate.actual_per_s` ≈ 20 ± 2, `verdict=ok`.

## 4. Повторный прогон на заполненной базе (SC-003)

Повторить п. 3 без очистки: ошибок `409` нет (новый `run-id`), сводка ровная.

## 5. Недопустимые параметры и чужая цель (FR-009, FR-016)

```bash
make emulate ARGS="-cabbers 0"                         # код выхода 2, ничего не отправлено
make emulate ARGS="-target https://example.com"        # код выхода 2: нужен -allow-remote
```

## 6. Остановка и сбой системы (SC-004, SC-005)

```bash
make emulate ARGS="-cabbers 1000 -interval 5s" &       # затем Ctrl-C
docker compose stop location                           # на 20 с во время прогона
docker compose start location
```

Ожидается: после Ctrl-C отправки прекращаются сразу, выход завершается в пределах 10 с (для парка ≤ 1 000), в сводке нет аварийного завершения; во время простоя растут `sent.failed.unavailable` и `skipped`, после старта `location` частота возвращается к заданной не позднее чем через минуту.

## 7. Ступенчатый highload-прогон (User Story 3)

```bash
for n in 1000 5000 10000 25000 50000; do
  make emulate ARGS="-cabbers $n -interval 5s -duration 5m -report run-$n.json"
done
jq '{n: .profile.cabbers, rate: .rate.actual_per_s, p95: .latency_ms.p95, verdict}' run-*.json
```

Ожидается: частота растёт линейно, пока стек справляется; на пределе растут `p95` и ошибки, `verdict` меняется на `system_saturated` — это и есть результат. Перед большими прогонами эмулятор печатает оценку прироста БД. Для 50 000 кабберов используйте несколько процессов (R-09):

```bash
for j in 0 1 2 3; do
  make emulate ARGS="-cabbers 50000 -instances 4 -instance $j -run-id r1 -seed 1 -duration 10m -report run-50k-$j.json" &
done; wait
```

## 8. Проверки кода

```bash
make check     # test, test-race, vet по всем модулям backend, включая emulator
```
