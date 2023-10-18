package sppriority

import (
	"context"
	"encoding/json"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

type SPPriority struct {
	sharedLister framework.SharedLister
	handle       framework.Handle
	priConfig    *PriorityConfig
}

var _ framework.ScorePlugin = &SPPriority{}
var _ framework.PostBindPlugin = &SPPriority{}

const (
	Name = "SPPriority"
)

var (
	rsKind = appsv1.SchemeGroupVersion.WithKind("ReplicaSet")
	ssKind = appsv1.SchemeGroupVersion.WithKind("StatefulSet")
)

// New initializes a new plugin and returns it.
func New(_ runtime.Object, handle framework.Handle) (framework.Plugin, error) {

	sharedLister := handle.SnapshotSharedLister()
	if sharedLister == nil {
		return nil, fmt.Errorf("SnapshotSharedLister is nil")
	}
	sharedInformerFactory := handle.SharedInformerFactory()
	if sharedInformerFactory == nil {
		return nil, fmt.Errorf("SharedInformerFactory is nil")
	}

	config, err := loadConfig()
	if err != nil {
		return nil, err
	}

	return &SPPriority{
		priConfig:    config,
		sharedLister: sharedLister,
		handle:       handle,
	}, nil
}

func (p *SPPriority) Name() string {
	return Name
}

func (p *SPPriority) Score(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) (int64, *framework.Status) {

	return 0, nil
}

func (p *SPPriority) ScoreExtensions() framework.ScoreExtensions {
	return p
}

func (p *SPPriority) NormalizeScore(ctx context.Context, state *framework.CycleState, pod *v1.Pod, scores framework.NodeScoreList) *framework.Status {
	if p.priConfig.disable(pod) {
		return nil
	}

	ownerObj, err := p.ownerReplicasets(ctx, pod)
	if err != nil {
		klog.Errorf("%s get pod<%s/%s> owner failed: %v", logPrefix, pod.Namespace, pod.Name, err)
		return nil
	}

	schState, err := p.getScheduleState(ownerObj)
	if err != nil {
		klog.Errorf("%s get scheduler count from owner obj failed: %v", logPrefix, err)
		return nil
	}

	total := schState.totalExceptLast()

	klog.V(5).InfoS(fmt.Sprintf("%s schedule state", logPrefix), "last", schState.Last, "total", total, "name", pod.Name, "namespace", pod.Namespace)

	for i := range scores {
		if scores[i].Name == schState.Last {
			scores[i].Score = framework.MinNodeScore
			continue
		}

		if total == 0 {
			scores[i].Score = framework.MaxNodeScore
			continue
		}

		count := schState.getScheduleCount(scores[i].Name)
		scores[i].Score = int64(float64(framework.MaxNodeScore) * (1 - float64(count)/float64(total)))
	}
	return nil
}

func (p *SPPriority) PostBind(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) {

	if p.priConfig.disable(pod) {
		klog.Infof("%s disable prority scheduler this pod<%s/%s>", logPrefix, pod.Namespace, pod.Name)
		return
	}

	ownerObj, err := p.ownerReplicasets(ctx, pod)
	if err != nil {
		klog.Errorf("%s get pod<%s/%s> owner failed: %v", logPrefix, pod.Namespace, pod.Name, err)
		return
	}

	if p.priConfig.SkipMultiReplica && *ownerObj.Spec.Replicas > 1 {
		klog.Infof("%s skip replica more one", logPrefix)
		return
	}

	schState, err := p.getScheduleState(ownerObj)
	if err != nil {
		klog.Errorf("%s get scheduler count from owner obj failed: %v", logPrefix, err)
		return
	}

	p.updateScheduleState(schState, nodeName)

	if err = p.updateOwnerReplicasets(ownerObj, schState); err != nil {
		klog.Errorf("%s update owner obj failed: %v", logPrefix, err)
		return
	}

}

func (p *SPPriority) ownerReplicasets(ctx context.Context, pod *v1.Pod) (*appsv1.ReplicaSet, error) {
	owner := metav1.GetControllerOfNoCopy(pod)
	if owner == nil {
		return nil, fmt.Errorf("owner is empty")
	}

	gv, err := schema.ParseGroupVersion(owner.APIVersion)
	if err != nil {
		return nil, fmt.Errorf("owner with error groupversion")
	}

	gvk := gv.WithKind(owner.Kind)

	if gvk != rsKind {
		return nil, fmt.Errorf("owner is not Replicasets")
	}
	return p.handle.ClientSet().AppsV1().ReplicaSets(pod.Namespace).Get(ctx, owner.Name, metav1.GetOptions{})

}

func (p *SPPriority) getScheduleState(rs *appsv1.ReplicaSet) (*scheduleState, error) {

	stateStr, isOK := rs.Annotations[p.priConfig.StateAnnKey]
	if !isOK {
		return &scheduleState{
			NodeCount: make(map[string]int),
		}, nil
	}

	state := &scheduleState{
		NodeCount: make(map[string]int),
	}

	if err := json.Unmarshal([]byte(stateStr), state); err != nil {
		return nil, err
	}

	return state, nil

}

func (p *SPPriority) updateScheduleState(state *scheduleState, nodename string) {

	if state.NodeCount == nil {
		state.NodeCount = make(map[string]int)
	}

	count, isOK := state.NodeCount[nodename]
	if isOK {
		state.NodeCount[nodename] = count + 1
	} else {
		state.NodeCount[nodename] = 1
	}

	state.Last = nodename

}

func (p *SPPriority) updateOwnerReplicasets(rs *appsv1.ReplicaSet, state *scheduleState) error {

	message, err := json.Marshal(state)
	if err != nil {
		return err
	}

	if rs.Annotations == nil {
		rs.Annotations = make(map[string]string)
	}
	rs.Annotations[p.priConfig.StateAnnKey] = string(message)

	patch := map[string]interface{}{"metadata": map[string]interface{}{"annotations": rs.Annotations}}

	patchBytes, _ := json.Marshal(patch)
	if _, err = p.handle.ClientSet().AppsV1().ReplicaSets(rs.Namespace).Patch(
		context.Background(), rs.Name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{},
	); err != nil {
		return err
	}

	return nil
}
