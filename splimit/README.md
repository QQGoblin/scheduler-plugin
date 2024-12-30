# 自定义插件：NodeLimitResource

## 1. 功能

基于`Pod`的`Limit Resources`配置，实现节点过滤以及打分，其效果类树内插件`NodeResourcesFit`的功能。

## 2. 插件实现

### 1. 实现接口

该插件包括以下两个部分：

## 2. 插件实现

### 1. 实现接口

基于`Limit Resources` 均衡节点负载:

```golang
type PreScorePlugin interface {
    Plugin
    PreScore(ctx context.Context, state *CycleState, pod *v1.Pod, nodes []*v1.Node) *Status
}
type ScorePlugin interface {
    Plugin
    Score(ctx context.Context, state *CycleState, p *v1.Pod, nodeName string) (int64, *Status)
    ScoreExtensions() ScoreExtensions
}
type ScoreExtensions interface {
    NormalizeScore(ctx context.Context, state *CycleState, p *v1.Pod, scores NodeScoreList) *Status
}
```

## 3. 配置文件

```yaml
apiVersion: kubescheduler.config.k8s.io/v1beta3
kind: KubeSchedulerConfiguration
clientConnection:
  kubeconfig: /etc/kubernetes/kube-scheduler.conf
profiles:
  - schedulerName: default-scheduler
    plugins:
      multiPoint:
        enabled:
          - name: FitLimitResources
            weight: 1
      score:
        disabled:
          - name: NodeResourcesFit
          - name: NodeResourcesBalancedAllocation
          - name: PodTopologySpread
```

## 4. 注意事项

