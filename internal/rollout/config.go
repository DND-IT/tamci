// Package rollout parses matrix.config.yaml and resolves per-service environment lists.
// Merge precedence (lowest → highest): global < environment < service.
package rollout

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// MatrixConfig is the top-level structure of matrix.config.yaml.
type MatrixConfig struct {
	Global      GlobalConfig                 `yaml:"global"`
	Environment map[string]EnvironmentConfig `yaml:"environment"`
	Service     map[string]ServiceConfig     `yaml:"service"`
}

type GlobalConfig struct {
	ChartsDir      string `yaml:"charts_dir"`
	AWSIAMRoleName string `yaml:"aws_iam_role_name"`
	AWSRegion      string `yaml:"aws_region"`
}

type EnvironmentConfig struct {
	AWSAccountID string `yaml:"aws_account_id"`
	Deploy       string `yaml:"deploy"`       // "auto" | "pr"
	Tag          string `yaml:"tag"`          // "version" | "sha"
	ValuesMode   string `yaml:"values_mode"`  // "image" (default) | "key" | "marker"
	ValuesKey    string `yaml:"values_key"`   // dot-path for key mode
	AutoMerge    bool   `yaml:"auto_merge"`   // enable auto-merge on deploy PRs
	MergeMethod  string `yaml:"merge_method"` // MERGE | SQUASH | REBASE
}

type ServiceConfig struct {
	Environments    []string `yaml:"environments"` // subset of environment keys; empty means all
	ECRRepository   string   `yaml:"ecr_repository"`
	Release         string   `yaml:"release"` // "auto" | "gated"
	Deploy          string   `yaml:"deploy"`  // overrides environment-level
	Tag             string   `yaml:"tag"`     // overrides environment-level
	TagPrefix       string   `yaml:"tag_prefix"`
	VersionStrategy string   `yaml:"version_strategy"`
	ValuesMode      string   `yaml:"values_mode"`
	ValuesKey       string   `yaml:"values_key"`
	AutoMerge       *bool    `yaml:"auto_merge"`
	MergeMethod     string   `yaml:"merge_method"`
}

// Environment is a fully-resolved environment entry for a specific service.
type Environment struct {
	Name         string
	Deploy       string
	Tag          string
	AWSAccountID string
	AWSRegion    string
	ValuesMode   string
	ValuesKey    string
	AutoMerge    bool
	MergeMethod  string
}

// LoadConfig reads and parses the matrix config file at path.
func LoadConfig(path string) (*MatrixConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	var cfg MatrixConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	return &cfg, nil
}

// Resolve returns the list of environments for the given service, applying
// merge precedence (global < environment < service). Sorted by name.
func (c *MatrixConfig) Resolve(service string) ([]Environment, error) {
	svcCfg, ok := c.Service[service]
	if !ok {
		return nil, fmt.Errorf("service %q not found in config", service)
	}

	selected := c.Environment
	if len(svcCfg.Environments) > 0 {
		selected = make(map[string]EnvironmentConfig, len(svcCfg.Environments))
		for _, name := range svcCfg.Environments {
			envCfg, ok := c.Environment[name]
			if !ok {
				return nil, fmt.Errorf("service %q lists environment %q, which is not defined", service, name)
			}
			selected[name] = envCfg
		}
	}

	envs := make([]Environment, 0, len(selected))
	for envName, envCfg := range selected {
		e := Environment{
			Name:         envName,
			Deploy:       envCfg.Deploy,
			Tag:          envCfg.Tag,
			AWSAccountID: envCfg.AWSAccountID,
			AWSRegion:    c.Global.AWSRegion,
			ValuesMode:   envCfg.ValuesMode,
			ValuesKey:    envCfg.ValuesKey,
			AutoMerge:    envCfg.AutoMerge,
			MergeMethod:  envCfg.MergeMethod,
		}
		if svcCfg.Deploy != "" {
			e.Deploy = svcCfg.Deploy
		}
		if svcCfg.Tag != "" {
			e.Tag = svcCfg.Tag
		}
		if svcCfg.ValuesMode != "" {
			e.ValuesMode = svcCfg.ValuesMode
		}
		if svcCfg.ValuesKey != "" {
			e.ValuesKey = svcCfg.ValuesKey
		}
		if svcCfg.AutoMerge != nil {
			e.AutoMerge = *svcCfg.AutoMerge
		}
		if svcCfg.MergeMethod != "" {
			e.MergeMethod = svcCfg.MergeMethod
		}

		if e.ValuesMode == "" {
			e.ValuesMode = "image"
		}
		if e.MergeMethod == "" {
			e.MergeMethod = "SQUASH"
		}
		envs = append(envs, e)
	}

	sort.Slice(envs, func(i, j int) bool { return envs[i].Name < envs[j].Name })
	return envs, nil
}
