# kube-power

Tool for gracefully performing power maintenance on Kubernetes clusters
that rely on Rook-based Ceph storage.

## Usage

```
go install github.com/ianunruh/kube-power@latest
```

```
kube-power suspend
kube-power resume

kube-power phase SuspendOperators --dry-run
```
