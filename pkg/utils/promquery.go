package utils

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	kv1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"gorgonia.org/tensor"
)

const (
	DefaultPrometheusQueryTimeout = 10 * time.Second
)

// PromClient provides client to interact with Prometheus.
type PromClient interface {
	QueryClusterMetrics([]*kv1.Node) (string, error)
	QueryNodeMetrics(string) ([]float64, error)
}

type promClient struct {
	API v1.API
}

// NewPromClient returns PromClient interface.
func NewPromClient(addr string) (PromClient, error) {
	config := api.Config{
		Address: addr,
	}

	client, err := api.NewClient(config)
	if err != nil {
		return nil, err
	}

	return &promClient{
		API: v1.NewAPI(client),
	}, nil
}

//  获取整个集群中的性能指标
func (p *promClient) QueryClusterMetrics(nodes []*kv1.Node) (*tensor.Dense, error) {
	metrics := make([][]float64, len(nodes))
	count := 0
	for _, node := range nodes {
		nodeMetrice, err := p.QueryNodeMetrics(node.Name)
		if err != nil {
			return "", err
		}
		metrics[count] = nodeMetrice
		count++
	}
	state := New(WithBacking(metrics), WithShape(len(filteredNodes), 8))
	return state, nil
}

// 根据节点名获取整个节点的性能指标
// cpu使用量、内存使用量、磁盘使用量、磁盘IO流入、cpu剩余量、内存剩余量、磁盘剩余量、磁盘IO流出
// 其中CPU使用量和内存使用量是取分配量和使用量的较大的值
// cpu 和内存使用量中取5分钟内使用的均值
func (p *promClient) QueryNodeMetrics(nodeName string) ([]float64, error) {
	nodeMetrics := make([]float64, 8)
	cpu_usage, err := p.GetCPUUasgeAndRequestMax(nodeName)
	if err != nil {
		return nil, err
	}
	nodeMetrics[0] = cpu_usage
	mem_usage, err := p.GetMemUasgeAndRequestMax(nodeName)
	if err != nil {
		return nil, err
	}
	nodeMetrics[1] = mem_usage
	fs_usage_string, err := p.query("sum (container_fs_usage_bytes{origin_prometheus=~\"\",device=~\"^/dev/.*$\",id=\"/\",node=\"" + nodeName + "\"})")
	if err != nil {
		return nil, err
	}
	nodeMetrics[2], _ = strconv.ParseFloat(fs_usage_string, 64)
	fs_write_string, err := p.query("sum(sum(rate(container_fs_writes_bytes_total{image!=\"\",node=\"" + nodeName + "\"} [ 1m])) without (device))")
	if err != nil {
		return nil, err
	}
	nodeMetrics[3], _ = strconv.ParseFloat(fs_write_string, 64)
	cpu_limit_string, err := p.query("sum(kube_node_status_allocatable{origin_prometheus=~\"\",resource=\"cpu\", unit=\"core\", node=\"" + nodeName + "\"})")
	if err != nil {
		return nil, err
	}
	cpu_limit, _ := strconv.ParseFloat(cpu_limit_string, 64)
	nodeMetrics[4] = cpu_limit - cpu_usage
	mem_limit_string, err := p.query("sum(kube_node_status_allocatable{origin_prometheus=~\"\",resource=\"memory\", unit=\"byte\", node=\"" + nodeName + "\"})")
	if err != nil {
		return nil, err
	}
	mem_limit, _ := strconv.ParseFloat(mem_limit_string, 64)
	nodeMetrics[5] = mem_limit - mem_usage

	fs_limit_string, err := p.query("sum (container_fs_limit_bytes{origin_prometheus=~\"\",device=~\"^/dev/.*$\",id=\"/\",node=\"" + nodeName + "\"})")
	if err != nil {
		return nil, err
	}
	fs_limit, _ := strconv.ParseFloat(fs_limit_string, 64)
	nodeMetrics[6] = fs_limit - nodeMetrics[2] // 获取存储剩余可用量
	fs_read_string, err := p.query("sum(sum(rate(container_fs_reads_bytes_total{image!=\"\",node=\"" + nodeName + "\"} [ 1m] )) without (device))")
	if err != nil {
		return nil, err
	}
	nodeMetrics[7], _ = strconv.ParseFloat(fs_read_string, 64)
	return nodeMetrics, nil
}
func (p *promClient) GetMemUasgeAndRequestMax(nodeName string) (float64, error) {
	mem_usage_string, err := p.query("sum(container_memory_working_set_bytes{origin_prometheus=~\"\",container!=\"\",node=\"" + nodeName + "\"})")
	if err != nil {
		return -1.0, err
	}
	mem_usage, err := strconv.ParseFloat(mem_usage_string, 64)
	if err != nil {
		return -1.0, err
	}
	mem_requests_string, err := p.query("sum(kube_pod_container_resource_requests{origin_prometheus=~\"\",resource=\"memory\", unit=\"byte\",node=\"" + nodeName + "\"})")
	if err != nil {
		return -1.0, err
	}
	mem_requests, err := strconv.ParseFloat(mem_requests_string, 64)
	if err != nil {
		return -1.0, err
	}
	return Ternary(mem_usage > mem_requests, mem_usage, mem_requests), nil
}

