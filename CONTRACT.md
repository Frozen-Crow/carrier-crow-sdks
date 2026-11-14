# Carrier Crow API contract (v1)

The interfaces between the three pieces of this change: **api** (control plane,
`Frozen-Crow/carrier-crow-mta-api`), **web** (console and lander,
`Frozen-Crow/carrier-crow-mta-client`) and **sdks** (this repo). They are
settled before the pieces are built, so the pieces can be built in parallel.

`openapi/v1.yaml` is the machine-readable form of section 1. If it and this
file disagree, raise it with the coordinator; don't pick one yourself.
Changes to either file go through the coordinator, never a worker.

Production origins: API `https://api.mta.carriercrow.com`, console
`https://mta.carriercrow.com`. Locally, the API's `CLIENT_HOST` and the
console's `VITE_API_BASE` point at each other as they do today.

---

## 1. Public REST API

### Authentication

`Authorization: Bearer <token>`, where the token is either:

- an **API key**, `ccm_…` (existing, `api/src/auth-keys.ts`), or
- an **OAuth access token**, `cco_…` (new, section 3).

New scopes, alongside the existing ones in `api-keys.schema.ts`:

| Scope | Allows |
|---|---|
| `emails:send` | `POST /v1/emails`, `POST /v1/emails/batch` |
| `emails:read` | `GET /v1/emails`, `GET /v1/emails/{id}` (as `telemetry:read` already does) |
| `domains:read` | `GET /v1/domains`, `GET /v1/domains/{id}`, `POST /v1/domains/{id}/check` |

`telemetry:read` keeps working everywhere it works today.

### Which app sends

Every email belongs to an **app** (today's SMTP connector), which decides its
sender (tenant), its hourly limit and its label contract. A key with
`emails:send` is **bound to one app** (`api-keys.connectorId`, new). An OAuth
token is not bound, so its sends must name the app (`"app": "<app name>"`).
If a bound key's request names a different app, the request is refused with
`403 app_mismatch`.

### Errors

Every error body is:

```json
{ "error": "human sentence saying what to send instead", "code": "snake_case_code", "field": "to[2]" }
```

`error` stays a string, as the console reads it today. `code` is new and
stable; `field` is present when one input is at fault. Codes:

| HTTP | code | When |
|---|---|---|
| 400 | `invalid_request` | Malformed JSON, missing or bad field (`field` set) |
| 401 | `unauthorized` | No token, bad token, revoked or expired |
| 403 | `forbidden` | Token lacks the scope, or the account isn't approved to send |
| 403 | `app_mismatch` | Bound key names another app |
| 404 | `not_found` | |
| 409 | `idempotency_conflict` | Same `Idempotency-Key`, different body |
| 413 | `too_large` | Message over 10 MB after encoding |
| 422 | `rejected` | The sending node refused the message (5xx at submission); `error` carries the node's reason in words |
| 422 | `invalid_stream` | Stream does not belong to the app's sender (`field: "stream"`) |
| 429 | `rate_limited` | The app's hourly limit, or the node said 4xx try later. `Retry-After` header in seconds |
| 503 | `unavailable` | Sending over the API isn't configured, or no node answered. `Retry-After` set |

### `POST /v1/emails`

Headers: optional `Idempotency-Key` (1–255 chars). The same key with the same
body within 24 hours returns the first response again, with
`Idempotent-Replayed: true`. The same key with a different body gets `409`.

Body:

| Field | Type | Notes |
|---|---|---|
| `from` | string | `"Name <addr@domain>"` or `"addr@domain"`. The domain must be one of the app's sender's domains |
| `to` | string or string[] | 1–50 addresses, counting `cc` and `bcc` together |
| `cc`, `bcc` | string or string[] | optional |
| `reply_to` | string or string[] | optional |
| `subject` | string | 1–998 chars |
| `html`, `text` | string | at least one |
| `headers` | object of string → string | optional. Not allowed: `From`, `To`, `Cc`, `Bcc`, `Subject`, `Reply-To`, `Message-ID`, `Date`, `MIME-Version`, `Content-*`, any `X-CC-*` |
| `attachments` | `[{ filename, content, content_type? }]` | `content` is base64. Up to 10 MB in total |
| `message_id` | string | your ID for the email, sent as `X-CC-Message-Id`, max 255 |
| `subscriber` | string | your ID for the recipient, sent as `X-CC-Subscriber`, max 255 |
| `campaign` | string | sent as `X-CC-Campaign`: 1–128 of `A-Za-z0-9_.-`, starting with a letter or digit |
| `stream` | string | optional; defaults to the app's stream. Must belong to the app's sender; otherwise `422` with `code: "invalid_stream"`, `field: "stream"` |
| `app` | string | the app's name. Required for OAuth tokens, optional for bound keys |

`202 Accepted`:

```json
{
  "object": "email",
  "status": "accepted",
  "message_id": "order-48213",
  "recipients": [
    { "address": "ma@example.com", "id": "0f3c…32 hex…" }
  ]
}
```

Each recipient's `id` is the MTA's ID from the submission's `250` reply, the
same ID `GET /v1/emails/{id}` and the webhooks' `message.mtaId` use. If the
node returns one ID for the whole message, every recipient carries it; if it
returns one per recipient, they are matched in order. Any other reply (no
IDs, or a count that is neither one nor one per recipient) still means the
email **was accepted**: every recipient's `id` is then `null`, the API logs it
as an error, and the caller finds the email later by its `message_id`. An ID is
never invented, and a `202` is never turned into an error that would invite a
duplicate send. `message_id` is null when none was given.

**All or nothing.** Every recipient is accepted or none is. If the node
refuses any recipient, the API ends the SMTP transaction before `DATA`,
nothing is sent, and the request fails as the refusal maps (contract §2,
step 5) with `field` naming the address, e.g. `to[1]`. A `202` therefore
always means every listed recipient was accepted.

### `POST /v1/emails/batch`

Body `{ "emails": [ <up to 100 send bodies> ] }`, `Idempotency-Key` allowed
for the whole batch. `200`:

```json
{ "results": [
  { "index": 0, "status": "accepted", "message_id": null, "recipients": [ … ] },
  { "index": 1, "status": "rejected", "error": "…", "code": "invalid_request", "field": "to" }
] }
```

Each email may specify its own `stream`; omission uses the app's stream.
An invalid stream rejects that item with `code: "invalid_stream"` and `field: "stream"`.

One bad email doesn't stop the others. A malformed batch as a whole (not an
array, more than 100) is a `400`.

