package controller

import (
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

type Config struct {
	DryRun bool `yaml:"dryRun"`

	Operators OperatorConfig `yaml:"operators"`
}

func LoadConfig(path string) (Config, error) {
	var cfg Config

	if path == "" {
		return cfg, nil
	}

	encoded, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}

	if err := yaml.Unmarshal(encoded, &cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}

func LoadKubeConfig(path string) (*restclient.Config, error) {
	cfg, err := restclient.InClusterConfig()
	if err != nil {
		if !errors.Is(err, restclient.ErrNotInCluster) {
			return nil, err
		}
	} else {
		return cfg, nil
	}

	if path == "" {
		if home := homedir.HomeDir(); home != "" {
			path = filepath.Join(home, ".kube", "config")
		} else {
			return nil, errors.New("cannot guess kube config path")
		}
	}

	return clientcmd.BuildConfigFromFlags("", path)
}
