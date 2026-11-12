# Carrier Crow PHP SDK

Requires PHP 8.1+ and Composer. For this source checkout run `composer install` in `php/` and require `vendor/autoload.php`.

```php
require 'vendor/autoload.php';
use CarrierCrow\CarrierCrow;
use CarrierCrow\Error;

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
$detail = $cc->emails->get($result['recipients'][0]['id']);
var_dump($detail['email']['messageId'], $detail['events'], $detail['updates']);
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
} catch (Error $e) {
    echo $e->getMessage();
    // HTTP metadata is null for local/network errors.
    var_dump($e->status, $e->code, $e->field, $e->retryAfter);
}
```

Verify the exact request bytes before decoding or changing them. The full `whsec_` secret is the HMAC key. Verification allows multiple `v1` signatures, uses constant-time comparison, rejects timestamps outside the tolerance in either direction, and throws `CarrierCrow\Error` on failure.

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
