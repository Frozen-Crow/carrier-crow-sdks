#!/usr/bin/env bash
# Every SDK must carry the version being released, both in its package
# metadata and in the version it reports in its User-Agent.
# Usage: scripts/check-versions.sh 0.1.0
set -euo pipefail
want="${1:?usage: check-versions.sh <version>}"
cd "$(dirname "$0")/.."

found() {
  local where="$1" got="$2"
  if [[ "$got" == "$want" ]]; then
    echo "ok    $where $got"
  else
    echo "WRONG $where has '${got}', expected $want" >&2
    status=1
  fi
}
status=0
found node/package.json "$(node -p 'require("./node/package.json").version')"
found node/src/index.ts "$(sed -nE 's/^export const VERSION = "([^"]+)".*/\1/p' node/src/index.ts)"
found python/pyproject.toml "$(sed -nE 's/^version = "([^"]+)".*/\1/p' python/pyproject.toml)"
found python/src/carriercrow/__init__.py "$(sed -nE 's/^__version__ = "([^"]+)".*/\1/p' python/src/carriercrow/__init__.py)"
found ruby/carriercrow.gemspec "$(sed -nE "s/^  s\.version = '([^']+)'.*/\1/p" ruby/carriercrow.gemspec)"
found ruby/lib/carriercrow.rb "$(sed -nE "s/^  VERSION = '([^']+)'.*/\1/p" ruby/lib/carriercrow.rb)"
found php/src/CarrierCrow.php "$(sed -nE "s/^    public const VERSION = '([^']+)'.*/\1/p" php/src/CarrierCrow.php)"
found go/client.go "$(sed -nE 's/^const Version = "([^"]+)".*/\1/p' go/client.go)"
exit $status
