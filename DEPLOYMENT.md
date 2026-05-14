# Deployment Guide

## Environment Variables

```bash
# Database
DB_HOST=localhost        # PostgreSQL host
DB_USER=postgres         # PostgreSQL user
DB_PASSWORD=postgres     # PostgreSQL password
DB_NAME=wallet_transfer  # Database name
DB_SSLMODE=disable       # SSL mode (disable/require for prod)

# Server
SERVER_ADDR=:8080        # Server listen address
```

## Production Checklist

- [ ] Enable SSL mode in production: `DB_SSLMODE=require`
- [ ] Use strong database password
- [ ] Set up connection pooling (25 connections)
- [ ] Configure logging level: `INFO` for production
- [ ] Set up monitoring and alerting
- [ ] Enable database backups
- [ ] Configure graceful shutdown (10s timeout)
- [ ] Set resource limits (CPU, memory)
- [ ] Use health check endpoint: `GET /health`
- [ ] Document service dependencies

## Docker Deployment

### Build Image

```bash
docker build -f docker/Dockerfile -t wallet-transfer:latest .
```

### Run Container

```bash
docker run -p 8080:8080 \
  -e DB_HOST=postgres \
  -e DB_USER=postgres \
  -e DB_PASSWORD=postgres \
  -e DB_NAME=wallet_transfer \
  wallet-transfer:latest
```

### Kubernetes Deployment (Example)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: wallet-transfer-api
spec:
  replicas: 3
  selector:
    matchLabels:
      app: wallet-transfer-api
  template:
    metadata:
      labels:
        app: wallet-transfer-api
    spec:
      containers:
      - name: api
        image: wallet-transfer:latest
        ports:
        - containerPort: 8080
        env:
        - name: DB_HOST
          value: postgres-service
        - name: DB_USER
          valueFrom:
            secretKeyRef:
              name: db-credentials
              key: username
        - name: DB_PASSWORD
          valueFrom:
            secretKeyRef:
              name: db-credentials
              key: password
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"
```

## Monitoring

### Recommended Metrics

- Request rate (requests/sec)
- Response time (p50, p95, p99)
- Error rate
- Transfer success rate
- Database connection pool usage
- Transaction commit/rollback rate
- Idempotency cache hit rate

### Recommended Alerts

- Error rate > 1%
- Response time p99 > 1 second
- Database connection pool exhaustion
- Transfer failures (insufficient funds, etc.)
- Transaction deadlocks
