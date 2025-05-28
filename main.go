// Copyright 2025 Rickard von Essen
// Copyright 2022 Maxime Brunet
// Copyright 2018 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Prometheus Service Discovery for GCP Memorystore
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	memcache "cloud.google.com/go/memcache/apiv1"
	memcachepb "cloud.google.com/go/memcache/apiv1/memcachepb"
	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	versioncollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/promslog"
	"github.com/prometheus/common/promslog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/refresh"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/documentation/examples/custom-sd/adapter"
	"github.com/prometheus/prometheus/util/strutil"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

const (
	msMemcachedLabel           = model.MetaLabelPrefix + "memorystore_memcached_"
	memcacheLabelInstanceID    = msMemcachedLabel + "instance_id"
	memcacheLabelInstanceState = msMemcachedLabel + "instance_state"

	memcacheLabelProjectID   = msMemcachedLabel + "project_id"
	memcacheLabelLocationID  = msMemcachedLabel + "location_id"
	memcacheLabelVersion     = msMemcachedLabel + "version"
	memcacheLabelFullVersion = msMemcachedLabel + "full_version"

	memcacheLabelCPUCount     = msMemcachedLabel + "cpu_count"
	memcacheLabelMemorySizeGB = msMemcachedLabel + "memory_size_gb"

	memcacheLabelNodeID    = msMemcachedLabel + "node_id"
	memcacheLabelNodeZone  = msMemcachedLabel + "node_zone"
	memcacheLabelNodeState = msMemcachedLabel + "node_state"
	memcacheLabelHost      = msMemcachedLabel + "host"
	memcacheLabelPort      = msMemcachedLabel + "port"

	memcacheLabelLabel = msMemcachedLabel + "label_"
)

type memorystoreMetrics struct {
	refreshMetrics discovery.RefreshMetricsInstantiator
}

var _ discovery.DiscovererMetrics = (*memorystoreMetrics)(nil)

// Register implements discovery.DiscovererMetrics.
func (m *memorystoreMetrics) Register() error {
	return nil
}

// Unregister implements discovery.DiscovererMetrics.
func (m *memorystoreMetrics) Unregister() {}

// MemorystoreSDConfig is the configuration for Memorystore-based service discovery.
type MemorystoreSDConfig struct {
	Project             string
	Location            string
	CredentialsFile     string
	memcachedInstanceID string
	RefreshInterval     time.Duration
}

// Name returns the name of the Memorystore Config.
func (*MemorystoreSDConfig) Name() string { return "memorystore" }

// NewDiscoverer returns a Discoverer for the Memorystore Config.
func (c *MemorystoreSDConfig) NewDiscoverer(opts discovery.DiscovererOptions) (discovery.Discoverer, error) {
	return NewMemorystoreDiscovery(c, opts.Logger, opts.Metrics)
}

// NewDiscovererMetrics implements discovery.Config.
func (*MemorystoreSDConfig) NewDiscovererMetrics(_ prometheus.Registerer, rmi discovery.RefreshMetricsInstantiator) discovery.DiscovererMetrics {
	return &memorystoreMetrics{
		refreshMetrics: rmi,
	}
}

// MemorystoreDiscovery periodically performs Memorystore-SD requests. It implements
// the Prometheus Discoverer interface.
type MemorystoreDiscovery struct {
	*refresh.Discovery
	logger   *slog.Logger
	cfg      *MemorystoreSDConfig
	memcache *memcache.CloudMemcacheClient
	lasts    map[string]struct{}
}

// NewMemorystoreDiscovery returns a new MemorystoreDiscovery which periodically refreshes its targets.
func NewMemorystoreDiscovery(conf *MemorystoreSDConfig, logger *slog.Logger, metrics discovery.DiscovererMetrics) (*MemorystoreDiscovery, error) {
	m, ok := metrics.(*memorystoreMetrics)
	if !ok {
		return nil, errors.New("invalid discovery metrics type")
	}

	if logger == nil {
		logger = promslog.NewNopLogger()
	}

	d := &MemorystoreDiscovery{
		logger: logger,
		cfg:    conf,
	}

	d.Discovery = refresh.NewDiscovery(
		refresh.Options{
			Logger:              logger,
			Mech:                "memorystore",
			Interval:            d.cfg.RefreshInterval,
			RefreshF:            d.refresh,
			MetricsInstantiator: m.refreshMetrics,
		},
	)

	return d, nil
}

