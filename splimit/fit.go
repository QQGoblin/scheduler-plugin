package splimit

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	schedutil "k8s.io/kubernetes/pkg/scheduler/util"
)

type FitLimitResources struct {
	sharedLister framework.SharedLister
	handle       framework.Handle
}

var _ framework.ScorePlugin = &FitLimitResources{}

const (
	Name = "FitLimitResources"
)

var (
	// TODO: 动态获取权重值
	resourceToWeightMap = map[v1.ResourceName]int64{
		v1.ResourceCPU:    1,
		v1.ResourceMemory: 1,
	}
)

func (f *FitLimitResources) Name() string {
	return Name
}

func New(_ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	sharedLister := handle.SnapshotSharedLister()
	if sharedLister == nil {
		return nil, fmt.Errorf("SnapshotSharedLister is nil")
	}
	sharedInformerFactory := handle.SharedInformerFactory()
	if sharedInformerFactory == nil {
		return nil, fmt.Errorf("SharedInformerFactory is nil")
	}

	return &FitLimitResources{
		sharedLister: sharedLister,
		handle:       handle,
	}, nil
}

func (f *FitLimitResources) Score(_ context.Context, _ *framework.CycleState, pod *v1.Pod, nodeName string) (int64, *framework.Status) {
	nodeInfo, err := f.handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
	if err != nil {
		return 0, framework.AsStatus(fmt.Errorf("getting node %q from Snapshot: %w", nodeName, err))
	}

	node := nodeInfo.Node()
	if node == nil {
		return 0, framework.NewStatus(framework.Error, "node not found")
	}

	nodeLimit, podLimit := f.calculateNodeResourceLimit(nodeInfo, pod)

	allocatable := map[v1.ResourceName]int64{
		v1.ResourceCPU:    nodeInfo.Allocatable.MilliCPU,
		v1.ResourceMemory: nodeInfo.Allocatable.Memory,
	}

	limit := make(map[v1.ResourceName]int64)

	for resource := range resourceToWeightMap {
		if allocatable[resource] != 0 {
			limit[resource] = nodeLimit[resource] + podLimit[resource]
		}
	}

	return f.score(limit, allocatable), nil

}

func (f *FitLimitResources) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

func (f *FitLimitResources) calculateNodeResourceLimit(nodeInfo *framework.NodeInfo, pod *v1.Pod) (map[v1.ResourceName]int64, map[v1.ResourceName]int64) {

	//  计算当前节点的 limit 总量，规则如下：
	//  1. 不考虑 rumtimeClass 注入的 Overhead 配置
	//  2. 只统计 CPU 和 Memory 其他一律返回 0
	//  3. 不考虑 InitContainer

	var (
		nodeLimit = make(map[v1.ResourceName]int64)
		podLimit  = make(map[v1.ResourceName]int64)
	)

	for _, podInfo := range nodeInfo.Pods {
		for _, container := range podInfo.Pod.Spec.Containers {
			nodeLimit[v1.ResourceCPU] += schedutil.GetRequestForResource(v1.ResourceCPU, &container.Resources.Limits, false)
			nodeLimit[v1.ResourceMemory] += schedutil.GetRequestForResource(v1.ResourceMemory, &container.Resources.Limits, false)
		}
	}

	for _, container := range pod.Spec.Containers {
		nodeLimit[v1.ResourceCPU] += schedutil.GetRequestForResource(v1.ResourceCPU, &container.Resources.Limits, false)
		nodeLimit[v1.ResourceMemory] += schedutil.GetRequestForResource(v1.ResourceMemory, &container.Resources.Limits, false)

	}

	if klog.V(9).Enabled() {
		klog.InfoS("Limit resource for node",
			"nodename", nodeInfo.Node().Name,
			v1.ResourceCPU, nodeLimit[v1.ResourceCPU],
			v1.ResourceMemory, nodeLimit[v1.ResourceMemory],
		)
	}

	if klog.V(9).Enabled() {
		klog.InfoS("Limit resource for pod",
			"podname", pod.Name,
			v1.ResourceCPU, nodeLimit[v1.ResourceCPU],
			v1.ResourceMemory, nodeLimit[v1.ResourceMemory],
		)
	}

	return nodeLimit, podLimit
}

func (f *FitLimitResources) score(limit, capacity map[v1.ResourceName]int64) int64 {

	var nodeScore, weightSum int64
	for resourceName, weight := range resourceToWeightMap {
		resourceScore := leastRequestedScore(limit[resourceName], capacity[resourceName])
		nodeScore += resourceScore * weight
		weightSum += weight
	}

	return nodeScore / weightSum
}

func leastRequestedScore(limited, capacity int64) int64 {
	if capacity == 0 {
		return 0
	}
	if limited > capacity {
		return 0
	}

	return ((capacity - limited) * int64(framework.MaxNodeScore)) / capacity
}
