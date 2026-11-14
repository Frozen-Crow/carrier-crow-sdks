# Carrier Crow for Python

Python 3.9+, synchronous HTTP via httpx.

```sh
pip install carriercrow
```

```python
from carriercrow import CarrierCrow, CarrierCrowError, webhooks

# Reads CARRIERCROW_API_KEY. An explicit key and base_url= are also supported.
with CarrierCrow() as client:
    sent = client.emails.send(
        {
            "from": "Orders <orders@example.com>",
            "to": "customer@example.net",
            "subject": "Your order",
            "text": "Thanks for your order!",
            "message_id": "order-42",
        },
        idempotency_key="order-42",
    )
    recipient_id = sent["recipients"][0]["id"]
    if recipient_id is None:
        print("Accepted; find it later by message_id:", sent["message_id"])
    else:
        story = client.emails.get(recipient_id)
        print(story["email"]["messageId"], story["events"], story["updates"])
    for email in client.emails.iter({"status": ["delivered"], "limit": 100}):
        print(email["id"], email["status"])
    # pages(query) yields full pages; list(query) returns one page.
    print(client.domains.list())
    # client.domains.get(id), client.domains.check(id)
```

`emails.batch([send_body, ...], idempotency_key=...)` sends a batch. Bodies and
responses retain the contract's wire names: send uses `reply_to` / `message_id`,
reads use `messageId`, `firstAt`, and `bounceClass`. TypedDict models are in
`carriercrow.models` and the package includes `py.typed`.

Reads and DNS checks retry network errors, 429, and 503 up to `max_retries=2`
with exponential backoff and jitter. `Retry-After` seconds is a minimum delay. If it exceeds `max_retry_after`
(default 60 seconds), retries stop and the error preserves `retry_after`.
Sends and batches retry only with an idempotency key; `retry=True` generates a
fresh UUID once for the operation and reuses it on every attempt. Use a stable
explicit key to deduplicate across separate calls. `backoff`, `timeout`, and
`max_retries` are client options. Missing API keys raise `CarrierCrowError`
with code `missing_api_key` at construction. Errors raise `CarrierCrowError`, with
`status` (0 for local errors), `code`, `message`, `field`, `retry_after`
(and its `retryAfter` alias). Use `close()` or a context manager.

Verify webhooks using the exact raw request bytes before parsing. The whole
secret is the HMAC key, including its `whsec_` prefix:

```python
import os

parsed = webhooks.verify(raw_body, signature_header, os.environ["WEBHOOK_SECRET"])
for event in parsed["events"]:
    print(event["id"], event["message"]["mtaId"])
```

`verify(..., tolerance=300)` rejects stale or future timestamps outside the
window and raises `CarrierCrowError` on invalid signatures or JSON. `now=` is
available for deterministic tests. The fixtures use only fake credentials and
all HTTP tests run against a local mock server.

A 202 means every recipient was accepted, including when an ID is null.
A refused recipient fails the send with 422 or 429 and an indexed `field`
such as `to[1]`; no recipients were sent.

Set the optional stream string in the send body (`"stream": "receipts"`) or in each email
body passed to batch. It is sent as the JSON field `stream`; omit it to use
the app's stream. Each batch email may select a different stream. A stream
outside the app's sender returns HTTP 422 with `code: "invalid_stream"` and
`field: "stream"`; in a batch, inspect the individual rejected result's
`code` and `field`.