func (d *MemorystoreDiscovery) memcacheClient(ctx context.Context) (*memcache.CloudMemcacheClient, error) {
	if d.memcache != nil {
		return d.memcache, nil
	}

	opt := option.WithCredentialsFile(d.cfg.CredentialsFile)
	var err error
	d.memcache, err = memcache.NewCloudMemcacheClient(ctx, opt)

	return d.memcache, err
}

func (d *MemorystoreDiscovery) refresh(ctx context.Context) ([]*targetgroup.Group, error) {
	memcacheClient, err := d.memcacheClient(ctx)
	if err != nil {
		return nil, err
	}

	current := make(map[string]struct{})
	tgs := []*targetgroup.Group{}

	req := &memcachepb.ListInstancesRequest{
		Parent: fmt.Sprintf("projects/%s/locations/%s", d.cfg.Project, d.cfg.Location),
	}
	if len(d.cfg.memcachedInstanceID) > 0 {
		req.Filter = fmt.Sprintf("name:projects/%s/locations/%s/instances/%s", d.cfg.Project, d.cfg.Location, d.cfg.memcachedInstanceID)
	}

	it := memcacheClient.ListInstances(ctx, req)

	for {
		instance, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("could not list memcached instances: %w", err)
		}

		projectID, locationID, instanceID := parseInstanceName(instance.Name)

		labels := model.LabelSet{
			model.LabelName(memcacheLabelInstanceID):    model.LabelValue(instanceID),
			model.LabelName(memcacheLabelProjectID):     model.LabelValue(projectID),
			model.LabelName(memcacheLabelLocationID):    model.LabelValue(locationID),
			model.LabelName(memcacheLabelInstanceState): model.LabelValue(instance.State.String()),
			model.LabelName(memcacheLabelVersion):       model.LabelValue(instance.MemcacheVersion.String()),
			model.LabelName(memcacheLabelFullVersion):   model.LabelValue(instance.MemcacheFullVersion),
			model.LabelName(memcacheLabelCPUCount):      model.LabelValue(strconv.FormatInt(int64(instance.NodeConfig.CpuCount), 10)),
			model.LabelName(memcacheLabelMemorySizeGB):  model.LabelValue(strconv.FormatInt(int64(instance.NodeConfig.MemorySizeMb), 10)),
		}

		for key, val := range instance.Labels {
			labels[model.LabelName(memcacheLabelLabel+strutil.SanitizeLabelName(key))] = model.LabelValue(val)
		}

		for _, node := range instance.MemcacheNodes {
			nodeLabels := labels.Clone()
			nodeLabels[model.LabelName(memcacheLabelNodeID)] = model.LabelValue(node.NodeId)
			nodeLabels[model.LabelName(memcacheLabelNodeZone)] = model.LabelValue(node.Zone)
			nodeLabels[model.LabelName(memcacheLabelNodeState)] = model.LabelValue(node.State.String())
			nodeLabels[model.LabelName(memcacheLabelHost)] = model.LabelValue(node.Host)
			nodeLabels[model.LabelName(memcacheLabelPort)] = model.LabelValue(strconv.FormatInt(int64(node.Port), 10))

			// Placeholder address
			nodeLabels[model.AddressLabel] = model.LabelValue("undefined")

			source := fmt.Sprintf("%s/%s", instance.Name, node.NodeId)

			current[source] = struct{}{}

			tgs = append(tgs, &targetgroup.Group{
				Source: source,
				Labels: nodeLabels,
				Targets: []model.LabelSet{
					{model.AddressLabel: model.LabelValue("undefined")},
				},
			})
		}
	}

	// Add empty groups for target which have been removed since the last refresh.
	for k := range d.lasts {
		if _, ok := current[k]; !ok {
			d.logger.Debug("target deleted", "source", k)

			tgs = append(tgs, &targetgroup.Group{Source: k})
		}
	}

	d.lasts = current

	return tgs, nil
}

// Parses projects/{project_id}/locations/{location_id}/instances/{instance_id} .
func parseInstanceName(name string) (string, string, string) {
	instanceNameRE := regexp.MustCompile("projects/([^/]+)/locations/([^/]+)/instances/(.+)")
	parts := instanceNameRE.FindSubmatch([]byte(name))

	return string(parts[1]), string(parts[2]), string(parts[3])
}

