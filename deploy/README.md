# Deploying agrelha

Two supported shapes. Both start with no required configuration beyond picking
a runtime; see [docs/configuration.md](../docs/configuration.md) for what each
optional key turns on.

## Kubernetes (Helm)

```sh
helm install agrelha ./deploy/helm/agrelha \
  --namespace agrelha --create-namespace \
  --set image.repository=ghcr.io/OWNER/agrelha \
  --set 'gameNamespaces={valheim,minecraft-modded}'
```

The namespaces you list must already exist — the chart binds a Role in each, it
does not create them. agrelha is never granted cluster-wide permissions: per
namespace it can read pods, logs and pod metrics, and scale or patch
Deployments. ConfigMaps are read-only, because edits travel through the
declarative plane rather than the API.

Reach the panel with a port-forward first, create an account, then expose it:

```sh
kubectl -n agrelha port-forward svc/agrelha 8080:8080
```

Enable the declarative plane and SSO when you want them:

```yaml
git:
  repoURL: https://codeberg.org/you/ops.git
  username: agrelha-bot
  token: <token>        # or git.existingSecret with GIT_USERNAME/GIT_TOKEN
oidc:
  enabled: true
  issuer: https://id.example.org
  clientID: agrelha
  redirectURL: https://agrelha.example.org/auth/callback
  clientSecret: <secret> # or oidc.existingSecret with OIDC_CLIENT_SECRET
allowedEmail: you@example.org
```

Expose it with either `ingress.enabled` (classic Ingress) or
`httpRoute.enabled` (Gateway API).

## Docker (compose)

```sh
docker compose -f deploy/docker-compose.yml up -d
```

`RUNTIME: docker` is the only variable that must be set, and the file sets it.
State lives in the `agrelha-data` volume.

Mounting the Docker socket grants control of the Docker daemon, which is
equivalent to root on the host. Run agrelha only on a host you would trust it
with.
