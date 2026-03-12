# FlowForge Deployment Guide

## Prerequisites

- Go 1.22+
- Docker & Docker Compose
- PostgreSQL 15+
- Redis 7+
- NATS 2.10+ with JetStream enabled

## Quick Start (Docker Compose)

The fastest way to get FlowForge running:

```bash
# Clone the repository
git clone https://github.com/kasidit-wansudon/flowforge.git
cd flowforge

# Start all services
docker-compose up -d

# Run database migrations
docker-compose exec server /app/migrate up

# Verify health
curl http://localhost:8080/api/v1/health
```

The following services will be available:

| Service | URL |
|---------|-----|
| FlowForge UI | http://localhost:8080 |
| REST API | http://localhost:8080/api/v1 |
| gRPC | localhost:9090 |
| Prometheus | http://localhost:9090 |
| Grafana | http://localhost:3001 |
| NATS Monitoring | http://localhost:8222 |

## Local Development

### Build from Source

```bash
# Install dependencies
go mod download

# Build all binaries
make build

# Run migrations
./bin/migrate up

# Start server (in one terminal)
./bin/server

# Start worker (in another terminal)
./bin/worker
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP server port |
| `GRPC_PORT` | `9090` | gRPC server port |
| `DATABASE_URL` | `postgres://flowforge:flowforge@localhost:5432/flowforge?sslmode=disable` | PostgreSQL connection string |
| `REDIS_URL` | `redis://localhost:6379/0` | Redis connection string |
| `NATS_URL` | `nats://localhost:4222` | NATS server URL |
| `JWT_SECRET` | (required) | JWT signing secret |
| `LOG_LEVEL` | `info` | Log level: debug, info, warn, error |
| `WORKER_CONCURRENCY` | `10` | Max concurrent tasks per worker |
| `WORKER_ID` | (auto) | Worker identifier |

### Makefile Targets

```bash
make help           # Show all available targets
make build          # Build all binaries
make test           # Run all tests
make lint           # Run linter
make fmt            # Format code
make docker-build   # Build Docker images
make run-server     # Run API server locally
make run-worker     # Run worker locally
make migrate-up     # Run database migrations
make migrate-down   # Rollback last migration
make proto-gen      # Generate protobuf code
make clean          # Clean build artifacts
```

## Kubernetes Deployment

### Using kubectl

```bash
# Create namespace
kubectl apply -f deploy/k8s/namespace.yaml

# Deploy infrastructure
kubectl apply -f deploy/k8s/postgres-statefulset.yaml
kubectl apply -f deploy/k8s/redis-deployment.yaml
kubectl apply -f deploy/k8s/nats-deployment.yaml
kubectl apply -f deploy/k8s/configmap.yaml
kubectl apply -f deploy/k8s/secrets.yaml

# Deploy FlowForge
kubectl apply -f deploy/k8s/server-deployment.yaml
kubectl apply -f deploy/k8s/worker-deployment.yaml
```

### Using Helm

```bash
# Add the chart
helm install flowforge deploy/helm/flowforge \
  --namespace flowforge \
  --create-namespace \
  --set server.replicas=2 \
  --set worker.replicas=3 \
  --set postgresql.auth.password=your-secure-password \
  --set auth.jwtSecret=your-jwt-secret

# Upgrade
helm upgrade flowforge deploy/helm/flowforge \
  --namespace flowforge \
  --set worker.replicas=5

# Uninstall
helm uninstall flowforge --namespace flowforge
```

### Scaling Workers

Workers scale horizontally. The Kubernetes HPA is configured to scale based on CPU utilization:

```yaml
# Automatic scaling (configured in HPA)
minReplicas: 2
maxReplicas: 20
targetCPUUtilizationPercentage: 70
```

Manual scaling:
```bash
kubectl scale deployment flowforge-worker --replicas=10 -n flowforge
```

## Production Checklist

### Security
- [ ] Set strong JWT_SECRET (min 32 characters)
- [ ] Enable TLS for all services
- [ ] Configure network policies in Kubernetes
- [ ] Rotate API keys regularly
- [ ] Set up PostgreSQL SSL connections
- [ ] Configure Redis AUTH

### Reliability
- [ ] Set up PostgreSQL replication
- [ ] Configure Redis Sentinel or Cluster
- [ ] Deploy NATS in clustered mode (3+ nodes)
- [ ] Set resource limits on all containers
- [ ] Configure PodDisruptionBudgets

### Monitoring
- [ ] Deploy Prometheus and Grafana
- [ ] Import FlowForge Grafana dashboards
- [ ] Set up alerting rules for:
  - High error rate (>5%)
  - Queue depth exceeding threshold
  - Worker count below minimum
  - Database connection pool exhaustion
- [ ] Configure log aggregation (Loki, ELK, etc.)

### Backup
- [ ] Schedule PostgreSQL backups (pg_dump or WAL archiving)
- [ ] Back up Redis RDB snapshots
- [ ] Archive workflow definitions to S3

## Troubleshooting

### Common Issues

**Workers not claiming tasks**
- Check NATS connectivity: `nats sub "flowforge.>"`
- Verify worker registration: `flowforge worker list`
- Check worker logs for errors

**Workflow stuck in "Running"**
- Check for failed tasks: `flowforge run status <id>`
- Verify worker health: workers may have crashed
- Check for dependency cycles (should be caught at validation)

**High memory usage**
- Reduce WORKER_CONCURRENCY
- Check for large task outputs consuming memory
- Enable Redis caching to reduce DB load

**Database connection errors**
- Check connection pool limits (default: 25)
- Verify DATABASE_URL is correct
- Check PostgreSQL max_connections setting
