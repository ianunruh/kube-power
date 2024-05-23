package controller

import (
	"context"
	"time"

	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

func (c *Controller) SuspendCluster() error {
	if err := c.SuspendOperators(); err != nil {
		return err
	}

	if err := c.SuspendCronJobs(); err != nil {
		return err
	}

	if err := c.SuspendCephConsumers(); err != nil {
		return err
	}

	if err := c.UpdateCephFlags(true); err != nil {
		return err
	}

	if err := c.SuspendCephDaemons(); err != nil {
		return err
	}

	return nil
}

func (c *Controller) SuspendCephConsumers() error {
	volumes, err := c.pvLister.List(labels.Everything())
	if err != nil {
		return err
	}

	deployments, err := c.deployLister.List(labels.Everything())
	if err != nil {
		return err
	}

	statefulSets, err := c.stsLister.List(labels.Everything())
	if err != nil {
		return err
	}

	var (
		deploysInScope = make(map[types.NamespacedName]bool)
		stsInScope     = make(map[types.NamespacedName]bool)
	)

	for _, vol := range volumes {
		claimRef := vol.Spec.ClaimRef
		if claimRef == nil || vol.Spec.CSI == nil || !filterCSIDriver(vol.Spec.CSI.Driver) {
			continue
		}

		claim, err := c.pvcLister.PersistentVolumeClaims(claimRef.Namespace).Get(claimRef.Name)
		if err != nil {
			return err
		}

		// look for anything referencing this claim, except anything
		// that looks like an operator, as those will be suspended first
		for _, deploy := range deployments {
			if deploymentHasClaim(deploy, claim) {
				if isOperator(deploy.Name) {
					continue
				}
				deploysInScope[namespacedName(deploy.Namespace, deploy.Name)] = true
			}
		}
		for _, sts := range statefulSets {
			if statefulSetHasClaim(sts, claim) {
				stsInScope[namespacedName(sts.Namespace, sts.Name)] = true
			}
		}
	}

	for ref := range deploysInScope {
		deploy, err := c.deployLister.Deployments(ref.Namespace).Get(ref.Name)
		if err != nil {
			return err
		}

		if err := c.suspendDeploy(deploy, CephConsumerClass); err != nil {
			return err
		}
	}

	if err := c.waitForDeploysToSuspend(SuspendedClassSelector(CephConsumerClass)); err != nil {
		return err
	}

	for ref := range stsInScope {
		sts, err := c.stsLister.StatefulSets(ref.Namespace).Get(ref.Name)
		if err != nil {
			return err
		}

		if err := c.suspendStatefulSet(sts, CephConsumerClass); err != nil {
			return err
		}
	}

	if err := c.waitForStatefulSetsToSuspend(SuspendedSelector()); err != nil {
		return err
	}

	return nil
}

func (c *Controller) SuspendCephDaemons() error {
	selectors := []labels.Selector{
		rookOperatorSelector(),
		rookDaemonSelector("rgw"),
		rookDaemonSelector("mds"),
		rookDaemonSelector("osd"),
		rookDaemonSelector("mon"),
	}

	for _, selector := range selectors {
		deploys, err := c.deployLister.List(selector)
		if err != nil {
			return err
		}

		for _, deploy := range deploys {
			if err := c.suspendDeploy(deploy, CephDaemonClass); err != nil {
				return err
			}
		}

		if err := c.waitForDeploysToSuspend(selector); err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) SuspendOperators() error {
	deployments, err := c.deployLister.List(labels.Everything())
	if err != nil {
		return err
	}

	for _, deploy := range deployments {
		if !isOperator(deploy.Name) {
			continue
		}

		deploy, err := c.deployLister.Deployments(deploy.Namespace).Get(deploy.Name)
		if err != nil {
			return err
		}

		if err := c.suspendDeploy(deploy, OperatorClass); err != nil {
			return err
		}
	}

	if err := c.waitForDeploysToSuspend(SuspendedClassSelector(OperatorClass)); err != nil {
		return err
	}

	statefulSets, err := c.stsLister.List(labels.Everything())
	if err != nil {
		return err
	}

	for _, sts := range statefulSets {
		if !isOperator(sts.Name) {
			continue
		}

		sts, err := c.stsLister.StatefulSets(sts.Namespace).Get(sts.Name)
		if err != nil {
			return err
		}

		if err := c.suspendStatefulSet(sts, OperatorClass); err != nil {
			return err
		}
	}

	if err := c.waitForStatefulSetsToSuspend(SuspendedClassSelector(OperatorClass)); err != nil {
		return err
	}

	return nil
}

func (c *Controller) suspendDeploy(deploy *appsv1.Deployment, class string) error {
	c.log.Info("Suspending Deployment",
		zap.String("namespace", deploy.Namespace),
		zap.String("name", deploy.Name))

	if _, ok := deploy.Annotations[SuspendedKey]; ok {
		return nil
	}

	var desiredReplicas int32 = 0

	state := suspendedState{
		Replicas: deploy.Spec.Replicas,
	}

	deploy = deploy.DeepCopy()
	updateMetadata(deploy, state, class)
	deploy.Spec.Replicas = &desiredReplicas

	if !c.dryRun {
		_, err := c.clientset.AppsV1().
			Deployments(deploy.Namespace).
			Update(context.Background(), deploy, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) waitForDeploysToSuspend(selector labels.Selector) error {
	c.WaitForCacheSync()

	for {
		deployments, err := c.deployLister.List(selector)
		if err != nil {
			return err
		}

		wait := false
		for _, deploy := range deployments {
			if deploy.Status.ReadyReplicas > 0 || deploy.Status.Replicas > 0 {
				c.log.Debug("Waiting on Deployment to suspend",
					zap.String("namespace", deploy.Namespace),
					zap.String("name", deploy.Name))
				wait = true
			}
		}

		if !wait {
			c.log.Info("All Deployments are suspended",
				zap.Stringer("selector", selector))
			return nil
		}

		// wait for controllers to work
		time.Sleep(10 * time.Second)
	}
}

func (c *Controller) SuspendCronJobs() error {
	cronJobs, err := c.cronJobLister.List(labels.Everything())
	if err != nil {
		return err
	}

	for _, cronJob := range cronJobs {
		if err := c.suspendCronJob(cronJob); err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) suspendCronJob(cronJob *batchv1.CronJob) error {
	c.log.Info("Suspending CronJob",
		zap.String("namespace", cronJob.Namespace),
		zap.String("name", cronJob.Name))

	if _, ok := cronJob.Annotations[SuspendedKey]; ok {
		return nil
	}

	desiredSuspend := true

	state := suspendedState{
		Suspend: cronJob.Spec.Suspend,
	}

	cronJob = cronJob.DeepCopy()
	updateMetadata(cronJob, state, CronJobClass)
	cronJob.Spec.Suspend = &desiredSuspend

	if !c.dryRun {
		_, err := c.clientset.BatchV1().
			CronJobs(cronJob.Namespace).
			Update(context.Background(), cronJob, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) suspendStatefulSet(sts *appsv1.StatefulSet, class string) error {
	c.log.Info("Suspending StatefulSet",
		zap.String("namespace", sts.Namespace),
		zap.String("name", sts.Name))

	// TODO warn if PVC retain policy is Delete?

	if _, ok := sts.Annotations[SuspendedKey]; ok {
		return nil
	}

	var desiredReplicas int32 = 0

	state := suspendedState{
		Replicas: sts.Spec.Replicas,
	}

	sts = sts.DeepCopy()
	updateMetadata(sts, state, class)
	sts.Spec.Replicas = &desiredReplicas

	if !c.dryRun {
		_, err := c.clientset.AppsV1().
			StatefulSets(sts.Namespace).
			Update(context.Background(), sts, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) waitForStatefulSetsToSuspend(selector labels.Selector) error {
	c.WaitForCacheSync()

	for {
		statefulSets, err := c.stsLister.List(selector)
		if err != nil {
			return err
		}

		wait := false
		for _, sts := range statefulSets {
			if sts.Status.ReadyReplicas > 0 || sts.Status.Replicas > 0 {
				c.log.Debug("Waiting on StatefulSet to suspend",
					zap.String("namespace", sts.Namespace),
					zap.String("name", sts.Name))
				wait = true
			}
		}

		if !wait {
			c.log.Info("All StatefulSets are suspended",
				zap.Stringer("selector", selector))
			return nil
		}

		// wait for controllers to work
		// TODO max timeout
		time.Sleep(10 * time.Second)
	}
}

func updateMetadata(object metav1.Object, state suspendedState, class string) {
	annotations := object.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
		object.SetAnnotations(annotations)
	}

	annotations[SuspendedKey] = encodeSuspendedState(state)

	labels := object.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
		object.SetLabels(labels)
	}

	labels[SuspendedKey] = ""

	if len(class) > 0 {
		labels[SuspendedClassKey] = class
	}
}
