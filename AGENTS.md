# Правила для агентов

## Работа над задачей

- Перед реализацией прочитайте `.specify/memory/constitution.md` и относящиеся к задаче файлы в `specs/`, если они есть.
- Сохраняйте посторонние изменения в рабочем дереве. Не меняйте, не удаляйте и не включайте их в свои коммиты без запроса пользователя.
- Перед завершением запустите подходящие проверки для затронутых частей проекта. В ответе перечислите выполненные проверки и объясните, если какие-то проверки не удалось запустить.

## Публичные эндпоинты и контракты

При добавлении нового публичного эндпоинта (или изменении пути, метода, схемы запроса/ответа существующего) обязательно обновите все связанные артефакты в том же наборе изменений:

1. **Контракт.** Добавьте операцию в канонический документ `backend/cabby-gateway/api/openapi.yaml` и синхронизируйте проектный эталон `specs/002-gateway-external-contract/contracts/openapi.yaml`. Обе копии должны совпадать байт-в-байт: это проверяет тест `TestCanonicalContractMatchesRepositoryReference`, а соответствие маршрутов роутера контракту — `TestRouterPathsAreDefinedInContract`. Обновите человекочитаемое руководство `specs/002-gateway-external-contract/contracts/external-api.md` и поднимите `info.version` по SemVer (новая операция — аддитивное изменение, minor).
2. **Bruno-коллекция.** Добавьте запрос в `backend/bruno/cabby-gateway/` (новый `<name>.bru` с `url: {{host}}/<path>` и уникальным `seq`), чтобы эндпоинт можно было проверить вручную во всех окружениях.
3. **Smoke-сценарий.** Добавьте запрос в `backend/bruno/cabby-gateway/smoke/` (файл `NN-<name>.bru` с `assert` на статус и код ошибки, `seq` в порядке выполнения): положительный случай и значимые ошибки — 400, 401, 409 и т. п. Сценарий должен проходить и на пустой, и на заполненной базе, поэтому данные, которые должны быть уникальными (email), генерируйте в `script:pre-request`. Проверка — `make docker-up-clean && make smoke`.

Эндпоинт считается готовым только когда код, контракт (обе копии + руководство), Bruno-коллекция и smoke-сценарий согласованы, `make smoke` на чистом стеке зелёный, а `make check` из корня репозитория (`test`, `test-race`, `vet` по всем модулям backend) зелёный.

## Новый backend-сервис

Новый gRPC-сервис строится по шаблону `backend/location` (эталон) и `backend/auth`. Отступайте от шаблона только осознанно и объясняйте причину в комментарии или описании изменения.

**Структура.** Отдельный Go-модуль `backend/<name>` со своей БД:

```text
cmd/<name>/main.go          # только сборка зависимостей и lifecycle.Run
internal/config/            # Load() читает env, падает при отсутствии DSN
internal/grpcserver/        # транспорт: server.go и toStatus; без метрик и SQL
internal/metrics/           # серия Prometheus сервиса, UnaryInterceptor (поверх platform/grpcobs), обёртки над repo
internal/service/           # сценарии (по файлу на сценарий), errors.go, validate.go
internal/repo/              # единственное место с SQL; интерфейсы + реализация на pgxpool
migrations/embed.go         # встроенные миграции и LockKey; раннер — platform/migrate, флаг -migrate
Dockerfile, Makefile (run, migrate, test, test-race, vet, test-integration), .env.example
```

Контракт сервиса — в `backend/contracts/proto/<name>/v1/`, сгенерированный код — в `<name>pb`. Запуск и остановка серверов — только через `backend/lifecycle`. Общий код сервисов лежит в `backend/platform`: `grpcobs` (interceptor: panic guard, строка лога, хук метрик), `metricshttp` (`/metrics`), `migrate` (раннер миграций), `requestid` (создание, перенос через gRPC-metadata и проверка `request_id`). Не копируйте эти пакеты в сервис; сервисно-специфичны только имена серий, множество значений `Method` и метки. Сервис, который вызывает другой сервис, держит клиент в `internal/<target>client/`.

**Слои.** `grpcserver → service → repo`, зависимости направлены только вниз и передаются интерфейсами. SQL — только в `repo`, соответствие ошибок статусам gRPC — только в `grpcserver`. Время в `service` берётся из внедряемого `Now`, а не из `time.Now`.

