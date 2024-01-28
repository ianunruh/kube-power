package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	batchv1listers "k8s.io/client-go/listers/batch/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

func NewController(dryRun bool, restConfig *restclient.Config, log *zap.Logger) (*Controller, error) {
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	stop := make(chan struct{})

	informerFactory := informers.NewSharedInformerFactory(clientset, 0)

	ctrl := &Controller{
		dryRun:     dryRun,
		restConfig: restConfig,
		log:        log,

		clientset:       clientset,
		informerFactory: informerFactory,

		cronJobLister: informerFactory.Batch().V1().CronJobs().Lister(),
		deployLister:  informerFactory.Apps().V1().Deployments().Lister(),
		podLister:     informerFactory.Core().V1().Pods().Lister(),
		pvcLister:     informerFactory.Core().V1().PersistentVolumeClaims().Lister(),
		pvLister:      informerFactory.Core().V1().PersistentVolumes().Lister(),
		stsLister:     informerFactory.Apps().V1().StatefulSets().Lister(),

		stop: stop,
	}

	informerFactory.Start(stop)

	return ctrl, nil
}

type Controller struct {
	dryRun     bool
	restConfig *restclient.Config
	log        *zap.Logger

	clientset       *kubernetes.Clientset
	informerFactory informers.SharedInformerFactory

	cronJobLister batchv1listers.CronJobLister
	deployLister  appsv1listers.DeploymentLister
	podLister     corev1listers.PodLister
	pvcLister     corev1listers.PersistentVolumeClaimLister
	pvLister      corev1listers.PersistentVolumeLister
	stsLister     appsv1listers.StatefulSetLister

	stop chan struct{}
}

func (c *Controller) Close() {
	close(c.stop)
}

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

func (c *Controller) ResumeCluster() error {
	if err := c.ResumeCephDaemons(); err != nil {
		return err
	}

	if err := c.UpdateCephFlags(false); err != nil {
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

func (c *Controller) SuspendOperators() error {
	deployments, err := c.deployLister.List(labels.Everything())
	if err != nil {
		return err
	}

	for _, deploy := range deployments {
		if !isNonCriticalOperator(deploy.Name) {
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
		if !isNonCriticalOperator(sts.Name) {
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

func (c *Controller) ResumeOperators() error {
	if err := c.ResumeDeploys(SuspendedClassSelector(OperatorClass)); err != nil {
		return err
	}

	if err := c.ResumeStatefulSets(SuspendedClassSelector(OperatorClass)); err != nil {
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
				if isNonCriticalOperator(deploy.Name) {
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

func (c *Controller) ResumeCephConsumers() error {
	if err := c.ResumeStatefulSets(SuspendedClassSelector(CephConsumerClass)); err != nil {
		return nil
	}

	if err := c.ResumeDeploys(SuspendedClassSelector(CephConsumerClass)); err != nil {
		return nil
	}

	return nil
}

func (c *Controller) UpdateCephFlags(ensure bool) error {
	pod, err := c.firstRunningPod(appSelector("rook-ceph-tools"))
	if err != nil {
		return err
	}

	// https://docs.mirantis.com/mcp/q4-18/mcp-operations-guide/scheduled-maintenance-power-outage/power-off-ceph-cluster.html
	osdFlags := []string{
		"noout",
		"nobackfill",
		"norecover",
		"norebalance",
		"nodown",
		"pause",
	}

	action := "set"

	if !ensure {
		slices.Reverse(osdFlags)
		action = "unset"
	}

	for _, osdFlag := range osdFlags {
		args := []string{"ceph", "osd", action, osdFlag}
		command := strings.Join(args, " ")

		c.log.Info("Executing Ceph command",
			zap.String("namespace", pod.Namespace),
			zap.String("name", pod.Name),
			zap.String("command", command))

		if !c.dryRun {
			stdout, stderr, err := c.execPodCommand(pod, command)
			c.log.Debug("Executed Ceph command",
				zap.String("namespace", pod.Namespace),
				zap.String("name", pod.Name),
				zap.String("command", command),
				zap.ByteString("stdout", stdout),
				zap.ByteString("stderr", stderr))
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Controller) firstRunningPod(selector labels.Selector) (*corev1.Pod, error) {
	pods, err := c.podLister.List(selector)
	if err != nil {
		return nil, err
	}

	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodRunning {
			return pod, nil
		}
	}

	return nil, errors.New("no running pods found")
}

func (c *Controller) execPodCommand(pod *corev1.Pod, command string) (stdout, stderr []byte, err error) {
	params := &corev1.PodExecOptions{
		Command: []string{"/bin/sh", "-c", command},
		Stderr:  true,
		Stdout:  true,
	}

	request := c.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		SubResource("exec").
		Namespace(pod.Namespace).
		Name(pod.Name).
		VersionedParams(params, scheme.ParameterCodec)

	var exec remotecommand.Executor
	exec, err = remotecommand.NewSPDYExecutor(c.restConfig, "POST", request.URL())
	if err != nil {
		return
	}

	stderrBuffer := &bytes.Buffer{}
	stdoutBuffer := &bytes.Buffer{}

	err = exec.Stream(remotecommand.StreamOptions{
		Stderr: stderrBuffer,
		Stdout: stdoutBuffer,
	})
	stderr = stderrBuffer.Bytes()
	stdout = stdoutBuffer.Bytes()
	return
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

func (c *Controller) ResumeCephDaemons() error {
	selectors := []labels.Selector{
		rookDaemonSelector("mon"),
		rookDaemonSelector("osd"),
		rookDaemonSelector("mds"),
		rookDaemonSelector("rgw"),
		rookOperatorSelector(),
	}

	for _, selector := range selectors {
		if err := c.ResumeDeploys(selector); err != nil {
			return err
		}
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

func (c *Controller) WaitForCacheSync() {
	c.informerFactory.WaitForCacheSync(c.stop)
}

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

func namespacedName(namespace, name string) types.NamespacedName {
	return types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
}

func isNonCriticalOperator(name string) bool {
	// TODO some of this could be configurable
	if name == "rook-ceph-operator" {
		return false
	} else if name == "argocd-application-controller" {
		return true
	}
	return strings.Contains(name, "operator")
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
