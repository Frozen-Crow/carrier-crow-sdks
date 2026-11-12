# Carrier Crow SDKs

Clients for the Carrier Crow email API: send email over HTTP, read what
happened to it, check your domains, and verify webhook signatures.

| Language | Folder |
|---|---|
| TypeScript / Node | [`node/`](node/) |
| Python | [`python/`](python/) |
| Go | [`go/`](go/) |
| PHP | [`php/`](php/) |
| Ruby | [`ruby/`](ruby/) |

The API itself is described in [`openapi/v1.yaml`](openapi/v1.yaml), and
[`CONTRACT.md`](CONTRACT.md) is the agreement between the API, the console
and these SDKs. Changes to either go through review before any code changes.

Prefer SMTP? Every app can still send over SMTP with the settings in the
console; the SDKs are for apps that would rather call an API.

Not yet published to any package registry.
