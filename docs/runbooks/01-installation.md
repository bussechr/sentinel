# Runbook: Installation

## Prerequisites

- Docker ≥ 24 with Compose V2
- Go 1.23 (for building from source)
- `kubectl` + a Kubernetes cluster (for K8s deployment)

## Local Dev (Compose)

```bash
# 1. Clone the repository
git clone https://github.com/your-org/sentinel.git
cd sentinel

# 2. Create the secrets placeholder (dev DSN, not for production)
make secrets-init

# 3. Start the full stack
make compose-up

# 4. Wait for all services to be healthy
docker compose -f deploy/compose/docker-compose.yml ps

# 5. Verify
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz

# 6. Register a test app
bin/sentinelctl register app \
  --app-id billing-api \
  --env dev \
  --owner platform \
  --mode observe

# 7. Emit a test packet
bin/sentinelctl emit-test-packet \
  --app-id billing-api \
  --action invoice.refund.create \
  --risk high \
  --mutating true

# 8. Run sentinelctl doctor
bin/sentinelctl doctor
```

## Kubernetes (Helm)

```bash
# 1. Create namespace
kubectl create namespace sentinel

# 2. Create the Postgres DSN secret
kubectl create secret generic sentinel-postgres \
  --namespace sentinel \
  --from-literal=dsn='postgresql://sentinel:CHANGE_ME@postgres:5432/sentinel?sslmode=require'

# 3. Install with Helm
helm install sentinel deploy/helm/sentinel \
  --namespace sentinel \
  --set global.registry=your-registry \
  --set global.imageTag=v1

# 4. Verify
kubectl rollout status deployment/sentinel-sentinel-api -n sentinel
kubectl exec -n sentinel deployment/sentinel-sentinel-api -- \
  wget -qO- http://localhost:8080/healthz
```

## Production Checklist

- [ ] Replace `make secrets-init` placeholder with real Postgres credentials
- [ ] Enable mTLS (`api.mtls_required: true` + cert-manager)
- [ ] Deploy SPIRE agent DaemonSet alongside sentinel-agent
- [ ] Configure OPA bundle URL to a signed bundle endpoint
- [ ] Set `ledger.fail_closed_for: [high, critical]`
- [ ] Confirm OTel Collector is exporting to your metrics backend
- [ ] Run `sentinelctl doctor` and resolve all warnings before go-live
