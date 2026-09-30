# Phase 1 Data Model: Каббер — регистрация, аутентификация и выход (v1)

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Research**: [research.md](./research.md)

Владелец всех сущностей — `auth`. `cabby-gateway` не читает хранилище и оперирует только контрактом из [contracts/auth.proto](./contracts/auth.proto).

## 1. Сущность `cabber` (каббер)

Ключевая сущность спецификации: пользователь-водитель, чья личность подтверждается входом.

| Атрибут | Тип | Обязательность | Правила | Источник |
|---|---|---|---|---|
| `id` | `uuid` | обязателен, PK | назначается БД (`gen_random_uuid()`), неизменяем, наружу отдаётся только в ответе регистрации | FR-002 |
| `name` | `text` | обязателен | 1..64 символа (не байтов), без ведущего и концевого пробела | FR-006, R-08 |
| `email` | `text` | обязателен, unique | канонический вид: trim + нижний регистр; не более 254 символов; минимальный формат — ровно один `@`, непустые части | FR-003, FR-006, R-08 |
| `password_hash` | `text` | обязателен | PHC-строка Argon2id; открытого пароля в хранилище нет; входной пароль — от 4 до 16 символов, состав не регламентируется | FR-001, FR-004, FR-006, FR-007, R-07 |
| `created_at` | `timestamptz` | обязателен | `default now()` | FR-022 (расследование) |

**Инварианты**
- `cabber.email` уникален: второй каббер с тем же адресом не создаётся (FR-009), существующая запись при отказе не изменяется.
- Уникальность действует бессрочно: адрес не освобождается ни по сроку неактивности, ни недоступностью входа (FR-027).
- В v1 запись не обновляется и не удаляется: нет смены пароля, редактирования и дерегистрации (FR-026, FR-028).
- `id` не является учётными данными и не принимается ни в одном запросе (FR-003).

## 2. Сущность `cabber_session` (подтверждённый доступ)

Серверное состояние, порождаемое успешным входом и проверяемое на каждом запросе к операции, требующей подтверждения личности.

**Терминология.** В коде, метриках и логах это понятие называется **сессией** (session), а строка `access_token` — её **токеном**. Операции называются `CreateSession` и `DeleteSession` на каждом слое (REST `createCabberSession`/`deleteCabberSession`, gRPC `CreateCabberSession`/`DeleteCabberSession`, кейсы сервиса, метки `operation="create_session"`/`"delete_session"`). «Вход» и «выход» остаются разговорными названиями этих операций в spec.md и в Bruno-коллекции. Запись сессии в хранилище не удаляется, а отзывается (`revoked_at`), поэтому внутри репозитория операция называется `Revoke`.

| Атрибут | Тип | Обязательность | Правила | Источник |
|---|---|---|---|---|
| `id` | `uuid` | обязателен, PK | назначается БД | — |
| `token_hash` | `bytea` | обязателен, unique | SHA-256 от предъявленного токена; самого токена в БД нет | R-04 |
| `cabber_id` | `uuid` | обязателен, FK → `cabber` | владелец доступа | FR-015 |
| `created_at` | `timestamptz` | обязателен | `default now()` | — |
| `expires_at` | `timestamptz` | обязателен | `created_at + 96h`, абсолютный предел | FR-013, R-10 |
| `last_seen_at` | `timestamptz` | обязателен | обновляется при успешном использовании не чаще раза в минуту | FR-013, R-10 |
| `revoked_at` | `timestamptz` | опционален | устанавливается выходом; повторная установка не выполняется | FR-019, FR-021 |

**Инварианты**
- Доступ действует тогда и только тогда, когда `revoked_at is null and expires_at > now() and last_seen_at + 24h > now()`. Все три условия проверяются на сервере (FR-015).
- Один каббер может иметь несколько действующих доступов; отзыв одного не изменяет другие (FR-014).
- После `revoked_at` запись остаётся для расследования, но перестаёт давать доступ; запрос с ней получает единый `unauthorized` (FR-021).
- Физически записи удаляет часовой клинер по истечении 7 дней после `expires_at` либо `revoked_at` — это техническая очистка, а не дерегистрация каббера (R-10).
- `on delete cascade` по `cabber_id` задан заранее: в v1 кабберов не удаляют, но у будущей дерегистрации не должно остаться способа оставить сиротские доступы (Dependencies).

