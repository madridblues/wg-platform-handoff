# 02 - API Contract

## Public Compatibility API (Mullvad-like)

All endpoints are JSON over HTTPS.

### Auth

- `POST /auth/v1/token`
  - Request: `{ "account_number": "string" }`
  - Response: `{ "access_token": "string", "expiry": "RFC3339 timestamp" }`
  - Notes: returned token is accepted as `Authorization: Bearer <token>` on compatibility-protected endpoints.
  - Errors: `400`, `401`

### Accounts

- `GET /accounts/v1/accounts/me`
  - Response: `{ "id": "string", "expiry": "RFC3339 timestamp" }`

- `POST /accounts/v1/accounts`
  - Response `201`: `{ "number": "string" }`

- `DELETE /accounts/v1/accounts/me`
  - Response `204`

### Devices

- `GET /accounts/v1/devices`
  - Response: `[{ device }]`

- `POST /accounts/v1/devices`
  - Request: `{ "pubkey": "base64-or-raw", "hijack_dns": false }`
  - Response `201`: `{ device + addresses }`

- `GET /accounts/v1/devices/{id}`
  - Response: `{ device }`

- `PUT /accounts/v1/devices/{id}/pubkey`
- `PUT /accounts/v1/devices/{id}`
  - Request: `{ "pubkey": "base64-or-raw" }`
  - Response: `{ ipv4_address, ipv6_address }`

- `DELETE /accounts/v1/devices/{id}`
  - Response `204`

### App

- `GET /app/v1/relays`
  - Supports `If-None-Match`
  - Returns `304` if unchanged
  - Returns `200` with ETag otherwise

- `GET /app/v1/api-addrs`
  - Response: `["ip:port", ...]`

- `HEAD /app/v1/api-addrs`
  - Response `200` when available

- `POST /app/v1/www-auth-token`
  - Response: `{ "auth_token": "string" }`

- `POST /app/v1/submit-voucher`
  - Request: `{ "voucher_code": "string" }`
  - Response: `{ "time_added": number, "new_expiry": "RFC3339 timestamp" }`

- `POST /app/v1/problem-report`
  - Request: `{ "address": "email", "message": "string", "log": "string", "metadata": { ... } }`
  - Response `204`

## Device Payload

```json
{
  "id": "string",
  "name": "string",
  "pubkey": "string",
  "hijack_dns": false,
  "created": "RFC3339 timestamp",
  "ipv4_address": "10.64.0.5/32",
  "ipv6_address": "fd00::5/128"
}
```

## Internal Gateway API

- `POST /internal/gateways/register`
  - Purpose: register gateway metadata and bootstrap auth state

- `GET /internal/gateways/{id}/desired-config`
  - Purpose: fetch full desired WG state or delta cursor

- `POST /internal/gateways/{id}/heartbeat`
  - Purpose: liveness + quick metrics

- `POST /internal/gateways/{id}/apply-result`
  - Purpose: apply acknowledgement with success/failure details

## Billing Webhooks

- `POST /webhooks/paddle`

## Error Shape

Support both compatibility forms:

- Old style: `{ "code": "SOME_ERROR_CODE" }`
- Problem style: `{ "type": "SOME_ERROR_CODE" }` with `application/problem+json`

## Admin Dashboard (Control Plane)

- `GET /admin/login` - login page
- `POST /admin/login` - form submit using `password`
- `GET /admin` - gateway/user dashboard (requires admin session cookie)
- `POST /admin/logout` - clear admin session

