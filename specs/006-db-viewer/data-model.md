# Data Model: Веб-просмотрщик БД

Собственных хранимых данных нет. Модель — представление схем `auth` и `location` в ответах API. Источник истины — каталог PostgreSQL ([research.md](research.md) R-03).

## Database
| Поле | Тип | Примечание |
|------|-----|-----------|
| `name` | `auth` \| `location` | фиксированный набор из конфигурации |
| `available` | bool | false при ошибке подключения; `error` — фиксированный текст без деталей |
| `sizeBytes` | int64 | `pg_database_size` |
| `tables` | []Table | |

## Table
| Поле | Тип | Примечание |
|------|-----|-----------|
| `name` | string | только схема `public`, из каталога |
| `estimatedRows` | int64 | `reltuples`; `exact` — true, если посчитано `count(*)` |
| `dataBytes`, `indexBytes`, `totalBytes` | int64 | R-07 |
| `columns` | []Column | |
| `primaryKey` | []string | для стабильной сортировки |

## Column
| Поле | Тип | Правило |
|------|-----|---------|
| `name`, `type` | string | тип приводится к виду `text`/`number`/`timestamp`/`uuid`/`bool`/`other` |
| `sensitive` | bool | имя содержит `hash\|secret\|token\|password` или тип `bytea` → значения скрыты |
| `searchable` | bool | `text`/`uuid` и не sensitive |
| `filterable`, `sortable` | bool | не sensitive и тип не `other` |

## RowQuery (параметры запроса)
`table`, `q` (поиск), `filters[]` {`column`, `op` ∈ `eq`, `contains`, `gte`, `lte`, `is_null`, `value`}, `sort` {`column`, `dir` ∈ `asc`, `desc`}, `page` ≥ 1, `pageSize` 1–200.

Валидация: все имена столбцов должны быть в `Table.columns` и пригодны для операции; `op` совместим с типом; значение приводится к типу столбца, иначе 400 с кодом `invalid_filter`. Фильтры объединяются по «И».

## RowPage (ответ)
`columns`, `rows` (массив массивов; sensitive → `{"masked": true}`, `NULL` → `null`), `total` {`value`, `kind` ∈ `exact`, `estimate`, `atLeast`}, `page`, `pageSize`.

## Связи
`cabber_session.cabber_id → cabber.id` (в auth); `cabber_location.cabber_id` ссылается на каббера логически, без внешнего ключа (разные БД). Просмотрщик связи не разворачивает, но фильтр по `cabber_id` позволяет перейти от каббера к его координатам вручную.