func ouputHTTPHandler(outputFile *string, logger *slog.Logger) func(w http.ResponseWriter, _ *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		jsonFile, err := os.ReadFile(*outputFile)
		// if we os.Open returns an error then handle it
		if err != nil {
			logger.Error("Failed to load target file.", "err", err)
			return
		}

		w.Header().Add("Content-Type", "application/json")
		_, err = w.Write(jsonFile)
		if err != nil {
			logger.Error("Response failed.", "err", err)
		}
	}
}

func main() {
	var (
		gcpProject            = kingpin.Flag("gcp.project", "The GCP project ID. If not provided, the default project from the GCP credential chain is used.").String()
		gcpLocation           = kingpin.Flag("gcp.location", "The GCP Memorystore Memcached location ID.").String()
		msMemcachedInstanceID = kingpin.Flag("memorystore.memcached-instance-id", "The user-supplied Memorystore Memcache identifier. If this parameter is specified, only information about that specific instance is returned. This parameter should be on the form projects/{project_id}/locations/{location_id}/instances/{instance_id}.").String()
		targetRefreshInterval = kingpin.Flag("target.refresh-interval", "Refresh interval to re-read the memorystore list.").Default("60s").Duration()
		outputFile            = kingpin.Flag("output.file", "The output filename for file_sd compatible file.").Default("memorystore.json").String()
		outputHTTPPath        = kingpin.Flag("output.http-path", "Path under which to expose targets, must start with /. \"\" disables HTTP service discovery").Default("/memorystore.json").String()
		webConfig             = webflag.AddFlags(kingpin.CommandLine, ":8888")
		metricsPath           = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
	)

	promslogConfig := &promslog.Config{}

	flag.AddFlags(kingpin.CommandLine, promslogConfig)
	kingpin.Version(version.Print("prometheus-memorystore-sd"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	logger := promslog.New(promslogConfig)

	logger.Info("Starting prometheus-memorystore-sd", "version", version.Info())
	logger.Info("Build context", "context", version.BuildContext())

	conf := &MemorystoreSDConfig{
		Project:             *gcpProject,
		Location:            *gcpLocation,
		memcachedInstanceID: *msMemcachedInstanceID,
		RefreshInterval:     *targetRefreshInterval,
	}
	discovery.RegisterConfig(conf)

	reg := prometheus.NewRegistry()
	refreshMetrics := discovery.NewRefreshMetrics(reg)
	metrics, err := discovery.RegisterSDMetrics(reg, refreshMetrics)
	if err != nil {
		logger.Error("failed to register service discovery metrics", "err", err)
		os.Exit(1)
	}

	discMetrics, ok := metrics[conf.Name()]
	if !ok {
		logger.Error("discoverer metrics not registered")
		os.Exit(1)
	}

	disc, err := conf.NewDiscoverer(discovery.DiscovererOptions{
		Logger:  logger,
		Metrics: discMetrics,
	})
	if err != nil {
		logger.Error("failed to instantiate discoverer", "err", err)
		os.Exit(1)
	}
	ctx := context.Background()

	sdAdapter := adapter.NewAdapter(ctx, *outputFile, "memorystore_sd", disc, logger, metrics, reg)
	sdAdapter.Run()

	prometheus.MustRegister(versioncollector.NewCollector("prometheus_memorystore_sd"))

	http.Handle(*metricsPath, promhttp.Handler())
	landingPage, err := web.NewLandingPage(web.LandingConfig{
		Name:        "Memorystore Service Discovery",
		Description: "Prometheus Memorystore Service Discovery",
		Version:     version.Info(),
		Links: []web.LandingLinks{
			{
				Address: *metricsPath,
				Text:    "Metrics",
			},
		},
	})
	if err != nil {
		logger.Error("Error instantiating landing page", "err", err)
		os.Exit(1)
	}

	http.Handle("/", landingPage)

	if *outputHTTPPath != "" {
		http.HandleFunc(*outputHTTPPath, ouputHTTPHandler(outputFile, logger))
	}

	srv := &http.Server{}

	if err := web.ListenAndServe(srv, webConfig, logger); err != nil {
		logger.Error("Error starting HTTP server", "err", err)
		os.Exit(1)
	}
}
