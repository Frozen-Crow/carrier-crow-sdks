# Validation

Validated on 2026-11-12. Only `ruby/` files were edited; no git commands were run.
Read `CONTRACT.md` and `openapi/v1.yaml` in full before implementation.

Commands and results:

- `ruby --version` from repository root: Ruby 3.4.4; minitest already available, no installation needed.
- `ruby -I ruby/lib ruby/test/client_test.rb` from repository root: 12 runs, 120 assertions, 0 failures, 0 errors, 0 skips.
- `ruby -c ruby/lib/carriercrow.rb`: Syntax OK.
- `ruby -Ilib test/client_test.rb && gem build carriercrow.gemspec` from `ruby/`: this shell selected `/usr/bin/ruby` 2.6.10, below the gem's required Ruby 3.1; 12 runs, 109 assertions, 1 error because its OpenSSL lacks `fixed_length_secure_compare`; gem build was not reached.
- `/opt/homebrew/opt/ruby/bin/ruby -Ilib test/client_test.rb && /opt/homebrew/opt/ruby/bin/gem build carriercrow.gemspec` from `ruby/`: 12 runs, 120 assertions, 0 failures, 0 errors, 0 skips; successfully built carriercrow 1.0.0. RubyGems warned that homepage was unspecified. The generated gem was removed after validation.

The suite exercises all seven methods against a local TCP HTTP mock; request
paths, body wrapping, auth and User-Agent; repeatable status query parameters;
API error fields and non-retry statuses; Retry-After and exhausted HTTP/network
retries; both sends with no key, explicit keys, generated keys, and retries
opt-out; byte-identical retries; and paging with preserved filters and cursors.
It loads all 11 shared webhook cases from `../test-vectors/webhooks.json` and
supports per-case `now` and `tolerance` overrides. Ruby 3.1 itself was not
installed or executed locally; the gem declares >= 3.1 and uses compatible APIs.
