package controller

import (
	"slices"
	"strings"
)

var defaultOperatorInclude = []OperatorMatch{
	{Name: "argocd-application-controller"},
	{Name: "operator"},
}

type OperatorConfig struct {
	Include []OperatorMatch
	Exclude []OperatorMatch

	DisableDefaults bool ` yaml:"disableDefaults"`
}

type OperatorMatch struct {
	Name string `yaml:"name"`

	Labels map[string]string `yaml:"labels"`
}

func (m OperatorMatch) Matches(name string, labels map[string]string) bool {
	if m.Name != "" {
		if !strings.Contains(name, m.Name) {
			return false
		}
	}

	if len(m.Labels) > 0 {
		for key, value := range m.Labels {
			if labels[key] != value {
				return false
			}
		}
	}

	return true
}

func (c *Controller) isOperator(name string, labels map[string]string) bool {
	result := false

	// at least one include must match
	for _, include := range c.operatorInclude() {
		if include.Matches(name, labels) {
			result = true
			break
		}
	}

	if !result {
		return false
	}

	// ensure no exclude matches
	for _, exclude := range c.cfg.Operators.Exclude {
		if exclude.Matches(name, labels) {
			return false
		}
	}

	return true
}

func (c *Controller) operatorInclude() []OperatorMatch {
	if c.cfg.Operators.DisableDefaults {
		// only match on user-provided include
		return c.cfg.Operators.Include
	}
	return slices.Concat(c.cfg.Operators.Include, defaultOperatorInclude)
}
