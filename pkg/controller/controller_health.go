package controller

import (
	"context"
	"encoding/json"
	"time"
)

func (c *Controller) WaitForCephHealthOK() error {
	c.log.Info("Waiting for Ceph cluster to reach HEATLH_OK")

	for {
		ok, err := c.IsCephHealthOK()
		if err != nil {
			return err
		} else if ok {
			c.log.Info("Ceph cluster has reached HEALTH_OK")
			return nil
		}

		// wait for cluster state to converge
		time.Sleep(10 * time.Second)
	}
}

func (c *Controller) IsCephHealthOK() (bool, error) {
	encoded, err := c.clientset.RESTClient().
		Get().
		AbsPath("/apis/ceph.rook.io/v1").
		Namespace("rook-ceph").
		Name("rook-ceph").
		Resource("cephclusters").
		DoRaw(context.Background())
	if err != nil {
		return false, err
	}

	cluster := CephCluster{}
	if err := json.Unmarshal(encoded, &cluster); err != nil {
		return false, err
	}

	return cluster.Status.Ceph.HealthOK(), nil
}
