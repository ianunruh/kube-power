# kube-power

Tool for gracefully performing power maintenance on Kubernetes clusters
that rely on Rook-based Ceph storage.

## Usage

### Argo Workflows

```
kubectl kustomize "https://github.com/ianunruh/kube-power.git/deploy/argo?ref=v1.0.0" | \
    kubectl apply -n kube-system -f-
```

### Manual

```
go install github.com/ianunruh/kube-power@latest
```

```
kube-power suspend
kube-power resume

kube-power phase SuspendOperators --dry-run
```
