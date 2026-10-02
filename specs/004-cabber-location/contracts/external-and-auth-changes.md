# Изменения контрактов: публикация местоположения (v1)

**Feature**: [spec.md](../spec.md) | **Plan**: [plan.md](../plan.md)

Канонические файлы правятся при реализации; здесь — целевое состояние для ревью. Артефакты, обязательные по AGENTS.md: обе копии `openapi.yaml`, руководство `external-api.md`, Bruno-запрос, `make check`.

## 1. REST: `POST /cabber/location` (openapi.yaml, `info.version` 1.1.0 → 1.2.0)

```yaml
  /cabber/location:
    post:
      tags: [cabber]
      operationId: recordCabberLocation
      summary: Record the current location of the cabber.
      description: |
        The cabber is the owner of the session in the `Authorization: Bearer` header; the
        request names no cabber. Every accepted request adds a new immutable record, and the
        previous records are kept. The service sets the time of receipt. There is no limit on
        the frequency of requests.

        An absent header, an unknown scheme, an empty token and an expired, idle or revoked
        session all give the same 401 answer as the other session operations, and nothing is
        recorded. A rejected body records nothing either.
      security:
        - bearerAccess: []
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/CabberLocationRequest'
      responses:
        '201':
          description: The location is recorded.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/CabberLocationReceipt'
        '400':
          $ref: '#/components/responses/InvalidRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '405':
          $ref: '#/components/responses/MethodNotAllowed'
        '500':
          $ref: '#/components/responses/InternalError'
        '503':
          $ref: '#/components/responses/DependencyUnavailable'
```

Схемы (`components/schemas`):

```yaml
    CabberLocationRequest:
      type: object
      required: [latitude, longitude]
      properties:
        latitude:  { type: number, minimum: -90,  maximum: 90,  description: Degrees. }
        longitude: { type: number, minimum: -180, maximum: 180, description: Degrees. }
      additionalProperties: false
    CabberLocationReceipt:
      type: object
      required: [received_at]
      properties:
        received_at: { type: string, format: date-time }
```

`description` верхнего уровня дополняется строкой о новой операции; `tags` и коды ошибок не меняются. `external-api.md` получает раздел операции и таблицу ответов.

## 2. gRPC `auth.v1`: `VerifyCabberSession` (аддитивно)

```proto
  // Подтверждает доступ и называет его владельца (FR-009 спецификации 004). Не отзывает доступ;
  // сдвигает last_seen_at по тем же правилам окна, что и любая подтверждённая операция.
  //   UNAUTHENTICATED — отсутствующий, отозванный или истёкший токен (тот же отказ, что у выхода)
  //   UNAVAILABLE — хранилище недоступно
  rpc VerifyCabberSession(VerifyCabberSessionRequest) returns (VerifyCabberSessionResponse);

message VerifyCabberSessionRequest {
  string access_token = 1;
}

message VerifyCabberSessionResponse {
  string cabber_id = 1;
}
```

Эталон `specs/003-cabber-auth/contracts/auth.proto` обновляется вместе с каноническим файлом (parity-тест).
