# kube-power

Tool for gracefully performing power maintenance on Kubernetes clusters
that rely on Rook-based Ceph storage.

## Features

* Identifies any Deployments and StatefulSets that use Rook-based storage
  and scales them to zero replicas
* Stops Ceph components in specific order to ensure data integrity
* Applies flags to the Ceph cluster to prevent unnecessary rebalancing
* Waits for Ceph to become healthy before resuming workflows that rely on it

## Usage

### Argo Workflows

If Argo Workflows is available in your Kubernetes cluster and it does not rely on
Rook-based Ceph storage to operate, then kube-power can be triggered by an operator
from a WorkflowTemplate.

```
kubectl kustomize "https://github.com/ianunruh/kube-power.git/deploy/argo?ref=v1.0.0" | \
    kubectl apply -n kube-system -f-
```

### Jobs

```
git clone https://github.com/ianunruh/kube-power.git
cd kube-power

# Edit job.yaml as needed for performing different steps
kubectl apply -k deploy/job
```

### Locally

```
go install github.com/ianunruh/kube-power@v1.0.0
```

```
kube-power suspend
kube-power resume

kube-power phase SuspendOperators --dry-run
```
