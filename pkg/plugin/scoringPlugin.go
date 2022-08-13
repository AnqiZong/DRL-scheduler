package plugin

import (
	"context"
	v1 "k8s.io/api/core/v1"
	"github.com/AnqiZong/DRL-scheduler/pkg/dqn"
	"github.com/AnqiZong/DRL-scheduler/pkg/common"
	"github.com/AnqiZong/DRL-scheduler/pkg/utils"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	runtime2 "k8s.io/kubernetes/pkg/scheduler/framework/runtime"
	metricsClientSet "k8s.io/metrics/pkg/client/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"gorgonia.org/tensor"
)
//  定义使用到的常量字符串
const (
	Name = "DRLScheduler"
	preScoreStateKey = "PreScore" + Name
)
//  定义连接prometheus 使用的参数，该值从配置文件获取
type Promarg struct {
	PrometheusAddr string `json:"prometheus_address,omitempty"`
}
//  保存相同服务的冲突值
type ServiceRole struct{
	roleName string,
	values []float64
}
// 服务名 从标签中获取该信息
type Service struct {
	serviceName string,
	roles []ServiceRole
}
//  定义DQN能够使用到的参数
type DQNArgs struct {
	state *tensor.Dense
	reward float64,
	action int,
	nextState *tensor.Dense,
	//  定义神经网络的输出与nodeName之间的映射
	nodeMap map[string]int
}
// DRLScheduler 配置文件参数
type DRLSchedulerPluginArg struct {
	PrometheusClient utils.PromClient // 建立Prometheus client
	MetricsClientSet *metricsClientSet.Clientset // prometheus未安装时的 metriccs client
}
// DRLSchedulerPlugin 使用的参数
type DRLSchedulerPlugin struct {
	handle framework.Handle,
	args   DRLSchedulerPluginArg,
	DQNargs DQNArgs,
	agent Agent,
	Services []Service
}

func (n *DRLSchedulerPlugin) Name() string {
	return Name
}
// 在PreScore阶段，完成状态的保存、神经网络的训练，并计算出所有动作的Q值保存到数组中，供Score阶段使用
func (n *DRLSchedulerPlugin) PreScore(ctx context.Context, cycleState *framework.CycleState, pod *v1.Pod, nodes []*v1.Node) *framework.Status {
	if len(nodes) == 0 {
		return nil
	}
	// 获取当前状态将前执行的步骤保存,并训练神经网络
	if action != -1{
		nextState := n.PrometheusClient.QueryClusterMetrics(v1.Nodes().List(context.TODO(), metav1.ListOptions{}))
		n.agent.Remember(new dqn.Event(n.DQNArgs.state,n.DQNArgs.action,n.DQNArgs.reward,nextState))
		n.DQNArgs.state = nextState  
		err = n.agent.Learn() // 训练神经网络
		require.NoError(err)
	}else{
		n.DQNArgs.state = n.PrometheusClient.QueryClusterMetrics(v1.Nodes().List(context.TODO(), metav1.ListOptions{}))
	}
	// 获取预测的qValues
	prediction, err := n.agent.Policy.Predict(n.DQNArgs.state)
	require.NoError(err)
	// 把获取的qValues 保存
	cycleState.Write(preScoreStateKey, prediction)
	return nil
}

func (n *DRLSchedulerPlugin) Score(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) (int64, *framework.Status) {
	index := n.DQNargs.nodeMap[nodeName]
	prediction , err := state.Read(preScoreStateKey)
	if err != nil {
		return 0, framework.AsStatus(fmt.Errorf("reading %q from cycleState: %w", preScoreStateKey, err))
	}

	qValues, ok := prediction.(*tensor.Dense)
	if !ok {
		return 0, framework.AsStatus(fmt.Errorf("cannot convert saved state to tensor.Dense"))
	}
	return int64(qValues.At(index)),nil
}

func (n *DRLSchedulerPlugin) NormalizeScore(ctx context.Context, state *framework.CycleState, pod *v1.Pod, scores framework.NodeScoreList) *framework.Status {
	var maxScore int64 = -math.MaxInt64
	var maxIndex int64 = -1
	for i := range scores {
		score := scores[i].Score
		if score > maxScore {
			maxScore = score
			maxIndex = i
		}
	}
	index,err := n.agent.Action(len(scores))
	if err != nil {
		return framework.AsStatus(fmt.Errorf("sampling index of actions : %w", err))
	}
	// 如果是随机选择的动作，修改动作对应的分数为最大值+5
	if index != -1{
		maxScore += 5
		maxIndex = index
		scores[index].Score = maxScore
	}
	n.DQNargs.action = n.DQNargs.nodeMap[scores[maxIndex].Name]
	n.DQNargs.reward = dqn.GetReward(n.DQNargs.state,n.services,pod)
	serviceName,ok := pod.ObjectMeta.Labels["servicename"]
	if !ok {
		return framework.AsStatus(fmt.Errorf("the pod doesn't have servicename Label: %w", err))
	}
	serviceIndex := -1
	for i := range len(n.Services){
		if n.Services[i].serviceName == serviceName{
			serviceIndex = i
			break
		}
	}
	if serviceIndex == -1{
		tmp := new(Service)
		tmp.Name = serviceName
		tmp.roles = make([]ServiceRole)
		append(n.Services,tmp)
		serviceIndex = len(n.Services)-1
	}
	roleName := pod.ObjectMeta.Labels["rolename"]
	roleIndex = -1
	for i := range n.Services[serviceIndex] {
		if n.Services[serviceIndex].roleName == roleName{
			roleIndex = i
			break
		}
	}
	if roleIndex == -1{
		tmp := new(ServiceRole)
		tmp.roleName = roleName
		tmp.values = make([]float64)
		append(n.Services[serviceIndex],tmp)
		roleIndex = len(n.Services[serviceIndex])-1
	}
	append(n.Services[serviceIndex].values[roleIndex],n.DQNargs.reward)
	for i := range scores {
		scores[i].Score =  int64(scores[i].Score / maxScore * 100)
	}
	return nil
}

func (n *DRLSchedulerPlugin) ScoreExtensions() framework.ScoreExtensions {
	return n
}

func New(configuration runtime.Object, f framework.Handle) (framework.Plugin, error) {
	args := &DRLSchedulerPluginArg{}
	promarg := &Promarg{}
	err := runtime2.DecodeInto(configuration, promarg)
	if err != nil {
		return nil, err
	}
	args.PrometheusClient, _ = utils.NewPromClient(promarg.PrometheusAddr)
	// 获取K8S .kube/config配置信息
	config, err := utils.GetClientConfig()
	if err != nil {
		return nil, err
	}
	mcs, err := metricsClientSet.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	args.MetricsClientSet = mcs
	agent := dqn.NewAgent(dqn.DefaultAgentConfig)
	dqnargs := &DQNArgs{}
	var services []Service 
	nodeList, err := v1.Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err)
	}
	dqnargs.nodeMap = make(map[string]int, len(nodeList))
	index int = 0;
	for _, node := range nodeList.Items {
		dqnargs.nodeMap[node.Name] = index
		index++
	}
	dqnargs.action = -1
	dqnargs.reward = 0.0
	return &DRLSchedulerPlugin{
		handle: f,
		args:   *args,
		DQNargs: *dqnargs,
		agent: agent,
		Services: services
	}, nil
} 

func (n *DRLSchedulerPlugin) ScoreExtensions() framework.ScoreExtensions {
	return n
}