## 3. Представление в хранилище (миграция `0001_cabber`)

Хранилище принадлежит сервису `auth`: база `auth`, роль `auth`, данные на именованном томе `auth-data` (R-05). Внутри базы таблицы названы по сущности v1 — `cabber` и `cabber_session`.

```sql
create table cabber (
  id            uuid primary key default gen_random_uuid(),
  name          text not null check (name = btrim(name) and length(name) between 1 and 64),
  email         text not null check (length(email) <= 254),
  password_hash text not null,
  created_at    timestamptz not null default now()
);

create unique index cabber_email_key on cabber (email);

create table cabber_session (
  id           uuid primary key default gen_random_uuid(),
  token_hash   bytea not null unique,
  cabber_id    uuid not null references cabber (id) on delete cascade,
  created_at   timestamptz not null default now(),
  expires_at   timestamptz not null,
  last_seen_at timestamptz not null default now(),
  revoked_at   timestamptz
);

create index cabber_session_active on cabber_session (cabber_id) where revoked_at is null;
create index cabber_session_expiry on cabber_session (expires_at);
```

Ограничения на стороне БД — вторая линия: приложение проверяет поле первым, чтобы отказ был валидационным (`400`), а не серверным (`500`) (FR-006, FR-008).

## 4. Состояния доступа

```text
                  вход (FR-011)
        ∅ ────────────────────────► действует
                                       │
        выход (FR-019)                 ├── 24h бездействия или expires_at ──► истёк
        действует ──────────► отозван  │
                                       └── повторный выход ──► отклонение, состояние не меняется
```

- `действует` → `отозван`: только по явному выходу, однократно.
- `действует` → `истёк`: вычисляется при проверке, отдельного записывающего перехода нет.
- `отозван` и `истёк` — терминальные; обратно только через новый вход.

## 5. Схемы публичного REST-контракта (добавляются в `api/openapi.yaml`, версия 1.1.0)

Имена в snake_case — как в существующих схемах (`status`, `code`, `message`). Все объекты с `additionalProperties: false`, как требует стиль 002.

| Схема | Поля |
|---|---|
| `CabberRegistrationRequest` | `name` (обяз.), `email` (обяз.), `password` (обяз.) |
| `Cabber` | `cabber_id` (обяз.), `email` (обяз.) — пароля нет (FR-010) |
| `CabberSessionRequest` | `email` (обяз.), `password` (обяз.) |
| `CabberSession` | `access_token` (обяз.), `expires_at` (обяз., date-time) |
| `Error` (существующая, расширяется) | `error.code`, `error.message` (обяз.), **новый необязательный** `error.field` |
| `ErrorCode` (существующая, расширяется) | `unknown_operation`, `method_not_allowed` **+ `invalid_request`, `unauthorized`, `email_taken`, `service_unavailable`, `internal_error`** |

Новые пути и ответы:

| Путь | Метод | Ответы |
|---|---|---|
| `/cabbers` | `POST` | `201 Cabber`, `400 invalid_request`, `405`, `409 email_taken`, `500 internal_error`, `503 service_unavailable` |
| `/cabber/session` | `POST` | `201 CabberSession`, `400`, `401 unauthorized`, `405`, `500`, `503` |
| `/cabber/session` | `DELETE` | `204`, `401 unauthorized`, `405`, `500`, `503` |

**Что в контракт не входит намеренно (FR-026, SC-011)**: смена пароля, восстановление пароля, подтверждение email, чтение и изменение профиля, дерегистрация. Обращение к ним даёт ответ `unknown_operation`, и это закреплено тестом.

