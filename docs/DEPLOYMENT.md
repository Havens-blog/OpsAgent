# OpsAgent Deployment Guide

## Prerequisites

- Docker 20.10+
- Docker Compose 2.0+
- Kubernetes 1.20+ (for K8s deployment)
- Anthropic API Key

## Quick Start with Docker Compose

1. **Clone the repository:**
   ```bash
   git clone https://github.com/your-org/OpsAgent.git
   cd OpsAgent
   ```

2. **Configure environment variables:**
   ```bash
   cp .env.example .env
   # Edit .env and add your ANTHROPIC_API_KEY
   ```

3. **Start the services:**
   ```bash
   docker-compose up -d
   ```

4. **Verify deployment:**
   ```bash
   docker-compose ps
   curl http://localhost:8080/health
   ```

## Services

- **opsagent**: Main application (ports 8080, 9090)
- **prometheus**: Metrics storage (port 9091)
- **grafana**: Visualization dashboard (port 3000)

### Access Points

- OpsAgent API: http://localhost:8080
- Health Check: http://localhost:8080/health
- Prometheus: http://localhost:9091
- Grafana: http://localhost:3000 (admin/admin)

## Kubernetes Deployment

1. **Create namespace and secrets:**
   ```bash
   kubectl create namespace opsagent
   kubectl create secret generic opsagent-secrets \
     --from-literal=anthropic-api-key=your-api-key \
     -n opsagent
   ```

2. **Apply deployment:**
   ```bash
   kubectl apply -f deployments/kubernetes/deployment.yaml
   kubectl apply -f deployments/kubernetes/ingress.yaml
   ```

3. **Verify deployment:**
   ```bash
   kubectl get pods -n opsagent
   kubectl get services -n opsagent
   ```

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `OPSAGENT_ENVIRONMENT` | Deployment environment | `production` |
| `OPSAGENT_HOST` | Server host | `0.0.0.0` |
| `OPSAGENT_PORT` | Server port | `8080` |
| `OPSAGENT_LOG_LEVEL` | Log level | `info` |
| `ANTHROPIC_API_KEY` | Anthropic API key | - |

### Configuration File

Edit `config/settings.yaml` to customize:

```yaml
environment: production
server:
  host: "0.0.0.0"
  port: 8080
agent:
  model: "claude-sonnet-4-20250514"
  max_turns: 10
  timeout: 300
  max_parallel_agents: 5
observability:
  log_level: "info"
  log_format: "json"
  enable_metrics: true
  metrics_port: 9090
```

## Monitoring

### Prometheus Metrics

Access metrics at: http://localhost:9090/metrics

Key metrics:
- `opsagent_rca_analyses_total`
- `opsagent_rca_duration_seconds`
- `opsagent_api_requests_total`
- `opsagent_api_response_time_seconds`

### Grafana Dashboards

Import dashboards from `deployments/grafana/dashboards/` or configure provisioning.

## Health Checks

### Docker Compose
```bash
docker-compose ps
curl http://localhost:8080/health
```

### Kubernetes
```bash
kubectl get pods -n opsagent
kubectl describe pod <pod-name> -n opsagent
kubectl logs <pod-name> -n opsagent
```

## Troubleshooting

### Logs

**Docker Compose:**
```bash
docker-compose logs -f opsagent
docker-compose logs -f prometheus
```

**Kubernetes:**
```bash
kubectl logs -f deployment/opsagent -n opsagent
```

### Common Issues

1. **API Key Not Found**
   - Verify `ANTHROPIC_API_KEY` is set
   - Check secret in Kubernetes: `kubectl get secret opsagent-secrets -n opsagent`

2. **Port Already in Use**
   - Change ports in docker-compose.yml
   - Or stop conflicting services

3. **Health Check Failing**
   - Check logs for errors
   - Verify configuration
   - Ensure all dependencies are running

## Scaling

### Docker Compose

```bash
docker-compose up -d --scale opsagent=3
```

### Kubernetes

Configure HPA in `deployments/kubernetes/ingress.yaml` or manually:

```bash
kubectl scale deployment opsagent --replicas=5 -n opsagent
```

## Security Recommendations

1. **Always use HTTPS in production**
2. **Rotate API keys regularly**
3. **Use secret management (Vault, AWS Secrets Manager)**
4. **Enable audit logging**
5. **Implement RBAC for Kubernetes**
6. **Scan images for vulnerabilities**
7. **Keep dependencies updated**

## Backup and Recovery

### Configuration Backup

```bash
# Backup
kubectl get configmap opsagent-config -n opsagent -o yaml > backup-config.yaml

# Restore
kubectl apply -f backup-config.yaml -n opsagent
```

### Data Backup

Data is stored in `./data` directory. Back up regularly.

## Updates

### Docker Compose

```bash
docker-compose pull
docker-compose up -d --force-recreate
```

### Kubernetes

```bash
kubectl set image deployment/opsagent \
  opsagent=opsagent:new-version -n opsagent
```
