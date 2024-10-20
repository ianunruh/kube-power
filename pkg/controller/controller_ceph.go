package controller

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

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

		if !c.cfg.DryRun {
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

	err = exec.StreamWithContext(context.TODO(), remotecommand.StreamOptions{
		Stderr: stderrBuffer,
		Stdout: stdoutBuffer,
	})
	stderr = stderrBuffer.Bytes()
	stdout = stdoutBuffer.Bytes()
	return
}
