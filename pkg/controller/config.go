package controller

import (
	"errors"
	"path/filepath"

	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func LoadKubeConfig(path string) (*restclient.Config, error) {
	if path == "" {
		if home := homedir.HomeDir(); home != "" {
			path = filepath.Join(home, ".kube", "config")
		} else {
			return nil, errors.New("cannot guess kube config path")
		}
	}

	return clientcmd.BuildConfigFromFlags("", path)
}
