# k8s/ — cluster manifests for shields-farm/kubernetes

This directory is the source ArgoCD watches. The `shields-farm/kubernetes`
repo carries an Application that points here.

## Deploy

```bash
# Render check
kubectl kustomize k8s/

# Dry-run against the cluster
kubectl apply -k k8s/ --dry-run=server

# Apply directly (Argo CD does this for you once the Application is live)
kubectl apply -k k8s/

# Set the default RP user id (write-path server-wide default; find yours in
# the network tab while signed into training.rpstrength.com). Optional.
kubectl -n rp-app-graph-backend create secret generic rp-app-graph-backend-config \
  --from-literal=rp-user-id=<your-rp-user-id>
```

## Image

The deployment pulls `ghcr.io/shields-farm/rp-app-graph-backend:latest`,
built and pushed by this repo's CI on every merge to main (no one builds
images on their machine).

**One-time step:** the ghcr package is private by default after first publish.
Flip it once at GitHub → shields-farm → Packages → rp-app-graph-backend →
Package settings → Change visibility → public. After that the cluster pulls
anonymously and there is no pull secret to manage.

The ArgoCD Application lives in `shields-farm/argocd-app` at
`prod/rp-app-graph-backend.yaml` (app-of-apps pattern).
