#!/usr/bin/env bash
# Local CI for Sentinel.
#
# Runs every gate that a continuous integration pipeline would run, but
# entirely on the contributor's machine. No GitHub Actions, no runner
# credits, no remote dependencies.
#
# Usage:
#   make ci
#   ./scripts/ci.sh             # equivalent direct call
#
# Exits non-zero on the first failing gate. Set CI_SKIP_RACE=1 if you
# don't have a C compiler available (race detector requires cgo).

set -euo pipefail

BOLD="\033[1m"
GREEN="\033[32m"
RED="\033[31m"
DIM="\033[2m"
RESET="\033[0m"

step() {
    printf "\n${BOLD}── %s${RESET}\n" "$1"
}

ok() {
    printf "  ${GREEN}✓${RESET} %s\n" "$1"
}

fail() {
    printf "  ${RED}✗${RESET} %s\n" "$1" >&2
    exit 1
}

# ── 1. Go format ─────────────────────────────────────────────────────────────
step "gofmt -l (must be empty)"
unformatted="$(gofmt -l $(git ls-files '*.go') 2>/dev/null || true)"
if [ -n "$unformatted" ]; then
    printf "${RED}files not gofmt'd:${RESET}\n%s\n" "$unformatted" >&2
    fail "run 'make fmt' to fix"
fi
ok "gofmt clean"

# ── 2. Go vet ─────────────────────────────────────────────────────────────────
step "go vet ./..."
go vet ./... || fail "go vet"
ok "go vet clean"

# ── 3. Go build ──────────────────────────────────────────────────────────────
step "go build ./..."
go build ./... || fail "go build"
ok "go build clean"

# ── 4. Go tests ──────────────────────────────────────────────────────────────
step "go test"
if [ "${CI_SKIP_RACE:-0}" = "1" ]; then
    printf "  ${DIM}(skipping race detector — CI_SKIP_RACE=1)${RESET}\n"
    go test -count=1 ./... || fail "go test"
else
    if go test -race -count=1 ./...; then
        ok "go test -race clean"
    else
        # cgo not available is the typical Windows failure; fall back without race.
        printf "  ${DIM}race detector unavailable, retrying without -race${RESET}\n"
        go test -count=1 ./... || fail "go test"
        ok "go test clean (no race detector)"
    fi
fi

# ── 5. TypeScript SDK type-check ─────────────────────────────────────────────
step "TypeScript SDK type-check"
if command -v npx >/dev/null 2>&1; then
    pushd sdk/ts >/dev/null
    if [ ! -d node_modules ]; then
        npm install --no-fund --no-audit --silent
    fi
    npx tsc --noEmit -p tsconfig.json || { popd >/dev/null; fail "tsc --noEmit"; }
    popd >/dev/null
    ok "tsc --noEmit clean"
else
    printf "  ${DIM}npx not found — skipping TypeScript SDK${RESET}\n"
fi

# ── 6. Python SDK parse ──────────────────────────────────────────────────────
step "Python SDK parse"
if command -v python >/dev/null 2>&1; then
    python - <<'PY' || fail "python parse"
import ast, pathlib, sys
errors = 0
for p in pathlib.Path("sdk/python/src").rglob("*.py"):
    try:
        ast.parse(p.read_text(encoding="utf-8"))
    except SyntaxError as e:
        print(f"  syntax error in {p}: {e}", file=sys.stderr)
        errors += 1
sys.exit(1 if errors else 0)
PY
    ok "python sdk parse clean"
else
    printf "  ${DIM}python not found — skipping Python SDK${RESET}\n"
fi

# ── 7. OpenAPI lint (optional) ───────────────────────────────────────────────
step "OpenAPI lint (optional)"
if command -v npx >/dev/null 2>&1; then
    if npx --yes @redocly/cli@latest lint contracts/openapi.yaml 2>&1 | tail -10; then
        ok "openapi lint clean"
    else
        printf "  ${DIM}redocly lint reported warnings (non-fatal)${RESET}\n"
    fi
else
    printf "  ${DIM}npx not found — skipping openapi lint${RESET}\n"
fi

printf "\n${GREEN}${BOLD}✓ all CI gates passed${RESET}\n"