### `GET /v1/emails` and `GET /v1/emails/{id}`

Already exist (`api/src/emails.ts`). Their responses don't change; the
OpenAPI file documents them as they are: `EmailList`
(`{ emails: EmailSummary[], next, since }`) and `EmailDetail`
(`{ email, events, updates }`). Query parameters as `parseQuery` reads them:
`q`, `status` (repeatable), `tenant`, `app`, `days`, `before`, `limit`.

### `GET /v1/domains`, `GET /v1/domains/{id}`, `POST /v1/domains/{id}/check`

```json
{ "domains": [ {
  "id": "…", "name": "mail.northwind.example", "sender": "northwind-retail",
  "verified": false,
  "records": [
    { "for": "signature", "type": "TXT", "name": "cc1._domainkey.mail.northwind.example", "value": "v=DKIM1; …", "status": "found" },
    { "for": "permission", "type": "TXT", "name": "mail.northwind.example", "value": "v=spf1 …", "status": "found" },
    { "for": "fakes", "type": "TXT", "name": "_dmarc.mail.northwind.example", "value": "v=DMARC1; …", "status": "missing" }
  ],
  "checked_at": "2026-11-12T10:00:00.000Z"
} ] }
```

`status` is `found`, `missing` or `wrong`. `verified` is true when all three
are found. `GET …/{id}` returns one domain object (no wrapper); `POST …/check`
re-reads DNS and returns the same.

### `GET /v1/openapi.json`

The API serves this contract, unauthenticated.

---

## 2. How the API submits mail (api only)

The HTTP API does not use Kumo's injection API (the node's admin ACL refuses
injection on purpose). It submits over **SMTP**, exactly as an app does, so
tenant attribution, hourly limits, the label contract, DKIM and telemetry stay
one path:

1. Build the RFC 5322 message from the request (labels as `X-CC-*` headers).
2. Connect to `submission.host` (`SUBMISSION_HOST`), port 587, STARTTLS.
3. `AUTH PLAIN` as the app's connector username, with a **minted
   submission token**, `ccs_<payload>.<sig>`: payload is
   `{ connectorId, exp: now + 60s, jti }`, signed HMAC-SHA256 with a new
   secret, `submission.tokenKey` (`SUBMISSION_TOKEN_KEY`).
