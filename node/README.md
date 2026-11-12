# Carrier Crow for Node.js

TypeScript SDK for Node.js 18+, with ESM, CommonJS and declarations. No runtime dependencies. This package is not published yet; build locally with `npm ci && npm run build` in `node/`, then install its directory (`npm install /path/to/sdks/node`).

```ts
import CarrierCrow, { CarrierCrowError, webhooks } from 'carriercrow';
const crow = new CarrierCrow(process.env.CARRIERCROW_API_KEY);
// new CarrierCrow() also reads CARRIERCROW_API_KEY.
// Override for development: new CarrierCrow('ccm_...', { baseUrl: 'http://localhost:3000' });
```

CommonJS: `const { CarrierCrow, webhooks } = require('carriercrow');`.
The default base URL is `https://api.mta.carriercrow.com`. Keys (`ccm_...`) and OAuth access tokens (`cco_...`) both work. OAuth sends must include `app`; bound API keys may omit it.

## Send and batch

```ts
const message = {
  from: 'Shop <orders@example.com>',
  to: ['customer@example.net'],
  subject: 'Your receipt',
  text: 'Thank you for your order.',
  html: '<p>Thank you for your order.</p>',
  reply_to: 'support@example.com',
  message_id: 'order-48213',
  subscriber: 'customer-42',
  campaign: 'receipts',
  app: 'shop',
  headers: { 'X-Order': '48213' },
  attachments: [{ filename: 'receipt.txt', content: Buffer.from('Receipt').toString('base64'), content_type: 'text/plain' }],
};
const sent = await crow.emails.send(message, { idempotencyKey: 'receipt-48213' });
console.log(sent.message_id, sent.recipients[0].id);
const batch = await crow.emails.batch([message], { idempotencyKey: 'receipts-batch-1' });
for (const result of batch.results) {
  if (result.status === 'rejected') console.error(result.index, result.error, result.field);
}
```

Use at least one of `html` or `text`, at most 50 recipients across `to`, `cc` and `bcc`, and at most 100 messages in a batch. Attachments contain base64, with a 10 MB encoded message limit. Custom headers cannot override From, To, Cc, Bcc, Subject, Reply-To, Message-ID, Date, MIME-Version, Content-* or X-CC-*.

## Read emails and page results

```ts
const detail = await crow.emails.get(sent.recipients[0].id);
console.log(detail.email.status, detail.events, detail.updates);
const page = await crow.emails.list({ q: '@example.net', status: ['delayed', 'waiting'], app: 'shop', days: 7, limit: 100 });
if (page.next) {
  const next = await crow.emails.list({ q: '@example.net', status: ['delayed', 'waiting'], app: 'shop', days: 7, limit: 100, before: page.next });
  console.log(next.emails);
}
for await (const email of crow.emails.iterate({ days: 7 })) console.log(email.id);
for await (const page of crow.emails.pages({ limit: 100 })) console.log(page.since, page.emails);
```

`list` returns one wire-format page; `pages` follows `next` using `before` while preserving filters; `iterate` yields each email. Query parameters also include `tenant`.

## Domains

```ts
const { domains } = await crow.domains.list();
const domain = await crow.domains.get(domains[0].id);
const checked = await crow.domains.check(domain.id);
console.log(checked.verified, checked.checked_at, checked.records);
```

## Errors and retries

```ts
try {
  await crow.emails.send(message, { retries: 2 });
} catch (error) {
  if (error instanceof CarrierCrowError) {
    console.error(error.status, error.code, error.message, error.field, error.retryAfter);
  } else throw error;
}
```

Reads and DNS checks retry up to twice on 429, 503 or network failures, using exponential backoff with jitter and honoring `Retry-After` seconds. Configure `{ maxRetries: 2, retryDelay: 500, timeout: 30000, maxRetryAfter: 60 }` on the client (delays and timeout are milliseconds). `maxRetries: 0` disables automatic retries. `maxRetryAfter` is in seconds and defaults to 60; a larger server delay stops retries and raises the error with its original `retryAfter`. Other HTTP errors are not retried.

Sends and batches without a key run once, regardless of the client retry count. Providing `idempotencyKey` enables retries up to `maxRetries`; a per-call `{ retries: 2 }` explicitly requests retries and generates a random key if none was supplied. That key and the serialized body stay identical across attempts. `{ retries: 0 }` disables retries for a call. For retries across application restarts, persist your own key; reusing it with a changed body causes `409 idempotency_conflict`. Network and webhook errors have status `0`; network failures use `network_error` and retain the underlying `cause`.

## Verify webhook bytes

```ts
// In a Node HTTP handler, collect chunks before any JSON parsing middleware.
const chunks: Buffer[] = [];
for await (const chunk of request) chunks.push(Buffer.from(chunk));
const raw = Buffer.concat(chunks);
const header = request.headers['x-cc-signature'];
if (typeof header !== 'string') throw new Error('Missing signature');
const body = webhooks.verify(raw, header, process.env.CARRIERCROW_WEBHOOK_SECRET!);
for (const event of body.events) console.log(event.id, event.type, event.message.mtaId);
// crow.webhooks.verify(...) is also available.
```

Pass the original UTF-8 string or raw `Buffer`/`Uint8Array`, never reserialized JSON. Verification uses the whole `whsec_...` secret, HMAC-SHA256 over timestamp + `.` + raw bytes, constant-time signature comparison, and any matching `v1` signature for rotation. Both old and future timestamps must be within 300 seconds. Override with `{ tolerance: 60, now: 1700000000 }`; `now` is Unix seconds for deterministic tests. Invalid signatures or timestamps throw `CarrierCrowError` with `invalid_signature`; authenticated invalid JSON/envelopes use `invalid_webhook`. Deduplicate deliveries using each event's `id`.

## Development

```sh
npm ci
npm run build
npm test
```

Builds emit `dist/esm` and `dist/cjs`, each with declarations. Tests use Node's test runner and a local HTTP server, plus `../test-vectors/webhooks.json`. No service credentials or remote API calls are needed.
