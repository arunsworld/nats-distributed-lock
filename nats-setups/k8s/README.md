# Setup

```bash
helm repo add nats https://nats-io.github.io/k8s/helm/charts/

helm install nats nats/nats -f values.yml
```

# Appendix

```bash
helm repo update nats

helm uninstall nats

helm show values nats/nats

helm template nats nats/nats -f values.yml
```