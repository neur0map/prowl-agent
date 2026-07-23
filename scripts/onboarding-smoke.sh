#!/usr/bin/env bash
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
bin="$tmp/prowl-agent"
project="$tmp/project"

cd "$repo"
CGO_ENABLED=1 go build -tags sqlite_fts5 -o "$bin" ./cmd/prowl-agent

mkdir -p "$project/.cursor"
printf 'package demo\n\nfunc Hello() string { return "hello" }\n' > "$project/main.go"
printf '# Demo\n' > "$project/README.md"

cd "$project"
"$bin" init --dry-run --json --integrations cursor,agents > "$tmp/plan.json"
python3 - "$tmp/plan.json" <<'PY'
import json, pathlib, sys
payload = json.loads(pathlib.Path(sys.argv[1]).read_text())
assert payload['dry_run'] is True
assert {a['integration'] for a in payload['plan']['actions']} == {'agents', 'cursor'}
PY
test ! -e .prowl

"$bin" init --no-ai --no-input --json --integrations cursor,agents > "$tmp/init.json"
python3 - "$tmp/init.json" <<'PY'
import json, pathlib, sys
report = json.loads(pathlib.Path(sys.argv[1]).read_text())
assert report['indexed']['Indexed'] == 2, report
assert report['integrations'] == ['agents', 'cursor'], report
PY
test -f .cursor/mcp.json
test -f AGENTS.md

"$bin" overview --format human | grep -q 'Project overview'
"$bin" overview --format json | python3 -m json.tool >/dev/null
"$bin" doctor --json | python3 -m json.tool >/dev/null

"$bin" init --remove-integrations --no-input --json --integrations cursor,agents > "$tmp/remove.json"
test ! -e AGENTS.md
python3 - <<'PY'
import json, pathlib
cfg = json.loads(pathlib.Path('.cursor/mcp.json').read_text())
assert 'prowl-agent' not in cfg.get('mcpServers', {})
PY

echo 'onboarding smoke test passed'
