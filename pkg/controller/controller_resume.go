package controller

import (
	"context"
	"time"

	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func (c *Controller) ResumeCluster() error {
	if err := c.ResumeCephDaemons(); err != nil {
		return err
	}

	if err := c.UpdateCephFlags(false); err != nil {
		return err
	}

	if err := c.ResumeCephRGW(); err != nil {
		return err
	}

	if err := c.ResumeCephOperator(); err != nil {
		return err
	}

	if err := c.WaitForCephHealthOK(); err != nil {
		return err
	}

	if err := c.ResumeCephConsumers(); err != nil {
		return err
	}

	if err := c.ResumeCronJobs(); err != nil {
		return err
	}

	if err := c.ResumeOperators(); err != nil {
		return err
	}

	return nil
}

func (c *Controller) ResumeCephConsumers() error {
	if err := c.ResumeStatefulSets(SuspendedClassSelector(CephConsumerClass)); err != nil {
		return nil
	}

	if err := c.ResumeDeploys(SuspendedClassSelector(CephConsumerClass)); err != nil {
		return nil
	}

	return nil
}

func (c *Controller) ResumeCephDaemons() error {
	selectors := []labels.Selector{
		rookDaemonSelector("mon"),
		rookDaemonSelector("osd"),
		rookDaemonSelector("mds"),
	}

	for _, selector := range selectors {
		if err := c.ResumeDeploys(selector); err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) ResumeCephRGW() error {
	return c.ResumeDeploys(rookDaemonSelector("rgw"))
}

func (c *Controller) ResumeCephOperator() error {
	return c.ResumeDeploys(rookOperatorSelector())
}

func (c *Controller) ResumeOperators() error {
	if err := c.ResumeDeploys(SuspendedClassSelector(OperatorClass)); err != nil {
		return err
	}

	if err := c.ResumeStatefulSets(SuspendedClassSelector(OperatorClass)); err != nil {
		return err
	}

	return nil
}

func (c *Controller) ResumeDeploys(selector labels.Selector) error {
	deploys, err := c.deployLister.List(selector)
	if err != nil {
		return err
	}

	for _, deploy := range deploys {
		if err := c.resumeDeploy(deploy); err != nil {
			return err
		}
	}

	if err := c.waitForDeploysToResume(selector); err != nil {
		return err
	}

	return nil
}

func (c *Controller) resumeDeploy(deploy *appsv1.Deployment) error {
	c.log.Info("Resuming Deployment",
		zap.String("namespace", deploy.Namespace),
		zap.String("name", deploy.Name))

	if _, ok := deploy.Annotations[SuspendedKey]; !ok {
		return nil
	}

	state, err := decodeSuspendedState(deploy.Annotations[SuspendedKey])
	if err != nil {
		return err
	}

	deploy = deploy.DeepCopy()
	delete(deploy.Annotations, SuspendedKey)
	delete(deploy.Labels, SuspendedKey)
	delete(deploy.Labels, SuspendedClassKey)
	deploy.Spec.Replicas = state.Replicas

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

func (c *Controller) waitForDeploysToResume(selector labels.Selector) error {
	c.WaitForCacheSync()

	for {
		deployments, err := c.deployLister.List(selector)
		if err != nil {
			return err
		}

		wait := false
		for _, deploy := range deployments {
			if deploy.Status.Replicas == 0 || deploy.Status.ReadyReplicas < deploy.Status.Replicas {
				c.log.Debug("Waiting on Deployment to resume",
					zap.String("namespace", deploy.Namespace),
					zap.String("name", deploy.Name))
				wait = true
			}
		}

		if !wait {
			c.log.Info("All Deployments are resumed",
				zap.Stringer("selector", selector))
			return nil
		}

		// wait for controllers to work
		// TODO max timeout
		time.Sleep(10 * time.Second)
	}
}

func (c *Controller) ResumeStatefulSets(selector labels.Selector) error {
	statefulSets, err := c.stsLister.List(selector)
	if err != nil {
		return err
	}

	for _, sts := range statefulSets {
		if err := c.resumeStatefulSet(sts); err != nil {
			return err
		}
	}

	if err := c.waitForStatefulSetsToResume(selector); err != nil {
		return err
	}

	return nil
}

func (c *Controller) ResumeCronJobs() error {
	cronJobs, err := c.cronJobLister.List(SuspendedSelector())
	if err != nil {
		return err
	}

	for _, cronJob := range cronJobs {
		if err := c.resumeCronJob(cronJob); err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) resumeCronJob(cronJob *batchv1.CronJob) error {
	c.log.Info("Resuming CronJob",
		zap.String("namespace", cronJob.Namespace),
		zap.String("name", cronJob.Name))

	if _, ok := cronJob.Annotations[SuspendedKey]; !ok {
		return nil
	}

	state, err := decodeSuspendedState(cronJob.Annotations[SuspendedKey])
	if err != nil {
		return err
	}

	cronJob = cronJob.DeepCopy()
	delete(cronJob.Annotations, SuspendedKey)
	delete(cronJob.Labels, SuspendedKey)
	cronJob.Spec.Suspend = state.Suspend

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

func (c *Controller) resumeStatefulSet(sts *appsv1.StatefulSet) error {
	c.log.Info("Resuming StatefulSet",
		zap.String("namespace", sts.Namespace),
		zap.String("name", sts.Name))

	if _, ok := sts.Annotations[SuspendedKey]; !ok {
		return nil
	}

	state, err := decodeSuspendedState(sts.Annotations[SuspendedKey])
	if err != nil {
		return err
	}

	sts = sts.DeepCopy()
	delete(sts.Annotations, SuspendedKey)
	delete(sts.Labels, SuspendedKey)
	delete(sts.Labels, SuspendedClassKey)
	sts.Spec.Replicas = state.Replicas

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

func (c *Controller) waitForStatefulSetsToResume(selector labels.Selector) error {
	c.WaitForCacheSync()

	for {
		statefulSets, err := c.stsLister.List(selector)
		if err != nil {
			return err
		}

		wait := false
		for _, sts := range statefulSets {
			if sts.Status.Replicas == 0 || sts.Status.ReadyReplicas < sts.Status.Replicas {
				c.log.Debug("Waiting on StatefulSet to resume",
					zap.String("namespace", sts.Namespace),
					zap.String("name", sts.Name))
				wait = true
			}
		}

		if !wait {
			c.log.Info("All StatefulSets are resumed",
				zap.Stringer("selector", selector))
			return nil
		}

		// wait for controllers to work
		time.Sleep(10 * time.Second)
	}
}
