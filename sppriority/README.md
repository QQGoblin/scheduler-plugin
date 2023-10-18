# 自定义插件：优先级调度

## 1. 功能

优先级调度基于策略：在不重复调度的前提下，将 `Pod` 尽可能调度到新的节点上。

假设 Kubernetes 集群中存在 `A`、`B`、`C` 节点，用户创建的单副本 `Deployment` 目前运行在 `B` 节点，且`Pod`曾经被调度到各个节点的次数为 `A(5)`、`B(1)`、`C(3)`。此时如果触发副本重新调度，`Pod` 调度到各个节点的优先级为 `A(3)` > `A(5)` > `B(1)`。

## 2. 插件实现

### 1. 实现接口

该插件包括以下两个部分：
## 2. 插件实现

### 1. 实现接口

该插件包括以下两个部分：

* `PostBindPlugin`：用于记录 `kube-scheduler`每次调度结果，这些信息将作为后续调度依据。

```golang
type PostBindPlugin interface {
	Plugin
	PostBind(ctx context.Context, state *CycleState, p *v1.Pod, nodeName string)
}
```

* `ScorePlugin`：新增的节点打分插件，用于控制 `Pod`最终的调度结果。

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

### 2. `PostBindPlugin`

`PostBindPlugin` 用于记录`Pod`的调度状态到`ownerReferences` （主要是`ReplicaSet`）的`conditions`或者`annotations` 中，其记录内容如下：

```yaml
# message 字符串内容如下： 
# {
#    "latest": "nodename-a",                           // 最近一次调度到的节点
#    "node_count": {                                   // 调度到各个节点次数的统计
#        "nodename-a": 11,
#        "nodename-b": 356,
#        "nodename-c": 6
#    }
# }
```

实际验证后发现，通过`conditions`记录调度状态时，由于`PostBindPlugin` 和 `ReplicaSet Controller` 同时修改 `Obj` 的 `status`，这会导致记录的状态丢失。

### 3. `ScorePlugin`

`ScorePlugin` 根据 `SPScheduleCount` 的内容对每个节点计算得分：

* 上次调度节点（后续称为 LastNode）得分为 0。
* 除 LastNode 外，其他节点的得分为：（1-<调度到该节点次数>/（<调度总次数>-<调度到 LastNode 次数>）* 100

假设`Pod` 的调度记录如下，下一次调度`ScorePlugin`的评分为：`A(0)`、`B(66.66)`、`C(33.33)`

```yaml
{
  "latest": "A",
  "node_count": {
    "A": 11,
    "B": 3,
    "C": 6
  }
}
```

参考 kubernetes 源码，其中包括多个 `ScorePlugin` 插件，为了使自定义逻辑的优先级能够覆盖原生插件优先级，因此将自定义插件的 `Wegith` 设置为 5。

PS：实际实现时，`PreScorePlugin` 和 `ScorePlugin` 实际上可以完全略过，将逻辑全部放置在 `NormalizeScore` 接口中。

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
      - name: SPPriority
        weight: 5
```

考虑到修订`KubeSchedulerConfiguration` 比较繁琐，`SPPriorityPlugin` 将有一个独立的配置文件`/etc/kubernetes/sppriority.yaml`。

```yaml
skipMultiReplica: true  # 禁止多副本时，采用优先级调度
namespaceWhiteList:     # 禁止优先级调度的 namespace
- kube-system
disableAnnKey: annotation.sp.io/disable-priority-scheduler  # 禁止优先级调度的注解
stateAnnKey: "annotation.sp.io/schedule-state"
```

修订`systemd`配置如下：

```
[Unit]
Description=Kubernetes Scheduler
Documentation=https://kubernetes.io/docs/
After=network-online.target etcd.service kube-apiserver.service
Wants=network-online.target

[Service]
EnvironmentFile=-/etc/sysconfig/kube-scheduler.conf
ExecStart=/usr/local/bin/kube-scheduler $KUBE_SCHEDULER_EXTEND \
--config=/etc/kubernetes/kube-scheduler.yaml \
--authentication-kubeconfig=/etc/kubernetes/kube-scheduler.conf \
--authorization-kubeconfig=/etc/kubernetes/kube-scheduler.conf \
--bind-address=0.0.0.0 \
--secure-port=10259 \
--v=2
Restart=always
RestartSec=5
Nice=-15
[Install]
WantedBy=multi-user.target
```

## 4. 注意事项

* 禁用优先级调度策略的方案：`PostBindPlugin`跳过状态记录
* 自定义开关

    * 默认情况下，对所有 `namespace` 的 `Pod` 生效
    * 添加 `namespace_white_list` 参数，这些命明空间下优先级调度策略不会生效。
    * `Pod` 添加注解 `annotation.sp.io/disable-priority-scheduler`时，优先级调度策略不会生效。

* 多副本场景下的策略：

    * 方案一：仅支持单副本`Pod`，通过`ownerReferences` 查询控制器信息，当且对应控制器为`ReplicaSet`，且`spec.replicas`为 1 时生效。
    * 方案二：多副本的支持，是否存在问题？
    * 是否支持多副本，应该有开关！
* 默认 `ScorePlugin` 可能带来的干扰，主要包括以下几个：

    * `TaintToleration(3)`：基于污点的选择调度
    * `NodeAffinity(2)`：基于节点亲和的选择调度
    * `InterPodAffinity(2)`：基于`Pod`亲和性的调度<br />
    * `SelectorSpread(1)`：由于默认开启`DefaultPodTopologySpread`特性，所以这个插件默认不生效。
    * `PodTopologySpread(2)`：基于拓扑域均衡 `Pod` 副本([参考](https://kubernetes.io/blog/2020/05/introducing-podtopologyspread/))，默认配置下这个插件不会产生影响。
* 修订 `kube-scheduler` 权限（由于 `PostBindPlugin`需要修订`Replicasets`）

  ```yaml
  apiVersion: rbac.authorization.k8s.io/v1
  kind: ClusterRole
  metadata:
    name: system:sp-scheduler
  rules:
  - apiGroups:
    - apps
    resources:
    - replicasets
    - replicasets/status
    verbs:
    - get
    - list
    - patch
    - update
  ---
  apiVersion: rbac.authorization.k8s.io/v1
  kind: ClusterRoleBinding
  metadata:
    name: system:sp-scheduler
  roleRef:
    apiGroup: rbac.authorization.k8s.io
    kind: ClusterRole
    name: system:sp-scheduler
  subjects:
  - apiGroup: rbac.authorization.k8s.io
    kind: User
    name: system:kube-scheduler

  ```