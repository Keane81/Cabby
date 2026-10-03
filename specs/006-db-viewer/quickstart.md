# Quickstart: проверка просмотрщика БД

Предусловия: Docker, заполненные `.env` у сервисов (`backend/dbviewer/.env` копируется из `.env.example`). Контракт — [contracts/viewer-api.md](contracts/viewer-api.md), модель — [data-model.md](data-model.md).

## 1. Запуск
```bash
make docker-up-clean
```
Ожидается: сервис `dbviewer` в `make docker-ps`, страница http://127.0.0.1:8090 открывается без настройки (SC-007, FR-018).

## 2. Данные для проверки
```bash
make emulate ARGS="-cabbers 1000 -interval 5s -duration 2m"
```
Ожидается: в `cabber`, `cabber_session`, `cabber_location` появились строки.

## 3. Сценарии UI
1. Главный экран: обе базы, таблицы, число строк, размер базы (US1, US3).
2. `cabber_location`: страницы вперёд/назад, сортировка по `received_at` ↓, фильтр по `cabber_id` и интервалу времени (US2).
3. `cabber`: поиск по части email; в таблице и карточке записи нет `password_hash` (US2, US4, SC-005).
4. Размеры: запомнить размер `location`, повторить п. 2, нажать «обновить» — размер вырос; сверить с `docker compose exec location-db psql -U location -c "select pg_size_pretty(pg_database_size('location'))"` (SC-004).
5. Остановить `auth-db`: база `auth` помечена ошибкой, `location` работает (FR-014).
6. Остановить gateway: все сценарии выполняются (SC-008).
7. Запрос `curl -X POST http://127.0.0.1:8090/api/databases` → 405; `?filter=password_hash:eq:x` → 400 (FR-009, FR-010).
8. Порт доступен только с `127.0.0.1` (`docker compose ps` показывает привязку к 127.0.0.1) (FR-016).

## 4. Автоматические проверки
```bash
make check
CABBY_DBVIEWER_AUTH_DB_URL=... CABBY_DBVIEWER_LOCATION_DB_URL=... make -C backend/dbviewer test-integration
```
Интеграционные тесты проверяют: пагинацию без пропусков/повторов, поиск, фильтры, сортировку, отказ записи в read-only транзакции, таймаут, маскирование, размеры против прямого `pg_database_size` и неизменность данных до/после (SC-006).