**Имена.**

- Env-переменные: `CABBY_<SERVICE>_DB_URL`, `CABBY_<SERVICE>_GRPC_PORT`; адрес чужого сервиса у вызывающего — `CABBY_<TARGET>_ADDR`. Порты метрик и gRPC не пересекаются с занятыми (`8080`, `9091`, `9093`–`9096`).
- Метрики: `cabby_<service>_requests_total`, `cabby_<service>_request_duration_seconds`, дальше `cabby_<service>_<noun>_total`.
- RPC-методы называются по контракту: `<Verb><Entity>` (`RecordCabberLocation`). Метод `service` — это действие, к которому добавляется сущность, если без неё неоднозначно (`CreateSession`, `Verify` → предпочитайте `VerifySession`).
- Репозиторий: тип во множественном числе (`Locations`), интерфейс `<Entity>Repository`, конструктор `New<Entities>`. Для записи — `Create`, для чтения по ключу — `Get...By<Key>` (не смешивайте с `Find`/`Insert`), для очистки — `Purge...`.
- Ошибки: `ErrDependency` для сбоя хранилища, остальные `Err<Reason>`; текст начинается с префикса пакета (`service:`, `repo:`). Типы `Validation{Field, Reason}`, `Field`, `Reason` определяются в сервисе и не выносятся в общий пакет.

**Логирование.**

- Только `github.com/rs/zerolog`, логгер создаётся в `main` как `zerolog.New(os.Stdout).With().Timestamp().Logger()` и передаётся значением `zerolog.Logger`, без глобальных переменных.
- Стартовые сообщения: `"migrations applied"`, `"<name> listening"` (с полями `grpc_address`, `metrics_address`), `"<name> stopped"`, `"<name> stopped with error"`.
- Строку на вызов пишет `platform/grpcobs`, свой interceptor не пишется: Info при `OK`, иначе Warn; поля `operation`, `request_id` и только при неуспехе `error_class`. Тот же набор полей — в gateway (вместо `status` используйте `error_class`, если не нужен HTTP-код).
- В логи не попадают значения запроса, текст ошибок хранилища, координаты, пароли и токены. Сообщения статусов — фиксированные константы без причины.

**Наблюдаемость.** Каждый сервис отдаёт `/metrics` через `platform/metricshttp` на отдельном порту, прописанном константой, и оборачивает репозитории счётчиком запросов в `internal/metrics`.

**Готовность.** `go.mod` сервиса содержит `replace` на `contracts`, `lifecycle` и `platform`, а его Dockerfile копирует все эти модули (контекст сборки — корень репозитория). Новый сервис добавлен в `compose.yaml`, `deploy/` (БД и мониторинг), `MODULES` в `backend/Makefile` (для `make check`), а в gateway — клиент и маршруты по правилам раздела про публичные эндпоинты.

## Слияние в main

Ветки вливайте в `main` только через `git merge --no-ff`, чтобы в истории оставался merge-коммит. Fast-forward и rebase-слияние не используйте.

## Сообщения коммитов

Все коммиты в репозитории должны соответствовать [Conventional Commits 1.0.0](https://www.conventionalcommits.org/ru/v1.0.0/).
Пишите заголовок, тело и футеры сообщения коммита на английском языке; технические идентификаторы и имена файлов сохраняйте без перевода.

Формат первой строки: `<type>: <description>` или `<type>(<scope>): <description>`. Для области (`scope`) используйте затронутую часть монорепозитория, например `backend`, `web`, `ios` или `android`.

- `feat` — новая функциональность; `fix` — исправление ошибки.
- Для остальных изменений выбирайте подходящий тип, например `docs`, `refactor`, `test`, `build`, `ci` или `chore`.
- Несовместимое изменение обозначайте `!` перед двоеточием или футером `BREAKING CHANGE: ...`.

Примеры:

```text
feat(backend): add trip cancellation
fix(ios): handle expired session
docs: clarify service boundaries
feat(api)!: change trip status response
```

## Разбиение изменений на коммиты

Если пользователь просит создать коммиты, разделяйте изменения на самостоятельные смысловые части и создавайте отдельный коммит для каждой части. Каждый коммит должен содержать только связанные изменения и иметь собственное сообщение в формате Conventional Commits. Не объединяйте независимые изменения в один коммит и не дробите единое изменение по файлам без смысловой причины.
