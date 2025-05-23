# Prometheus GCP Memorystore Service Discovery

[![release](https://img.shields.io/github/v/release/rickard-von-essen/prometheus-memorystore-sd?sort=semver)](https://github.com/rickard-von-essen/prometheus-memorystore-sd/releases)
[![build](https://github.com/rickard-von-essen/prometheus-memorystore-sd/actions/workflows/build.yml/badge.svg)](https://github.com/rickard-von-essen/prometheus-memorystore-sd/actions/workflows/build.yml)
[![go report](https://goreportcard.com/badge/github.com/rickard-von-essen/prometheus-memorystore-sd)](https://goreportcard.com/report/github.com/rickard-von-essen/prometheus-memorystore-sd)

Memorystore SD allows retrieving scrape targets from [GCP Memorystore](https://cloud.google.com/memorystore) for [Prometheus](https://prometheus.io/). **No address is defined by default**, it must be configured with relabeling and requires a [third-party exporter](https://prometheus.io/docs/instrumenting/exporters/#third-party-exporters) supporting the [multi-target pattern](https://prometheus.io/docs/guides/multi-target-exporter/).
This is a rewrite of the [Prometheus AWS ElastiCache Service Discovery (prometheus-elasticache-sd)](https://github.com/maxbrunet/prometheus-elasticache-sd).

## Configuration

Help on flags:

```
./prometheus-memorystore-sd --help
```

The following meta labels are available on targets during [relabeling](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#relabel_config):

* `__meta_memorystore_memcached_instance_id`: The instance ID.
* `__meta_memorystore_memcached_instance_state`: The current stat of the instance. See `State` in the [REST API reference](https://cloud.google.com/memorystore/docs/memcached/reference/rest/v1/projects.locations.instances#State_1) for possible values.
* `__meta_memorystore_memcached_location_id`: The _location_ where the instance is running. Location is a.k.a. _region_.
* `__meta_memorystore_memcached_project_id`: The GCP project ID where the instance is running.
* `__meta_memorystore_memcached_version`: The current version of the instance. See `MemcacheVersion` in the [REST API reference](https://cloud.google.com/memorystore/docs/memcached/reference/rest/v1/projects.locations.instances#MemcacheVersion) for possible values.
* `__meta_memorystore_memcached_full_version`: The full vesion of the instance, e.g. `memcached-1.5.16`.
* `__meta_memorystore_memcached_label_<labelKey>`: The Memorystore instance label's value.
* `__meta_memorystore_memcached_node_id`: The ID of the node, e.g. `node-c-1`.
* `__meta_memorystore_memcached_node_state`: The current stat of the node. See `State` in the [REST API reference](https://cloud.google.com/memorystore/docs/memcached/reference/rest/v1/projects.locations.instances#State) for possible values.
* `__meta_memorystore_memcached_host`: The IPv4 address of the node.
* `__meta_memorystore_memcached_port`: The TCP port of the node, e.g. `11211`.
* `__meta_memorystore_memcached_cpu_count`: The CPU count for the node.
* `__meta_memorystore_memcached_memory_size_gb`: The memory size in GB for the node.
* `__meta_memorystore_memcached_node_zone`: The _zone_ where the node is located.

The following GCP IAM permissions are required:

* `memcache.instances.list`
* `memcache.instances.get`

## Usage

### Docker

To run the Memorystore SD as a Docker container, run:

```
docker run ghcr.io/rickard-von-essen/prometheus-memorystore-sd:latest --help
```

### oliver006/redis_exporter

_Redis/Valkey is not supported yet._

### prometheus/memcached_exporter

This service discovery can be used with the official [memcached_exporter](https://github.com/prometheus/memcached_exporter),
see its [README](https://github.com/prometheus/memcached_exporter#multi-target) for details.

```yaml
scrape_configs:
  - job_name: "memcached_exporter_targets"
    file_sd_configs:
    - files:
        - /path/to/memorystore.json  # File created by service discovery
    metrics_path: /scrape
    relabel_configs:
      # Filter for memcached cache nodes
      - source_labels: [__meta_memorystore_memcached_full_version]
        regex: memcached
        action: keep
      # Build Memcached URL to use as target parameter for the exporter
      - source_labels:
          - __meta_memorystore_memcached_host
          - __meta_memorystore_memcached_port
        replacement: $1
        separator: ':'
        target_label: __param_target
      # Use Memcached URL as instance label
      - source_labels: [__param_target]
        target_label: instance
      # Set exporter address
      - target_label: __address__
        replacement: memcached-exporter-service.company.com:9151
```

## Development

### Build

Binary:

```
go build .
```

Container image with [ko](https://ko.build):

```
ko build --base-import-paths --local .
```

### Test

```
go test -v ./...
```

## License

Apache License 2.0, see [LICENSE](LICENSE).
