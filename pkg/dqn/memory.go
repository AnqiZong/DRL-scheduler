package dqn

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/aunum/log"
	"github.com/gammazero/deque"
	"gorgonia.org/tensor"
)

// Event is an event that occurred.
type Event struct {
	// 下一个状态
	NextState *tensor.Dense
	// State by which the action was taken.
	State *tensor.Dense
	// 选取的节点名称
	Action string
	// 获取的奖励
	Reward float64
	// 该event 在队列中的序号
	i int
}

// NewEvent returns a new event
func NewEvent(state *tensor.Dense, action string, reward float64, nextState *tensor.Dense) Event {
	return Event{
		State:     state,
		Action:    action,
		NextState: nextState,
		Reward:    reward,
	}
}

// Print the event.
func (e *Event) Print() {
	log.Infof("event --> \n state: %v \n action: %v \n reward: %v \n  nextState: %v\n\n", e.State, e.Action, e.Reward, e.NextState)
}

// Memory for the dqn agent.
type Memory struct {
	*deque.Deque[Event]
}

// NewMemory returns a new Memory store.
func NewMemory() *Memory {
	return &Memory{
		Deque: deque.New[Event](),
	}
}

// Sample from the memory with the given batch size.
func (m *Memory) Sample(batchsize int) ([]Event, error) {
	if m.Len() < batchsize {
		return nil, fmt.Errorf("queue size %d is less than batch size %d", m.Len(), batchsize)
	}
	events := []Event{}
	rand.Seed(time.Now().UnixNano())
	for i, value := range rand.Perm(m.Len()) {
		if i >= batchsize {
			break
		}
		event := m.At(value)
		event.i = value
		events = append(events, event)
	}
	return events, nil
}
