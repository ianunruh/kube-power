package controller

type CephCluster struct {
	Status CephClusterStatus
}

type CephClusterStatus struct {
	Ceph CephClusterCephStatus
}

type CephClusterCephStatus struct {
	Health string
}

func (s CephClusterCephStatus) HealthOK() bool {
	return s.Health == "HEALTH_OK"
}
