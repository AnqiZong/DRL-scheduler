package dqn

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/AnqiZong/DRL-scheduler/pkg/common"
	"github.com/aunum/gold/pkg/v1/dense"
	"github.com/aunum/goro/pkg/v1/model"
	"github.com/aunum/log"
	"gorgonia.org/tensor"
)

// Agent is a dqn agent.
type Agent struct {
	// Base for the agent.
	//*agentv1.Base

	// Hyperparameters for the dqn agent.
	*Hyperparameters

	// Policy for the agent.
	Policy model.Model

	// Target policy for double Q learning.
	TargetPolicy model.Model

	// Epsilon is the rate at which the agent explores vs exploits.
	Epsilon common.Schedule

	//env               *envv1.Env
	epsilon           float32
	updateTargetSteps int
	batchSize         int
	memory            *Memory
	steps             int
}

// Hyperparameters for the dqn agent.
type Hyperparameters struct {
	// Gamma is the discount factor (0≤γ≤1). It determines how much importance we want to give to future
	// rewards. A high value for the discount factor (close to 1) captures the long-term effective award, whereas,
	// a discount factor of 0 makes our agent consider only immediate reward, hence making it greedy.
	Gamma float32

	// Epsilon is the rate at which the agent should exploit vs explore.
	Epsilon common.Schedule

	// UpdateTargetSteps determines how often the target network updates its parameters.
	UpdateTargetSteps int

	// BuferSize is the buffer size of the memory.
	BufferSize int
}

// DefaultHyperparameters are the default hyperparameters.
var DefaultHyperparameters = &Hyperparameters{
	Epsilon:           common.DefaultDecaySchedule(),
	Gamma:             0.95,
	UpdateTargetSteps: 100,
	BufferSize:        10e6,
}

// AgentConfig is the config for a dqn agent.
type AgentConfig struct {
	// Base for the agent.
	//Base *agentv1.Base

	// Hyperparameters for the agent.
	*Hyperparameters

	// PolicyConfig for the agent.
	PolicyConfig *PolicyConfig
}

// DefaultAgentConfig is the default config for a dqn agent.
var DefaultAgentConfig = &AgentConfig{
	Hyperparameters: DefaultHyperparameters,
	PolicyConfig:    DefaultPolicyConfig,
	//Base:            agentv1.NewBase("DeepQ"),
}

// NewAgent returns a new dqn agent.
func NewAgent(c *AgentConfig) (*Agent, error) {
	if c == nil {
		c = DefaultAgentConfig
	}
	if c.Epsilon == nil {
		c.Epsilon = common.DefaultDecaySchedule()
	}
	policy, err := MakePolicy("online", c.PolicyConfig)
	if err != nil {
		return nil, err
	}
	c.PolicyConfig.Track = false
	targetPolicy, err := MakePolicy("target", c.PolicyConfig)
	if err != nil {
		return nil, err
	}
	return &Agent{
		Hyperparameters: c.Hyperparameters,
		memory:          NewMemory(),
		Policy:          policy,
		TargetPolicy:    targetPolicy,
		Epsilon:         c.Epsilon,
		epsilon:         c.Epsilon.Initial(),
		updateTargetSteps: c.UpdateTargetSteps,
		batchSize:         c.PolicyConfig.BatchSize,
	}, nil
}

// Learn the agent.
func (a *Agent) Learn() error {
	if a.memory.Len() < a.batchSize {
		return nil
	}
	batch, err := a.memory.Sample(a.batchSize)
	if err != nil {
		return err
	}
	batchStates := []*tensor.Dense{}
	batchQValues := []*tensor.Dense{}
	for _, event := range batch {
		qUpdate := float32(event.Reward)
		// if !event.Done {
		// 	prediction, err := a.TargetPolicy.Predict(event.Observation)
		// 	if err != nil {
		// 		return err
		// 	}
		// 	qValues := prediction.(*tensor.Dense)
		// 	nextMax, err := dense.AMaxF32(qValues, 1)
		// 	if err != nil {
		// 		return err
		// 	}
		// 	qUpdate = event.Reward + a.Gamma*nextMax
		// }
		prediction, err := a.Policy.Predict(event.State)
		if err != nil {
			return err
		}
		qValues := prediction.(*tensor.Dense)
		qValues.Set(event.Action, qUpdate)
		batchStates = append(batchStates, event.State)
		batchQValues = append(batchQValues, qValues)
	}
	states, err := dense.Concat(0, batchStates...)
	if err != nil {
		return err
	}
	qValues, err := dense.Concat(0, batchQValues...)
	if err != nil {
		return err
	}
	err = a.Policy.FitBatch(states, qValues)
	if err != nil {
		return err
	}
	a.epsilon = a.Epsilon.Value()

	err = a.updateTarget()
	if err != nil {
		return err
	}
	return nil
}

// updateTarget copies the weights from the online network to the target network on the provided interval.
func (a *Agent) updateTarget() error {
	if a.steps%a.updateTargetSteps == 0 {
		log.Debugf("updating target model - current steps %v target update %v", a.steps, a.updateTargetSteps)
		err := a.Policy.(*model.Sequential).CloneLearnablesTo(a.TargetPolicy.(*model.Sequential))
		if err != nil {
			return err
		}
	}
	return nil
}

// Action selects the best known action for the given state.
func (a *Agent) Action(scoreslistLen int) (action int, err error) {
	//a.steps++
	a.Tracker.TrackValue("epsilon", a.epsilon)
	if num.RandF32(0.0, 1.0) < a.epsilon {
		// explore
		action, err = a.RandomSampleAction(scoreslistLen)
		if err != nil {
			return
		}
		return
	}
	return -1,nil
}

//  对不可选动作进行屏蔽，随机选择可部署动作
func (a *Agent) RandomSampleAction(scoreslistLen int) (action int, err error) {
	rand.Seed(time.Now().Unix()) // unix 时间戳，秒
	action = rand.Intn(scoreslistLen)
	return action, nil
}

// func (a *Agent) action(state *tensor.Dense) (action int, err error) {
// 	prediction, err := a.Policy.Predict(state)
// 	if err != nil {
// 		return
// 	}
// 	qValues := prediction.(*tensor.Dense)
// 	log.Debugv("qvalues", qValues)
// 	actionIndex, err := qValues.Argmax(1)
// 	if err != nil {
// 		return action, err
// 	}
// 	action = actionIndex.GetI(0)
// 	return
// }

// Remember an event.
func (a *Agent) Remember(event *Event) {
	a.memory.PushFront(event)
	if a.memory.Len() > a.BufferSize {
		a.memory.PopBack()
	}
}

func (a *Agent) Wait() {
	fmt.Print("\npress enter to exit\n")
	input := bufio.NewScanner(os.Stdin)
	input.Scan()
}
