# Releasing the SDKs

All five SDKs are released together, at the same version, by pushing a tag.
[`Release`](.github/workflows/release.yml) then:

1. checks that every SDK carries that version (`scripts/check-versions.sh`);
2. runs the full [test suites](.github/workflows/test.yml); and
3. publishes:

| SDK | Registry | How |
|---|---|---|
| Node | npm, `carriercrow` | trusted publishing, with provenance |
| Python | PyPI, `carriercrow` | trusted publishing |
| Ruby | RubyGems, `carriercrow` | trusted publishing |
| Go | proxy.golang.org, `github.com/Frozen-Crow/carrier-crow-sdks/go` | a `go/vX.Y.Z` tag |
| PHP | Packagist, `carriercrow/carriercrow` | a `vX.Y.Z` tag on the [carrier-crow-php](https://github.com/Frozen-Crow/carrier-crow-php) mirror |

No registry token is stored anywhere. npm, PyPI and RubyGems each trust this
repository's `release.yml` workflow, running in the `release` environment.
Every publish step skips a version its registry already has, so a run that
failed halfway can be re-run.

## Cutting a release

1. Pick the version, following semver. Before 1.0, a breaking change bumps the minor version.
2. Set it in all eight places `scripts/check-versions.sh` lists, and check them:
   `scripts/check-versions.sh 0.2.0`.
3. Merge that change to `main`.
4. Tag `main` and push the tag:
   ```sh
   git tag v0.2.0 && git push origin v0.2.0
   ```
5. Watch the Release run in GitHub Actions. When it's green, check each registry page.

## Developing

Each SDK's tests run against local servers and the shared
[webhook vectors](test-vectors/webhooks.json), never the real service:

| SDK | From its folder |
|---|---|
| Node | `npm ci && npm test` |
| Python | `pip install -e '.[test]' && pytest -q && mypy src && ruff check .` |
| Go | `go vet ./... && go test ./...` |
| PHP | `composer install && composer test` |
| Ruby | `ruby -Ilib -Itest test/client_test.rb` |

## One-time setup

The repository side is done:
- this repository is public;
- the `release` environment exists;
- the `carrier-crow-php` mirror exists, with a write deploy key whose private
  half is the `PHP_MIRROR_DEPLOY_KEY` secret here.

The registry side needs an account owner. Use Frozen Crow organisation
accounts, not personal ones, and turn on two-factor authentication everywhere.
Each trusted publisher takes the same four values:

| Field | Value |
|---|---|
| Owner / organisation | `Frozen-Crow` |
| Repository | `carrier-crow-sdks` |
| Workflow file | `release.yml` |
| Environment | `release` |

**PyPI.** On pypi.org, go to *Your projects* → *Publishing* → *Add a new pending
publisher*, choose GitHub, and enter project name `carriercrow` and the four
values above. A pending publisher reserves the name until the first release
creates the project.

**RubyGems.** On rubygems.org, go to *Settings* → *Trusted publishers* → *Pending
trusted publishers*, and add gem `carriercrow`, type GitHub Actions, and the
four values above.

**npm.**
1. Create a `carriercrow` organisation on npmjs.com, or use an existing Frozen Crow one.
2. Try adding a trusted publisher for package `carriercrow` (*Package settings* →
   *Trusted Publisher* → GitHub Actions) with the four values above.
3. If npm only offers that once the package exists, publish 0.1.0 once by hand,
   then add the trusted publisher:
   ```sh
   cd node && npm ci && npm run build && npm publish --access public
   ```
   The Release run then sees 0.1.0 on npm and skips it. Later versions publish
   through the workflow. A trusted publisher must be used for a successful
   publish within two days of being created.
4. Once it works, set the package to *Require two-factor authentication and
   disallow tokens* so only the workflow can publish.

**Packagist.** After the first push of the mirror, sign in to packagist.org
with GitHub, choose *Submit*, and enter
`https://github.com/Frozen-Crow/carrier-crow-php`. With the GitHub sign-in,
Packagist updates itself on every push to the mirror.

**Go.** Nothing to set up. The first `go get` of a version, or the Release run's
request to proxy.golang.org, makes it appear on pkg.go.dev.

## The PHP mirror

Packagist reads `composer.json` only at the root of a repository, and ours is
in `php/`. [`PHP mirror`](.github/workflows/php-mirror.yml) copies `php/`,
with its history, to carrier-crow-php: its `main` follows ours whenever `php/`
changes, and a release adds the version tag. The mirror is read-only. Change
the code here, never there.
