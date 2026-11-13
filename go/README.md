# Carrier Crow for Go

Requires Go 1.21 or newer; uses only the standard library.

```sh
go get github.com/Frozen-Crow/carrier-crow-sdks/go
```

```go
import (
    "context"
    "log"
    cc "github.com/Frozen-Crow/carrier-crow-sdks/go"
)

client := cc.New("") // reads CARRIERCROW_API_KEY; accepts API keys or OAuth tokens
ctx := context.Background()
result, err := client.Emails.Send(ctx, cc.SendEmail{
    From: "Shop <orders@example.com>",
    To: cc.Addresses{"customer@example.net"},
    Subject: "Your order", Text: "Thanks for your order!",
    App: "shop", // required for OAuth; optional for an app-bound API key
    MessageID: "order-48213",
}, cc.SendOptions{IdempotencyKey: "order-48213"})
if err != nil { log.Fatal(err) }
id := result.Recipients[0].ID // *string; nil when the node reported no usable ID
if id == nil {
    log.Print("Accepted; find it later by message_id: ", result.MessageID)
} else {
    log.Print(*id)
}
```

All HTTP methods accept a context. Configure the endpoint, HTTP client, and retries with
`cc.New("ccm_...", cc.Options{BaseURL: "http://localhost:3000", MaxRetries: 2})`.
The default endpoint is `https://api.mta.carriercrow.com`, the HTTP timeout is 30 seconds,
and `MaxRetries: 0` selects the default of two additional attempts. Set
`DisableRetries: true` to disable retries (`MaxRetries: -1` also works). Responses 429 and 503, and network errors, retry with exponential backoff and
jitter; `Retry-After` seconds are a minimum delay. Cancellation interrupts retry waits. `MaxRetryAfter` defaults to `60 * time.Second`;
if the server requests a longer delay, the call returns immediately with the original
`Error.RetryAfter` instead of retrying.
Sends and batches without an idempotency key make one attempt. Use
`cc.SendOptions{Retry: true}` to generate a random key and safely retry that operation.
Reuse your own key across separate calls when recovering an uncertain outcome.

```go
batch, err := client.Emails.Batch(ctx, []cc.SendEmail{
    {From: "orders@example.com", To: cc.Addresses{"a@example.net"}, Subject: "Hi", Text: "Hello"},
}, cc.SendOptions{Retry: true})
if err != nil { log.Fatal(err) }
for _, item := range batch.Results { log.Print(item.Status, item.Error) }

if id != nil {
    story, err := client.Emails.Get(ctx, *id)
    if err != nil { log.Fatal(err) }
    log.Print(story.Email.Status, story.Events, story.Updates)
}

err = client.Emails.EachPage(ctx, cc.ListQuery{
    Status: []cc.EmailStatus{cc.Delivered, cc.Delayed}, Days: 7, Limit: 100,
}, func(page *cc.EmailList) bool {
    for _, email := range page.Emails { log.Print(email.ID) }
    return true // false stops paging
})
// Manual paging: Emails.List returns Next; pass *Next as ListQuery.Before.

domains, err := client.Domains.List(ctx)
if err != nil { log.Fatal(err) }
for _, domain := range domains.Domains {
    current, err := client.Domains.Get(ctx, domain.ID)
    if err != nil { log.Fatal(err) }
    checked, err := client.Domains.Check(ctx, current.ID)
    if err != nil { log.Fatal(err) }
    log.Print(checked.Verified, checked.Records)
}
```

Wire names are represented by JSON tags, including `message_id` on sends versus
`messageId` in email history. Nullable fields use pointers. `Addresses` encodes a
string array and decodes either wire format; attachment `Content` is base64 text.

```go
var apiErr *cc.Error
if errors.As(err, &apiErr) {
    log.Print(apiErr.Status, apiErr.Code, apiErr.Message, apiErr.Field, apiErr.RetryAfter)
}
```

Import `errors` for this example. Local and network failures have status zero;
`errors.Is` also reaches underlying context and transport errors. If no key is supplied
or found in the environment, the first request returns `missing_api_key` without
sending HTTP traffic.

Webhook verification uses the original HTTP request bytes, including whitespace and
UTF-8, and the entire `whsec_...` secret:

```go
raw, err := io.ReadAll(r.Body)
if err != nil { /* respond with an error */ return }
event, err := cc.Verify(raw, r.Header.Get("X-CC-Signature"), "whsec_...")
if err != nil { /* reject the webhook */ return }
log.Print(event.Delivery, event.Events)
```

Import `io` in your handler. `Verify` returns a typed `*cc.Error` on invalid signatures,
stale timestamps, or invalid JSON. It supports multiple `v1` signatures, constant-time
HMAC comparison, lowercase-only 64-character hex signatures, first-timestamp-wins
parsing, and a default 300 second tolerance in either time direction.
For deterministic tests use `cc.VerifyOptions{Now: time.Unix(1805047200, 0)}`.
Set `Tolerance` to a `*time.Duration` to override it (including zero).

Run the mock HTTP server and shared webhook vector tests from this directory:

```sh
go test ./...
go vet ./...
```

A 202 means every recipient was accepted, including when an ID is null.
A refused recipient fails the send with 422 or 429 and an indexed `field`
such as `to[1]`; no recipients were sent.
