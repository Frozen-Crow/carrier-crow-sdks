# Local validation

Validated on 2026-11-12. No requests were made to the real Carrier Crow API;
HTTP tests use local mock servers and all credentials are obvious test values.
All five suites read all **14 cases** in `test-vectors/webhooks.json`.

| SDK | Runtime used locally | Exact command (from repository root unless noted) | Result |
| --- | --- | --- | --- |
| Node | Node 20.19.6 | `cd node && npm ci --ignore-scripts && npm test` | Passed: strict TypeScript ESM/CJS/declaration builds and 10 test groups; zero failures/skips, zero npm audit vulnerabilities |
| Python | Python 3.14.7 | `python/.venv/bin/pytest python/tests -q` | Passed: 18 tests |
| Python lint | same | `python/.venv/bin/ruff check python` | Passed |
| Python types | same | `python/.venv/bin/mypy python/src/carriercrow` | Passed: both source files |
| Go | Go 1.24.3 | `cd go && go test -count=1 ./... && go vet ./...` | Passed: test suite and vet, no external dependencies |
| PHP | PHP 8.4.8, PHPUnit 10.5.66 | `cd php && composer install --no-interaction --prefer-dist && composer test && composer validate --no-check-publish` | Passed: 9 tests, 71 assertions; Composer metadata valid |
| Ruby | Ruby 3.4.4 | `/opt/homebrew/opt/ruby/bin/ruby -I ruby/lib ruby/test/client_test.rb` | Passed: 12 tests, 123 assertions; no failures/errors/skips |

Setup was `python3 -m venv python/.venv` followed by
`python/.venv/bin/pip install -e 'python[test]'`; the only Python runtime
dependency is httpx. PHP's lockfile was resolved with
`composer config --working-dir=php platform.php 8.1.0` and
`composer update --working-dir=php --no-interaction --quiet` so its dependencies
respect the declared minimum, rather than the newer local PHP.

Additional packaging/compatibility checks by language helpers:

- `cd node && npm pack --dry-run`: passed; both module formats and types included.
- `npx --yes --package=node@18 node --test node/test/sdk.test.mjs`: passed,
  all 10 groups on Node 18, including all 14 final shared vectors.
- `cd ruby && /opt/homebrew/opt/ruby/bin/gem build carriercrow.gemspec`: passed;
  the generated `.gem` is ignored, not published.

The shell sometimes resolves `ruby` to the unsupported macOS Ruby 2.6.10 when
using a separate working directory. That invocation failed on OpenSSL's
constant-time comparison API; selecting the installed supported Homebrew Ruby
3.4.4 explicitly passes. No supported language toolchain is missing.

Formatting checks also pass: PHP CS Fixer `@PSR12` (source and tests), Ruff
formatting, and RuboCop's Layout, SingleLineMethods, and Semicolon checks.
Tooling was installed outside the repository; it is not a runtime dependency.
The initial PHP CS Fixer development install's post-install script failed;
`composer install --no-scripts` completed the formatting tool installation.

The single GitHub Actions workflow runs every language on push and PR: Node
18/22, Python 3.9/3.13, Go 1.21, PHP 8.1, and Ruby 3.1. These minimum-version
CI runs remain subject to GitHub execution after push; this report claims local
results only. No package registry publication or registry credentials were added.


Initial GitHub CI confirmed Node 18/22, Go 1.21, PHP 8.1 and Ruby 3.1 passed.
Python 3.9 setup failed before tests because `ubuntu-latest` resolved to Ubuntu
26.04 with no Python 3.9 binary; Python's matrix now uses Ubuntu 22.04 and
independent matrix results. The corrected run is tracked in the draft PR.
