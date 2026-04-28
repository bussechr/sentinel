# Runbook: Installation

## Prerequisites

- Docker ≥ 24 with Compose V2
- Go 1.25 (for building from source — required by the immudb client)
- `psql` if you intend to run migrations against a Postgres you already operate
- `kubectl` and a Kubernetes cluster (for the Helm path)
- Optional, for SDK contributors: Node ≥ 20 (TypeScript SDK) and Python ≥ 3.10 (Python SDK)

## Local dev (Compose)

The bundled stack brings up Postgres, MinIO (S3-compatible cold archive),
OPA, the OTel Collector, CometBFT, and the three Sentinel binaries
(`sentinel-api`, `sentinel-agent`, `sentinel-chain-app`).

```bash
# 1. Clone
git clone https://github.com/your-org/sentinel.git
cd sentinel

# 2. Write a placeholder Postgres DSN secret (dev only — never for prod)
make secrets-init

# 3. Bring up the stack. Migrations under
#    internal/store/migrations auto-apply on first boot because the
#    Postgres container mounts that directory at /docker-entrypoint-initdb.d.
make compose-up

# 4. Wait for everything to be healthy
docker compose -f deploy/compose/docker-compose.yml ps

# 5. Build the local CLI/binaries
make build

# 6. Run the doctor against the running stack — exercises four endpoints:
#    /healthz, /readyz, /v1/ledger/writers, /v1/policy/bundles
SENTINEL_API_ENDPOINT=http://localhost:8080 ./bin/sentinelctl doctor

# 7. Register a test app + emit a packet end-to-end
./bin/sentinelctl register app \
  --app-id billing-api --service billing --env dev --owner platform --mode observe

./bin/sentinelctl emit-test-packet \
  --app-id billing-api --action invoice.refund.create --risk high --mutating

# 8. Confirm multi-backend ledger health
./bin/sentinelctl writers
```

`./bin/sentinelctl writers` lists every registered ledger writer with its
kind (`cometbft`, `besu`, `immudb`), name, current height, and whether it
is healthy. The default registry built by the dev stack registers the
CometBFT writer; Besu and immudb writers are wired in production
deployments via `sentinel.yaml` and a writer registry bootstrap.

## Migrations against an existing Postgres

If you bring your own Postgres instead of the Compose container:

```bash
export SENTINEL_POSTGRES_DSN="postgres://user:pass@host:5432/sentinel?sslmode=require"
make migrate
```

`make migrate` walks every `*.sql` file under `internal/store/migrations`
in lexical order with `psql -v ON_ERROR_STOP=1`. Two migrations ship today:

- `001_initial.sql` — apps, packets, decisions, receipts, segments, AI traces, anchor queue, config revisions.
- `002_replay_and_shadow.sql` — replay-friendly receipt columns (`correlation_id`, `evidence_root_hash`, `writer_kind`, `writer_name`), `shadow_decisions`, `cold_archive_index`.

## Kubernetes (Helm)

```bash
# 1. Namespace
kubectl create namespace sentinel

# 2. Secrets
kubectl create secret generic sentinel-postgres \
  --namespace sentinel \
  --from-literal=dsn='postgresql://sentinel:CHANGE_ME@postgres:5432/sentinel?sslmode=require'

# 3. Install
helm install sentinel deploy/helm/sentinel \
  --namespace sentinel \
  --set global.registry=your-registry \
  --set global.imageTag=v1

# 4. Verify
kubectl rollout status deployment/sentinel-sentinel-api -n sentinel
kubectl exec -n sentinel deployment/sentinel-sentinel-api -- \
  wget -qO- http://localhost:8080/healthz
```

## Production checklist

- [ ] Replace the dev Postgres DSN with real, rotated credentials mounted as a secret.
- [ ] Enable mTLS on the API (`api.mtls_required: true`) and front it with cert-manager / Vault PKI.
- [ ] Deploy a SPIRE agent DaemonSet alongside `sentinel-agent` for SVID-based identity.
- [ ] Configure the OPA bundle URL to a signed bundle endpoint; promote bundles via `simulate-policy` and the shadow path (see runbook 03).
- [ ] Set `ledger.fail_closed_for: [high, critical]` in `sentinel.yaml`.
- [ ] Wire **at least one real ledger writer**:
      CometBFT (default app chain) requires a 64-byte ed25519 signing key from a mounted secret;
      Besu/QBFT requires an `EVMSigner` plugged in (go-ethereum, web3signer, an HSM);
      immudb requires `endpoint`, `database`, `username`, password.
- [ ] Provision an S3-compatible bucket and wire `evidence.NewObjectStoreSink` for cold archives.
- [ ] Confirm the OTel Collector is exporting to your metrics backend.
- [ ] Run `sentinelctl doctor` and `sentinelctl writers` against the live cluster — both must report green before go-live.
