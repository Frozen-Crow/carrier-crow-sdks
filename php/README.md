# Carrier Crow PHP SDK

Requires PHP 8.1+.

```sh
composer require carriercrow/carriercrow
```

This package is developed in [carrier-crow-sdks](https://github.com/Frozen-Crow/carrier-crow-sdks) under `php/`; [carrier-crow-php](https://github.com/Frozen-Crow/carrier-crow-php) is a read-only copy for Packagist. Open issues and pull requests on carrier-crow-sdks.

```php
require 'vendor/autoload.php';
use CarrierCrow\CarrierCrow;
use CarrierCrow\CarrierCrowException;

// Omit the key to read CARRIERCROW_API_KEY. OAuth access tokens also work.
$cc = new CarrierCrow('ccm_test_example', [
    'baseUrl' => 'https://api.mta.carriercrow.com',
    'timeout' => 30,
    'retries' => 2,
]);
$email = [
    'from' => 'Northwind <news@example.com>',
    'to' => ['reader@example.net'],
    'subject' => 'Your order',
    'text' => 'Thanks for your order.',
    'html' => '<p>Thanks for your order.</p>',
    'app' => 'orders', // Required with OAuth; must match a bound API key.
    'message_id' => 'order-48213',
    'subscriber' => 'customer-12',
    'campaign' => 'order-confirmations',
    'reply_to' => 'help@example.com',
    'headers' => ['X-Order' => '48213'],
    'attachments' => [[
        'filename' => 'receipt.txt',
        'content' => base64_encode('Order receipt'),
        'content_type' => 'text/plain',
    ]],
];
$result = $cc->emails->send($email, ['idempotencyKey' => 'order-48213']);
$id = $result['recipients'][0]['id']; // ?string in send and batch results
if ($id === null) {
    echo 'Accepted; find it later by message_id: ' . $result['message_id'];
} else {
    $detail = $cc->emails->get($id);
    var_dump($detail['email']['messageId'], $detail['events'], $detail['updates']);
}
$batch = $cc->emails->batch([$email], ['idempotencyKey' => 'batch-48213']);
// Batch results can contain individual rejected entries without throwing.
foreach ($batch['results'] as $entry) {
    if ($entry['status'] === 'rejected') echo $entry['error'];
}

$page = $cc->emails->list([
    'q' => '@example.net', 'status' => ['delivered', 'bounced'],
    'tenant' => 'northwind', 'app' => 'orders', 'days' => 7, 'limit' => 100,
]);
if ($page['next'] !== null) {
    $nextPage = $cc->emails->list(['before' => $page['next']]);
}
// Lazy iteration follows next as before and preserves your filters.
foreach ($cc->emails->all(['status' => ['delivered'], 'limit' => 100]) as $emailSummary) {
    echo $emailSummary['id'];
}
$domains = $cc->domains->list();
$domain = $cc->domains->get('domain-id');
$checked = $cc->domains->check('domain-id');
```

Reads and domain checks retry network errors, HTTP 429 and 503 twice by default, using exponential backoff with jitter and respecting numeric `Retry-After` seconds. Configure the retry count with the client `retries` option. Sends and batches never retry without an idempotency key, regardless of the client retry setting. Explicitly opt in per send to generate a key once and reuse it with the identical request body:

```php
$accepted = $cc->emails->send($email, ['retries' => 2]);
$batch = $cc->emails->batch([$email], ['retries' => 2]);
// Or supply a durable application key for safe retries across process restarts.
$accepted = $cc->emails->send($email, [
    'idempotencyKey' => 'order-48213', 'retries' => 2,
]);
try {
    $cc->emails->get('missing-id');
} catch (CarrierCrowException $e) {
    echo $e->getMessage();
    // HTTP metadata is null for local/network errors.
    var_dump($e->status, $e->errorCode, $e->field, $e->retryAfter);
}
```

Verify the exact request bytes before decoding or changing them. The full `whsec_` secret is the HMAC key. Verification allows multiple `v1` signatures, uses constant-time comparison, rejects timestamps outside the tolerance in either direction, and throws `CarrierCrow\CarrierCrowException` on failure.

```php
$rawBody = file_get_contents('php://input');
$event = $cc->webhooks->verify(
    $rawBody,
    $_SERVER['HTTP_X_CC_SIGNATURE'] ?? '',
    getenv('CARRIERCROW_WEBHOOK_SECRET'),
    ['tolerance' => 300],
);
foreach ($event['events'] as $item) {
    // Persist item.id to deduplicate retried deliveries.
    echo $item['id'];
}
// Deterministic tests can set ['now' => 1805047200, 'tolerance' => 300].
```

Run `composer install` and `composer test` from `php/`. Tests use a local PHP HTTP server and the shared `../test-vectors/webhooks.json`; injected Guzzle network failures also verify safe retry behavior. The client accepts `httpClient` (a Guzzle ClientInterface) and `sleep` (seconds callback) options for custom transports and deterministic tests.

Missing API keys raise `missing_api_key` at construction. `maxRetryAfter` defaults to 60 seconds; responses above it are raised immediately with `retryAfter` preserved. Repeated pagination cursors raise `invalid_response`. `CarrierCrow\Error` remains a deprecated alias of `CarrierCrowException`. Webhook verification uses the first timestamp and accepts lowercase v1 hex only.

A 202 means every recipient was accepted, including when an ID is null.
A refused recipient fails the send with 422 or 429 and an indexed `field`
such as `to[1]`; no recipients were sent.

Set the optional stream string in the send body (`'stream' => 'receipts'`) or in each email
body passed to batch. It is sent as the JSON field `stream`; omit it to use
the app's stream. Each batch email may select a different stream. A stream
outside the app's sender returns HTTP 422 with `code: "invalid_stream"` and
`field: "stream"`; in a batch, inspect the individual rejected result's
`code` and `field`.