//  获取cpu使用量和申请量中较大的一个
func (p *promClient) GetCPUUasgeAndRequestMax(nodeName string) (float64, error) {
	cpu_usage_string, err := p.query("sum (irate(container_cpu_usage_seconds_total{origin_prometheus=~\"\",container!=\"\",node=\"" + nodeName + "\"}[3m]))")
	if err != nil {
		return -1.0, err
	}
	cpu_usage, err := strconv.ParseFloat(cpu_usage_string, 64)
	if err != nil {
		return -1.0, err
	}
	cpu_requests_string, err := p.query("sum(kube_pod_container_resource_requests{origin_prometheus=~\"\",resource=\"cpu\", unit=\"core\",node=\"" + nodeName + "\"})")
	if err != nil {
		return -1.0, err
	}
	cpu_requests, err := strconv.ParseFloat(cpu_requests_string, 64)
	if err != nil {
		return -1.0, err
	}
	return Ternary(cpu_usage > cpu_requests, cpu_usage, cpu_requests), nil
}

// 获取节点的单个性能指标
// func (p *promClient) QueryByNode(metricName, nodeName string) (string, error) {
// 	klog.V(4).Infof("Try to query %s by nodeName %s", metricName, nodeName)

// 	querySelector := fmt.Sprintf("sum(%s{origin_prometheus=~\"\",container!=\"\",node=\"%s\"})", metricName, nodeName)
// 	fmt.Printf("querySelector ====>%s\n", querySelector)
// 	testName := "sum(container_memory_working_set_bytes{origin_prometheus=~\"\",container!=\"\",node=\"node1\"})"
// 	fmt.Printf("testName===>%s\n", testName)
// 	result, err := p.query(querySelector)
// 	if result != "" && err == nil {
// 		return result, nil
// 	}

// 	return "", err
// }

func (p *promClient) query(query string) (string, error) {
	klog.V(4).Infof("Begin to query prometheus by promQL [%s]...", query)

	ctx, cancel := context.WithTimeout(context.Background(), DefaultPrometheusQueryTimeout)
	defer cancel()
	result, warnings, err := p.API.Query(ctx, query, time.Now())
	if err != nil {
		return "", err
	}

	if len(warnings) > 0 {
		return "", fmt.Errorf("unexpected warnings: %v", warnings)
	}

	if result.Type() != model.ValVector {
		return "", fmt.Errorf("illege result type: %v", result.Type())
	}

	var metricValue string
	for _, elem := range result.(model.Vector) {
		if float64(elem.Value) < float64(0) || math.IsNaN(float64(elem.Value)) {
			elem.Value = 0
		}
		metricValue = strconv.FormatFloat(float64(elem.Value), 'f', 5, 64)
	}

	return metricValue, nil
}
