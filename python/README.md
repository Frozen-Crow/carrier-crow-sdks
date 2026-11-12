# Carrier Crow for Python

Python 3.9+, synchronous HTTP via httpx. Install from this checkout:

```sh
python3 -m venv .venv
. .venv/bin/activate
pip install -e './python[test]'
pytest python/tests -q
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
    story = client.emails.get(sent["recipients"][0]["id"])
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
with exponential backoff and jitter. `Retry-After` seconds is a minimum delay.
Sends and batches retry only with an idempotency key; `retry=True` generates a
fresh UUID once for the operation and reuses it on every attempt. Use a stable
explicit key to deduplicate across separate calls. `backoff`, `timeout`, and
`max_retries` are client options. Errors raise `CarrierCrowError`, with
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
