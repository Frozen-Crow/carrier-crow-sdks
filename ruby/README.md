# Carrier Crow for Ruby

Ruby 3.1+ client using `net/http`, with no runtime gem dependencies.
Install from this checkout with `gem build carriercrow.gemspec` and
`gem install ./carriercrow-0.1.0.gem`, or use a Bundler path dependency:

```ruby
gem 'carriercrow', path: '/path/to/api-sdks-mcp/ruby'
```

## Send and batch

```ruby
require 'carriercrow'
require 'base64'

client = CarrierCrow::Client.new # reads CARRIERCROW_API_KEY
# Or: CarrierCrow::Client.new('ccm_test_example', base_url: 'http://localhost:3000')
# OAuth access tokens (cco_...) also work; include app on OAuth sends.
message = {
  from: 'Northwind <orders@mail.example.com>',
  to: ['customer@example.net'],
  cc: 'accounts@example.net',
  reply_to: 'support@example.com',
  subject: 'Order received',
  text: 'Thanks for your order.',
  html: '<p>Thanks for your order.</p>',
  headers: {'X-Order-Number' => '48213'},
  attachments: [{filename: 'receipt.txt', content: Base64.strict_encode64('Paid'), content_type: 'text/plain'}],
  message_id: 'order-48213', subscriber: 'customer-42', campaign: 'orders', app: 'orders'
}
result = client.emails.send(message, idempotency_key: 'order-48213')
id = result['recipients'].first['id'] # String or nil in send and batch results
if id.nil?
  puts "Accepted; find it later by message_id: #{result['message_id']}"
else
  puts id
end
batch = client.emails.batch([message], idempotency_key: 'batch-48213')
batch['results'].each do |item|
  puts item['status'] # accepted or rejected; inspect error/code/field on rejection
end
```

Responses are parsed Ruby hashes with string keys. Batch wraps the supplied
array as `{"emails": [...]}`; individual rejections remain in `results`.

## Read and page emails

```ruby
unless id.nil?
  story = client.emails.get(id)
  puts story['email']['status']
  puts story['events']
  puts story['updates']
end
page = client.emails.list(q: 'customer@example.net', status: ['delivered', 'bounced'],
                          tenant: 'northwind', app: 'orders', days: 7, limit: 100)
next_page = client.emails.list(before: page['next'], limit: 100) if page['next']
client.emails.each(status: ['delivered'], days: 7).each do |email|
  puts email['id']
end
```

`emails.each` is a lazy enumerator that follows `next` as `before`, retaining
filters. `list` returns a single page including `emails`, `next`, and `since`.

## Domains

```ruby
domains = client.domains.list['domains']
domain = client.domains.get(domains.first['id'])
checked = client.domains.check(domain['id'])
checked['records'].each { |record| puts "#{record['name']}: #{record['status']}" }
```

## Errors and retries

```ruby
begin
  client.emails.send(message, retries: 2) # opts into a generated idempotency key
rescue CarrierCrow::Error => error
  warn "#{error.status} #{error.code}: #{error.message} (#{error.field})"
  warn "Retry after #{error.retry_after} seconds" if error.retry_after
end
```

`CarrierCrow::Error` carries `status`, `code`, `message`, `field`, and
`retry_after` (seconds or nil). Network failures use `code: 'network_error'`
and nil status. Webhook errors use `invalid_signature` or `invalid_payload`.

Client options are `base_url`, `retries` (default 2 additional attempts),
`timeout` (default 30 seconds for connection and reads), and an injectable
`sleeper` callable. Reads and domain checks retry 429, 503, and network failures
with exponential backoff and jitter, honoring a numeric `Retry-After` minimum.
Other HTTP failures are raised immediately. Every request sends
`User-Agent: carriercrow-ruby/0.1.0` and bearer authentication.

Sends and batches without a key make exactly one attempt, even when client
retries are configured. A supplied `idempotency_key` enables the client retry
budget. Setting per-send `retries: 2` explicitly opts into a generated UUID when
no key was supplied. The same key and serialized bytes are reused across all
attempts; `retries: 0` disables retries. Keep your own stable key when you need
safe retries across separate SDK calls. Keys must contain 1–255 characters.

## Verify webhooks

```ruby
# Rack: read the original request bytes, before parsing or transforming them.
raw_body = request.body.read
payload = client.webhooks.verify(raw_body, request.env['HTTP_X_CC_SIGNATURE'],
                                 ENV.fetch('CARRIERCROW_WEBHOOK_SECRET'))
payload['events'].each { |event| puts event['id'] } # deduplicate on this ID

# Deterministic testing can fix the clock and adjust the tolerance in seconds:
payload = CarrierCrow::Webhooks.verify(raw_body, signature, 'whsec_test_example',
                                      now: 1_805_047_200, tolerance: 300)
```

Verification uses the entire secret including `whsec_`, HMAC-SHA256 over the
original timestamp plus `.` plus the exact body bytes, constant-time signature
comparison, and an inclusive ±300-second window by default. Multiple `v1`
signatures support secret rotation. Only after verification is JSON parsed.
Invalid signatures, expired timestamps, or signed invalid JSON raise
`CarrierCrow::Error`.

## Tests

From the repository root:

```sh
ruby -I ruby/lib ruby/test/client_test.rb
```

The minitest suite uses a local TCP HTTP mock and runs every case in
`../test-vectors/webhooks.json`, including per-case clock/tolerance overrides.
No API credentials or external API connection are required.

Missing API keys raise `CarrierCrow::Error` with `missing_api_key` at construction. The client option `max_retry_after` defaults to 60 seconds; responses above it raise immediately with `retry_after` preserved. Repeated pagination cursors raise `invalid_response`. Webhook verification uses the first timestamp and accepts lowercase v1 hex only.

A 202 means every recipient was accepted, including when an ID is null.
A refused recipient fails the send with 422 or 429 and an indexed `field`
such as `to[1]`; no recipients were sent.
