package utils

import (
	"fmt"
	"testing"
)

func TestPrometheusConnection(t *testing.T) {
	var prometheusAddr string = "http://192.168.75.130:30200"
	promClient, err := NewPromClient(prometheusAddr)
	if err != nil {
		t.Logf("err=====>%s", err)
	}
	nodeMetrics, _ := promClient.QueryNodeMetrics("node1")
	for i := 0; i < 8; i++ {
		fmt.Println(nodeMetrics[i])
	}
	//fmt.Printf("the values of Prometheus query is: %v\n", value)
}
