package controller

import (
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	batchv1listers "k8s.io/client-go/listers/batch/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	restclient "k8s.io/client-go/rest"
)

func NewController(cfg Config, restConfig *restclient.Config, log *zap.Logger) (*Controller, error) {
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	stop := make(chan struct{})

	informerFactory := informers.NewSharedInformerFactory(clientset, 0)

	ctrl := &Controller{
		cfg:        cfg,
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
	cfg        Config
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

func (c *Controller) WaitForCacheSync() {
	c.informerFactory.WaitForCacheSync(c.stop)
}

func namespacedName(namespace, name string) types.NamespacedName {
	return types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
}
