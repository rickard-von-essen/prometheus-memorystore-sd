package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	memcachepb "cloud.google.com/go/memcache/apiv1/memcachepb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/promslog"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/documentation/examples/custom-sd/adapter"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type fakeMemcacheServer struct {
	memcachepb.UnimplementedCloudMemcacheServer
	instances []*memcachepb.Instance
}

func (f *fakeMemcacheServer) ListInstances(_ context.Context, _ *memcachepb.ListInstancesRequest) (*memcachepb.ListInstancesResponse, error) {
	return &memcachepb.ListInstancesResponse{Instances: f.instances}, nil
}

func TestMemorystoreIntegrationProducesServiceDiscoveryFile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const bufSize = 1024 * 1024
	lis := bufconn.Listen(bufSize)
	server := grpc.NewServer()
	instance := &memcachepb.Instance{
		Name:                "projects/test-project/locations/us-central1/instances/test-instance",
		State:               memcachepb.Instance_READY,
		MemcacheVersion:     memcachepb.MemcacheVersion_MEMCACHE_1_5,
		MemcacheFullVersion: "memcached-1.5.16",
		NodeConfig:          &memcachepb.Instance_NodeConfig{CpuCount: 2, MemorySizeMb: 1024},
		MemcacheNodes: []*memcachepb.Instance_Node{
			{
				NodeId: "node-a-1",
				Zone:   "us-central1-a",
				State:  memcachepb.Instance_Node_READY,
				Host:   "10.0.0.5",
				Port:   11211,
			},
		},
		Labels: map[string]string{
			"environment": "test",
			"service":     "foo",
		},
	}
	memcachepb.RegisterCloudMemcacheServer(server, &fakeMemcacheServer{instances: []*memcachepb.Instance{instance}})

	go func() {
		err := server.Serve(lis)
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Errorf("grpc server exited: %v", err)
		}
	}()
	defer server.Stop()
	defer lis.Close()

	tempDir := t.TempDir()
	outputFile := filepath.Join(tempDir, "memorystore.json")

	cfg := &MemorystoreSDConfig{
		Project:         "test-project",
		Location:        "test-location",
		RefreshInterval: 50 * time.Millisecond,
	}

	discovery.RegisterConfig(cfg)

	logger := promslog.NewNopLogger()
	reg := prometheus.NewRegistry()
	refreshMetrics := discovery.NewRefreshMetrics(reg)
	sdMetrics, err := discovery.RegisterSDMetrics(reg, refreshMetrics)
	if err != nil {
		t.Fatalf("failed to register service discovery metrics: %v", err)
	}

	discMetrics, ok := sdMetrics[cfg.Name()]
	if !ok {
		t.Fatalf("discoverer metrics not present for config")
	}

	disc, err := cfg.NewDiscoverer(discovery.DiscovererOptions{Logger: logger, Metrics: discMetrics})
	if err != nil {
		t.Fatalf("failed to create discoverer: %v", err)
	}

	memorystoreDisc, ok := disc.(*MemorystoreDiscovery)
	if !ok {
		t.Fatalf("unexpected discoverer type: %T", disc)
	}

	memorystoreDisc.clientOptions = append(memorystoreDisc.clientOptions,
		option.WithEndpoint("bufconn"),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
		option.WithGRPCDialOption(grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			return lis.Dial()
		})),
	)

	sdAdapter := adapter.NewAdapter(ctx, outputFile, "memorystore_sd", memorystoreDisc, logger, sdMetrics, reg)
	sdAdapter.Run()

	deadline := time.Now().Add(5 * time.Second)
	var content []byte
	for {
		content, err = os.ReadFile(outputFile)
		if err == nil && len(content) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for service discovery file: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	cancel()
	if memorystoreDisc.memcache != nil {
		_ = memorystoreDisc.memcache.Close()
	}

	expected := `
[
	{
		"targets": ["undefined"],
		"labels": {
    	"__address__": "undefined",
    	"__meta_memorystore_memcached_cpu_count": "2",
    	"__meta_memorystore_memcached_full_version": "memcached-1.5.16",
    	"__meta_memorystore_memcached_host": "10.0.0.5",
    	"__meta_memorystore_memcached_instance_id": "test-instance",
    	"__meta_memorystore_memcached_instance_state": "READY",
    	"__meta_memorystore_memcached_label_environment": "test",
    	"__meta_memorystore_memcached_label_service": "foo",
    	"__meta_memorystore_memcached_location_id": "us-central1",
    	"__meta_memorystore_memcached_memory_size_gb": "1024",
    	"__meta_memorystore_memcached_node_id": "node-a-1",
    	"__meta_memorystore_memcached_node_state": "READY",
    	"__meta_memorystore_memcached_node_zone": "us-central1-a",
    	"__meta_memorystore_memcached_port": "11211",
    	"__meta_memorystore_memcached_project_id": "test-project",
    	"__meta_memorystore_memcached_version": "MEMCACHE_1_5"
		}
	}
]
`
	require.JSONEq(t, expected, string(content))
}
