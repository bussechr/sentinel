# Contributing to Sentinel

Thanks for considering a contribution. This document captures the few
non-obvious things that keep the Sentinel codebase consistent.

## Toolchain

- **Go**: 1.25 or newer. The immudb client pulls Go 1.25 forward, so
  earlier toolchains will fail at `go build`.
- **Docker**: required for `make compose-up` (Postgres, MinIO, OPA, OTel,
  CometBFT all run as containers).
- **psql**: required for `make migrate` against an existing Postgres.
- **Node 20+**: required to type-check the TypeScript SDK (`make sdk-ts`).
- **Python 3.10+**: required to sanity-check the Python SDK (`make sdk-py`).

## Local workflow

```bash
make tidy        # sync go.mod / go.sum
make build       # build all four binaries into ./bin
make vet         # go vet ./...
make test        # go test -race -count=1 ./...
make ci          # run every CI gate locally (gofmt, vet, build, test, SDKs, openapi)
```

**Before pushing a change, run `make ci`.** This repo deliberately does
not run continuous integration on GitHub Actions — every gate is the
local `scripts/ci.sh` script, so contributors decide when their machine
spends time on it. The script falls back gracefully when a tool is
missing (Node, Python, gcc), so partial environments still work.

For the full dev stack:

```bash
make compose-up         # Postgres, MinIO, OPA, OTel, CometBFT, sentinel-*
make doctor             # exercise sentinelctl against the running api
make compose-down       # tear down (and remove volumes)
```

## Code conventions

- Public packages live under `internal/` unless they are explicitly
  exported via `sdk/`. Adding a package outside `internal/` is a
  deliberate API decision — please raise it in the PR.
- The canonical packet schema is `internal/core` ([packet.go](internal/core/packet.go)).
  Breaking changes ripple through the SDKs and the OPA bundle's `input`
  shape; accompany them with a migration note and bump
  `core.SchemaVersion`.
- Prefer constructor functions like `NewWriter`, `NewArchiver` over
  zero-value structs. Keep dependencies passed in explicitly so handlers
  remain testable with `nil` stores.
- Ledger backends conform to the [`Writer`](internal/ledger/writer.go)
  interface. New backends must:
  - return a stable `Kind()` and a configurable `Name()`
  - support a no-network mode keyed off an empty endpoint, so
    `make test` does not require external services
- Keep the OpenAPI spec ([contracts/openapi.yaml](contracts/openapi.yaml))
  in lockstep with handler changes. SDKs derive their typed surface from
  it.

## Testing rules

- Tests run with `-race`; goroutine starts must not leak past the test.
- Tests **must not** require live Postgres, MinIO, CometBFT, Besu, or
  immudb. Use empty-endpoint shadow paths or fake stores.
- Each new HTTP handler should have at least one happy-path test plus
  one negative test (missing field, wrong method, or bypass attempt).
- New ledger backends should ship a test that covers Submit + Verify
  using the in-memory shadow path.

## Commit / PR style

- Use conventional-style subject lines (`feat:`, `fix:`, `docs:`,
  `refactor:`, `test:`, `chore:`).
- Keep PRs scoped — one capability per PR is easier to review than a
  large bundle. The repo's recent history is a good template.
- If your change touches the HTTP surface, update the OpenAPI spec
  **and** at least one SDK in the same PR.

## Reporting issues

Please include:

1. The Sentinel commit SHA you are running.
2. Whether you are running the Compose stack, Helm chart, or a custom
   deploy.
3. The output of `sentinelctl doctor` against the affected control plane.
4. The minimal reproduction (a packet, a config snippet, a log line).

## Security disclosures

Please **do not** open public issues for security findings. Email the
maintainers privately or use GitHub's private vulnerability reporting on
the upstream repository.