4. `api/src/auth-smtp.ts` accepts such a token for that connector when the
   signature and expiry check out and its `jti` hasn't been used, skipping
   the connector's CIDR rule (the API key's own checks already ran).
   Everything else stays as it is.
5. Map the reply: `250 … ids=…` → `202`; `4xx` → `429 rate_limited` (or
   `503` if the node didn't answer); `5xx` → `422 rejected`. The same mapping
   applies to a refused `RCPT TO` (all or nothing, §1).

**Kumo policy (changed in this change set).** The node's SMTP AUTH hook
caches successful logins, and serves that cache when the control plane is
unreachable. A `ccs_` token must never be cached or served from cache, or it
could be replayed. `kumo/policy/init.lua` (not in git; deployed with node
config) therefore sends every `ccs_` login to the control plane, and neither
reads nor writes the cache for it. Until a node runs that policy, production
leaves `SUBMISSION_TOKEN_KEY` unset, so HTTP sending stays off.

Without `SUBMISSION_TOKEN_KEY`, `POST /v1/emails` answers `503 unavailable`
with a sentence saying sending over the API isn't set up.

---

## 3. OAuth 2.1 authorization server (api) and consent (web)

For MCP clients (Claude, Cursor, …). Public clients only, **PKCE S256
required**, no client secrets, no implicit or password grants.

| Endpoint | Spec |
|---|---|
| `GET /.well-known/oauth-protected-resource` (and `/.well-known/oauth-protected-resource/mcp`) | RFC 9728: `{ resource: "<api>/mcp", authorization_servers: ["<api>"], scopes_supported, bearer_methods_supported: ["header"] }` |
| `GET /.well-known/oauth-authorization-server` | RFC 8414: `issuer`, `authorization_endpoint` `<api>/oauth/authorize`, `token_endpoint` `<api>/oauth/token`, `registration_endpoint` `<api>/oauth/register`, `revocation_endpoint` `<api>/oauth/revoke`, `response_types_supported: ["code"]`, `grant_types_supported: ["authorization_code","refresh_token"]`, `code_challenge_methods_supported: ["S256"]`, `token_endpoint_auth_methods_supported: ["none"]`, `scopes_supported` |
| `POST /oauth/register` | RFC 7591 dynamic registration. Body `{ client_name, redirect_uris }`. Redirects must be `https://…`, or `http://localhost` / `http://127.0.0.1` on any port. Returns `{ client_id, client_id_issued_at, client_name, redirect_uris, token_endpoint_auth_method: "none", grant_types, response_types }`. Rate-limited by IP |
| `GET /oauth/authorize` | `response_type=code`, `client_id`, `redirect_uri` (exact match), `code_challenge`, `code_challenge_method=S256`, `scope`, `state`, optional `resource` (RFC 8707; must be `<api>/mcp` if given). Errors before the redirect URI is trusted are shown as a page, never redirected. Valid requests are stored for 10 minutes and redirected to `<console>/oauth/consent?request=<id>` |
| `POST /oauth/token` | Form-encoded. `grant_type=authorization_code` with `code`, `redirect_uri`, `client_id`, `code_verifier`. `grant_type=refresh_token` with `refresh_token`, `client_id`. Returns `{ access_token: "cco_…", token_type: "Bearer", expires_in: 3600, refresh_token: "ccr_…", scope }`. Codes last 60 seconds and are single use. Refresh tokens last 30 days and rotate on every use; presenting a used one revokes the whole grant |
| `POST /oauth/revoke` | RFC 7009 |

Scopes offered: `emails:send`, `emails:read`, `domains:read`, `telemetry:read`.

A token acts as **the user who approved it, in the one organization they
chose**, limited to the granted scopes and to what that user's role allows
at the time of each request (a Viewer can't grant `emails:send`). Tokens and
codes are stored hashed. `readScope` and the send route accept `cco_` tokens
the way they accept `ccm_` keys.

Console-facing endpoints (session JWT only; keys and OAuth tokens get `403`):

| Endpoint | Returns |
|---|---|
| `GET /v1/oauth/requests/{id}` | `{ client_name, redirect_host, scopes: [{ scope, label, allowed }], organizations: [{ id, name, role }], expires_at }` |
| `POST /v1/oauth/requests/{id}/decision` | Body `{ approve: boolean, organization, scopes }` (scopes may narrow what was asked). Returns `{ redirect }`: the client's `redirect_uri` with `code` and `state`, or `error=access_denied` |
| `GET /v1/oauth/grants` | `{ grants: [{ id, client_name, scopes, user, created_at, last_used_at }] }` for the active organization. An Admin sees everyone's; others see their own |
| `DELETE /v1/oauth/grants/{id}` | Revokes the grant and every token from it at once |

The console's consent page lives at `/oauth/consent`. It needs a signed-in
user (send them through sign-in and back), shows who is asking (client name
and redirect host, plainly marked as self-described), what each scope lets
it do in plain words, and the organization picker.

---

## 4. MCP server (api)

Streamable HTTP at `<api>/mcp`, built with `@modelcontextprotocol/sdk`.
Without a valid token: `401` with
`WWW-Authenticate: Bearer resource_metadata="<api>/.well-known/oauth-protected-resource"`.
Accepts `cco_` tokens and also `ccm_` keys (for CLI agents).

Tools (each checks its scope; descriptions in plain words, like the console):

| Tool | Scope | Input | Does |
|---|---|---|---|
| `send_email` | `emails:send` | the `POST /v1/emails` body (`app` required) | sends one email |
| `search_emails` | `emails:read` | `q?`, `status?[]`, `days?`, `limit?` | `GET /v1/emails` |
| `get_email` | `emails:read` | `id` | an email's story |
| `list_domains` | `domains:read` | none | domains and their records |
| `check_domain` | `domains:read` | `id` | re-reads DNS |
| `get_deliverability` | `telemetry:read` | `days?` | delivered/delayed/bounced/spam by provider |
| `list_issues` | `telemetry:read` | none | open issues, as the console's Issues page words them |

---

## 5. Console ↔ API for apps and keys (api, web)

- `api-keys` gains optional `connectorId`. A key with `emails:send` must have
  one; the API refuses to create it otherwise (`400`, `field: "connectorId"`).
- `api-keys` scopes gain `emails:send`, `emails:read`, `domains:read`. These
  three are customer scopes, not operator scopes.
- The console's Apps page gives every app two ways in, **SMTP** (today's
  settings) and **API** (create a key for this app, shown once; code samples
  in cURL and each SDK). Settings › API keys offers the new scopes.
  Settings gains **Connected apps** (OAuth grants, revoke).
- `GET /v1/settings` gains `apiSending: boolean` (whether
  `SUBMISSION_TOKEN_KEY` is set) and `mcpUrl: string | null`.

---

## 6. SDKs (sdks)

Five languages: `node/` (TypeScript), `python/`, `go/`, `php/`, `ruby/`.
Each one has the same shape in its language's idiom:

- A client from an API key: `new CarrierCrow(apiKey, { baseUrl? })`.
  `baseUrl` defaults to `https://api.mta.carriercrow.com`, and the
  `CARRIERCROW_API_KEY` env var is read when no key is passed.
- `emails.send(params, { idempotencyKey? })`, `emails.batch(list, …)`,
  `emails.get(id)`, `emails.list(query)` (with paging over `next`).
- `domains.list()`, `domains.get(id)`, `domains.check(id)`.
- `webhooks.verify(body, signatureHeader, secret, { tolerance = 300 })`.
  This is exactly `verifySignature` in `api/src/webhooks/signing.ts`:
  `X-CC-Signature: t=<unix>,v1=<hex>[,v1=…]`, HMAC-SHA256 keyed with the
  whole `whsec_…` secret over `` `${t}.${body}` ``, constant-time compare,
  `|now − t| ≤ 300`. It returns the parsed `{ webhook, delivery, events }`
  body or throws a typed error.
- One error type carrying `status`, `code`, `message`, `field` and
  `retryAfter`.
- Retries with backoff and jitter on `429`, `503` and network errors. Sends
  are retried only when an idempotency key was given; the SDK generates one
  when the caller asks for retries on sends.
- A `User-Agent` of `carriercrow-<lang>/<version>`.
- Each language has tests against a local mock server, a README with
  examples, and runs in CI (GitHub Actions). Nothing gets published to a
  package registry in this pass.
