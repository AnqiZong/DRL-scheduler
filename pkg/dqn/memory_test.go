package dqn

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"gorgonia.org/tensor"
)

func TestMemory(t *testing.T) {
	mem := NewMemory()

	var pre *tensor.Dense
	for i := 0; i < 10; i++ {
		s := tensor.New(tensor.WithShape(1, 4), tensor.WithBacking([]int{i, i + 1, i + 2, i + 3}))
		ev := NewEvent(pre, "node1", 0, s)
		pre = s
		mem.PushFront(ev)
	}

	sample, err := mem.Sample(5)
	require.NoError(t, err)
	for _, s := range sample {
		fmt.Printf("%#v\n", s.NextState)
		fmt.Printf("%#v\n", s)
		fmt.Println("----")
	}
	require.Len(t, sample, 5)
}
