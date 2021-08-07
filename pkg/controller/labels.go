package controller

import (
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

const (
	SuspendedKey      = "k8s.ianunruh.com/suspended"
	SuspendedClassKey = "k8s.ianunruh.com/suspended-class"

	CephConsumerClass = "ceph-consumer"
	CephDaemonClass   = "ceph-daemon"
	OperatorClass     = "operator"
	CronJobClass      = "cronjob"
)

func appSelector(name string) labels.Selector {
	selector := labels.NewSelector()
	req, _ := labels.NewRequirement("app", selection.Equals, []string{name})
	return selector.Add(*req)
}

func rookDaemonSelector(name string) labels.Selector {
	selector := labels.NewSelector()
	req, _ := labels.NewRequirement("ceph_daemon_type", selection.Equals, []string{name})
	return selector.Add(*req)
}

func rookOperatorSelector() labels.Selector {
	selector := labels.NewSelector()
	req, _ := labels.NewRequirement("operator", selection.Equals, []string{"rook"})
	return selector.Add(*req)
}

func SuspendedSelector() labels.Selector {
	selector := labels.NewSelector()
	req, _ := labels.NewRequirement(SuspendedKey, selection.Exists, nil)
	return selector.Add(*req)
}

func SuspendedClassSelector(class string) labels.Selector {
	selector := labels.NewSelector()
	req, _ := labels.NewRequirement(SuspendedKey, selection.Exists, nil)
	classReq, _ := labels.NewRequirement(SuspendedClassKey, selection.In, []string{class})
	return selector.Add(*req, *classReq)
}
