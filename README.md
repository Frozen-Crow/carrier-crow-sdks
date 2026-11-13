# Carrier Crow SDKs

Clients for the Carrier Crow email API: send email over HTTP, read what
happened to it, check your domains, and verify webhook signatures.

| Language | Folder | Install |
|---|---|---|
| TypeScript / Node | [`node/`](node/) | `npm install carriercrow` |
| Python | [`python/`](python/) | `pip install carriercrow` |
| Go | [`go/`](go/) | `go get github.com/Frozen-Crow/carrier-crow-sdks/go` |
| PHP | [`php/`](php/) | `composer require carriercrow/carriercrow` |
| Ruby | [`ruby/`](ruby/) | `gem install carriercrow` |

The API itself is described in [`openapi/v1.yaml`](openapi/v1.yaml), and
[`CONTRACT.md`](CONTRACT.md) is the agreement between the API, the console
and these SDKs. Changes to either go through review before any code changes.

Prefer SMTP? Every app can still send over SMTP with the settings in the
console; the SDKs are for apps that would rather call an API.


All five clients support the API key environment variable
`CARRIERCROW_API_KEY`, a configurable API origin, typed errors, safe retries,
paging, and raw-body webhook verification. Sends retry only with an idempotency
key; each SDK also offers an explicit option to generate one. Wire names are
preserved exactly, including snake_case on sends and camelCase in email stories.
See each language README for source installation and complete examples.

Tests use local HTTP servers and the shared
[`webhook vectors`](test-vectors/webhooks.json), never the real service.
[`SDK tests`](.github/workflows/test.yml) runs every language on pushes and PRs;
it does not publish packages. Releases are published by
[`Release`](.github/workflows/release.yml); see [`RELEASING.md`](RELEASING.md).

MIT licensed; see [`LICENSE`](LICENSE).
