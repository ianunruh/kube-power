package controller

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

var volumeCSIDriverFilter = []string{
	"rook-ceph.cephfs.csi.ceph.com",
	"rook-ceph.rbd.csi.ceph.com",
}

func deploymentHasClaim(deploy *appsv1.Deployment, claim *corev1.PersistentVolumeClaim) bool {
	for _, volume := range deploy.Spec.Template.Spec.Volumes {
		if spec := volume.PersistentVolumeClaim; spec != nil && spec.ClaimName == claim.Name {
			return true
		}
	}
	return false
}

func statefulSetHasClaim(sts *appsv1.StatefulSet, claim *corev1.PersistentVolumeClaim) bool {
	for _, volume := range sts.Spec.Template.Spec.Volumes {
		if spec := volume.PersistentVolumeClaim; spec != nil && spec.ClaimName == claim.Name {
			return true
		}
	}
	for _, template := range sts.Spec.VolumeClaimTemplates {
		if strings.HasPrefix(claim.Name, stsClaimPrefix(sts.Name, template.Name)) {
			return true
		}
	}
	return false
}

func filterCSIDriver(name string) bool {
	for _, driver := range volumeCSIDriverFilter {
		if driver == name {
			return true
		}
	}
	return false
}

func stsClaimPrefix(stsName, claimName string) string {
	return fmt.Sprintf("%s-%s-", claimName, stsName)
}
