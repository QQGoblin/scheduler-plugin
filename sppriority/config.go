package sppriority

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/klog/v2"
	"os"
)

const (
	defaultConfigPath = "/etc/kubernetes/sppriority.yaml"
	logPrefix         = "[SPScheduler]"
)

type PriorityConfig struct {
	SkipMultiReplica   bool     `yaml:"skipMultiReplica,omitempty"`
	NamespaceWhiteList []string `yaml:"namespaceWhiteList,omitempty"`
	DisableAnnKey      string   `yaml:"disableAnnKey,omitempty"`
	stateAnnKey        string   `yaml:"stateAnnKey,omitempty"`
}

func defaultConfig() *PriorityConfig {

	return &PriorityConfig{
		SkipMultiReplica:   false,
		NamespaceWhiteList: []string{"kube-system"},
		DisableAnnKey:      "annotation.sp.io/disable-priority-scheduler",
		stateAnnKey:        "annotation.sp.io/schedule-state",
	}
}

func loadConfig() (*PriorityConfig, error) {

	c := defaultConfig()

	if _, isNotExistErr := os.Stat(defaultConfigPath); isNotExistErr != nil {
		if os.IsNotExist(isNotExistErr) {
			return c, nil
		}
	}

	data, err := os.ReadFile(defaultConfigPath)
	if err != nil {
		klog.Errorf("%s read config file %s failed: %v", logPrefix, defaultConfigPath, err)
		return nil, err
	}

	if err = yaml.UnmarshalStrict(data, c); err != nil {
		klog.Warningf("% unmarshal config file %s failed: %v", logPrefix, defaultConfigPath, err)
		return nil, err
	}

	return c, nil
}

func (pc *PriorityConfig) disable(pod *v1.Pod) bool {

	for _, n := range pc.NamespaceWhiteList {
		if n == pod.Namespace {
			return true
		}
	}
	_, isDisableAnn := pod.Annotations[pc.DisableAnnKey]
	return isDisableAnn

}