**Совместимость (принцип II)**: добавление путей, схем и значений `ErrorCode` — аддитивно, отсюда minor 1.0.0 → 1.1.0. Расширение `Error` необязательным полем `field` не ломает клиентов, которые не читают неизвестные поля; это предписано руководством 002 и повторено в FR-024.

## 6. Маппинг состояний сервиса на REST

| gRPC-код сервиса | Источник | HTTP | `error.code` | `error.field` |
|---|---|---|---|---|
| `OK` | — | 201 / 204 | — | — |
| `INVALID_ARGUMENT` + `ErrorField` | FR-006, FR-008 | 400 | `invalid_request` | из `ErrorField.field` |
| `ALREADY_EXISTS` | FR-009 | 409 | `email_taken` | `email` |
| `UNAUTHENTICATED` | FR-012, FR-016, FR-021 | 401 | `unauthorized` | — |
| `UNAVAILABLE`, `DEADLINE_EXCEEDED` | FR-009 edge case «Отказ хранилища», R-09 | 503 | `service_unavailable` | — |
| любой отказ, который нельзя описать наружу: `INTERNAL` сервиса и всякая доменная ошибка вне списка отображения | код 002 (`InternalError`) | 500 | `internal_error`, фиксированное сообщение, без деталей | — |
| неизвестный путь/метод в gateway | 002 | 404 / 405 | `unknown_operation` / `method_not_allowed` | — |

Сообщения об ошибках фиксированы и не включают ни данных запроса, ни причин отказа хранилища; утечка проверяется тестами по образцу `TestErrorDoesNotLeakRequestData` (FR-004, FR-016).

## 7. Поток данных

**Регистрация** — `POST /cabbers` → gateway читает и ограничивает тело, вызывает `RegisterCabber` → сервис канонизирует email, проверяет поля, считает Argon2id, вставляет запись (unique-index разрешает гонку двух одинаковых регистраций в `ALREADY_EXISTS`) → `201`.

**Вход** — `POST /cabber/session` → `CreateCabberSession` → поиск по каноническому email; если записи нет, всё равно выполняется хеширование-заглушка, чтобы время ответа не выдавало существование учётной записи (FR-012, SC-006); при совпадении — вставка `cabber_session` и выдача токена.

**Выход** — `DELETE /cabber/session` → заголовок разбирается на `Authorization: Bearer`, токен передаётся в `DeleteCabberSession` → поиск по `token_hash` и проверка трёх условий → установка `revoked_at` в том же запросе; отзыв идемпотентен относительно сторонних эффектов, но не возвращает успех уже отозванному токену (FR-021).

## 8. Матрица: сущностные правила → проверки

| Правило | FR | SC | Проверка |
|---|---|---|---|
| Уникальность email | FR-003, FR-009 | SC-001 | unit репозитория + интеграционный тест на `cabber_email_key` |
| Нет открытого пароля | FR-001, FR-004 | SC-002 | тест формата PHC-строки + тест логов и метрик на отсутствие секретов |
| Минимальная валидация полей | FR-006..FR-008 | SC-005, SC-010 | табличные unit-тесты `service` с инъекцией `Now` |
| Единый отказ входа | FR-012 | SC-006 | unit-тест одинакового ответа + замеры времени на фиксированной заглушке |
| 96 часов абсолют и 24 часа бездействия | FR-013 | SC-009 | unit-тест с подставными часами, границы 95:59:59 / 96:00:01 и 23:59 / 24:01 |
| Независимость доступов | FR-014 | SC-004 | интеграционный тест: два токена, отзыв одного |
| Отзыв на сервере | FR-019..FR-021 | SC-004, SC-007 | unit + интеграционный тест повторного отзыва |
| Нет операций восстановления | FR-026 | SC-011 | тест роутера: `/cabbers/password` и `/cabber/session/recover` → `unknown_operation`; тест proto-файла на отсутствие методов |
| Email занят навсегда | FR-027 | SC-011 | интеграционный тест повторной регистрации занятого адреса |
| Контракт и пути согласованы | FR-023 | SC-008 | `TestRouterPathsAreDefinedInContract`, `TestCanonicalContractMatchesRepositoryReference` |
