# Plaud API

What we have figured out about the (undocumented) Plaud consumer API. This is a living document; new endpoints get added as future specs require them.

**Status of knowledge:** reverse-engineered from network captures of `web.plaud.ai` plus prior-art repos (`jaisonerick/plaud-cli`, `sergivalverde/plaud-toolkit`). Empirically confirmed against the EU host on 2026-05-01 via spec 0001's smoke. Plaud has no published consumer API, so all of this is subject to change without notice.

## Hosts

| Region | Host |
|---|---|
| Region discovery (global) | `https://api.plaud.ai` |
| US | `https://api.plaud.ai` |
| EU | `https://api-euc1.plaud.ai` |
| JP | `https://api-jp.plaud.ai` |

The same shapes work on all regional hosts. Accounts are bound to one region; calling the wrong host returns 401 with no useful body.

## Auth

### Authorization header

`Authorization: bearer <jwt>` (lowercase scheme, matches what the consumer web app's `tokenstr` value is prefixed with). Confirmed working on `/file/simple/web` against `api-euc1.plaud.ai` on 2026-05-01. **Cookies are NOT required** for non-browser clients. The web app uses cookies; we do not.

### Bearer token shape

A standard JWT (`alg: HS256`, `typ: UT` for "User Token"). Around 270 characters when base64-encoded. Carries `sub` (user_id), `iat`, `exp`, `region` (`aws:<region>`), `client_id` (`web` from a browser session). Long-lived (tens of months) when issued via the consumer login flows.

### OTP flow (3 calls)

**Step 1 — Region discovery.** `POST https://api.plaud.ai/auth/otp-send-code`

Request:

```json
{
  "username": "<email>",
  "user_area": "<2-letter ISO 3166-1, e.g. NO>"
}
```

Response (status 200):

```json
{
  "status": 0,
  "msg": "ok",
  "data": { "domains": { "api": "https://api-euc1.plaud.ai" } }
}
```

The `data.domains.api` value is the regional host the user's account belongs to.

**Step 2 — Send OTP.** `POST <regional>/auth/otp-send-code`

Same request body as step 1. Response:

```json
{
  "status": 0,
  "msg": "ok",
  "request_id": "<id>",
  "token": "<one-time exchange token, NOT a bearer JWT>"
}
```

The `token` is a short-lived correlation handle, not the bearer.

**Step 3 — Verify OTP.** `POST <regional>/auth/otp-login`

Request:

```json
{
  "token": "<from step 2>",
  "code": "<6-digit OTP from email>",
  "user_area": "<same as step 1>",
  "require_set_password": true,
  "team_enabled": false
}
```

Response on success:

```json
{
  "status": 0,
  "msg": "ok",
  "request_id": "<id>",
  "access_token": "<bearer JWT>",
  "token_type": "bearer",
  "has_password": true,
  "is_new_user": false
}
```

Response when account has no password:

```json
{
  "status": 0,
  "msg": "ok",
  "access_token": "<JWT, but probably not usable for protected calls>",
  "token_type": "bearer",
  "has_password": false,
  "is_new_user": true,
  "set_password_token": "<set-pw token for step 4>"
}
```

When `set_password_token` is present, the access_token may not work for protected calls until a password-set step (`POST /auth/set-password-issue-token`) completes. plaud-cli treats this as `ErrPasswordNotSet` and surfaces a "set a password at https://web.plaud.ai" message rather than implementing the password-set flow itself.

### Apple / Google SSO

`POST <regional>/auth/sso-callback` with `{ id_token, sso_from, sso_type, user_area }` returns the same `access_token` shape. plaud-cli does not implement SSO directly; users with SSO accounts paste their JWT via `plaud login --token` instead.

### `user_area`

A 2-letter ISO 3166-1 alpha-2 country code. Defaults derived from `$LANG` / `$LC_ALL` (`nb_NO.UTF-8` → `NO`), falling back to `US`. Whether the server strictly validates this is not yet known; the cautious thing is to send something plausible.

## Endpoints

### `GET /file/simple/web` (list recordings)

Query parameters:

| Param | Type | Notes |
|---|---|---|
| `skip` | int | Pagination offset (0-based). |
| `limit` | int | Page size. The web client uses 99999; plaud-cli defaults to 200. |
| `is_trash` | int | `0` = active recordings only; `2` = include deleted. plaud-cli's `list` uses `0`. |
| `sort_by` | string | `start_time`, `edit_time`, etc. |
| `is_desc` | bool | `true` = newest first. |

Response (status 200):

```json
{
  "status": 0,
  "msg": "ok",
  "request_id": "<id>",
  "data_file_total": <int>,
  "data_file_list": [
    {
      "id": "<32-hex>",
      "filename": "<title>",
      "fullname": "<36-hex, server-side audio filename>",
      "filesize": <bytes>,
      "filetype": "<extension>",
      "file_md5": "<32-hex>",
      "start_time": <epoch_ms>,
      "end_time": <epoch_ms>,
      "edit_time": <epoch_ms>,
      "duration": <ms>,
      "timezone": <int>,
      "zonemins": <int>,
      "scene": <int>,
      "serial_number": "<7 chars>",
      "version": <int>,
      "version_ms": <ms>,
      "wait_pull": <int>,
      "is_trash": <bool>,
      "is_trans": <bool>,
      "is_summary": <bool>,
      "is_markmemo": <bool>,
      "ori_ready": <bool>,
      "keywords": [...],
      "filetag_id_list": [...],
      "edit_from": "<source>"
    }
  ]
}
```

### Time fields are milliseconds

`start_time`, `end_time`, `edit_time`, `duration` are all **milliseconds since epoch** (or millisecond durations). The lack of an `_ms` suffix is misleading; field-name conventions said seconds, observation said ms. plaud-cli uses `time.UnixMilli` and `time.Millisecond` to convert.

This generalization holds for any other Plaud time field we encounter unless proven otherwise.

## Plaud's response envelope

All endpoints we have seen return JSON with at least these top-level fields:

```json
{
  "status": <int>,
  "msg": "<human-readable reason>",
  "request_id": "<id>"
}
```

`status: 0` indicates success. Non-zero is a logical error; the HTTP status is usually still 200. plaud-cli's `internal/api/auth.go` and `list.go` both check the envelope `status` field after decoding and surface non-zero values as `ErrAPIError`. HTTP 401 is treated separately as `ErrUnauthorized`.

## Custom headers used by the web client

The web client sends a number of telemetry / routing headers on every authenticated call:

| Header | Value example | Notes |
|---|---|---|
| `app-language` | `en` | UI language. |
| `app-platform` | `web` | Client platform. |
| `edit-from` | `web` | Mutation provenance. |
| `x-device-id` | `9f2552294b4b2129` (16 hex) | Sometimes literally `[object Object]` (web client bug). Server tolerates. |
| `x-pld-user` | 64-hex | The user's `account_id` from `/user/me`. **Not a credential.** |
| `x-request-id` | random short string | Per-request correlation. |
| `timezone` | `Europe/Oslo` | IANA timezone of the client. |

**plaud-cli does NOT send any of these.** The smoke against `/file/simple/web` confirmed bearer auth alone is sufficient. If a future endpoint requires one of these (download might), we add them then. We deliberately do not pre-send headers we do not need.

## What we still need to figure out

These open questions are tracked in spec notes and resolved as new specs land:

- The audio download endpoint shape (signed URL? direct bytes?). Spec 0002.
- The transcript and summary endpoints. Spec 0002.
- Whether `file_md5` in the list response is the audio bytes' MD5 or the upload container's. Affects spec 0002's idempotency story.
- Pagination behavior at very large page sizes (the web client uses `limit=99999` with no apparent cap).
- Rate-limit headers / 429 behavior (not seen yet).
- Whether the Authorization-bearer path remains stable as Plaud rolls out new versions.

## Sources of empirical truth

- HAR captures from `web.plaud.ai`, redacted, captured during spec 0001's design phase. Not committed (gitignored as `*.har`); excerpts in `specs/0001-auth-and-list/notes.md`.
- Real-API smoke against the EU host on 2026-05-01 (spec 0001 phase 6).
- Cross-references with prior-art repos:
  - https://github.com/jaisonerick/plaud-cli (Go)
  - https://github.com/sergivalverde/plaud-toolkit (TypeScript)
  - https://github.com/leonardsellem/plaud-sync-for-obsidian (TypeScript, Obsidian plugin; long-running real-world signal that bearer auth has held for ~10 months)
