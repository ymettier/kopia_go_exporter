[![GitHub](https://img.shields.io/badge/ymettier/kopia__exporter-grey?logo=github)](https://github.com/ymettier/kopia_go_exporter)
[![Release](https://img.shields.io/github/v/release/ymettier/kopia_go_exporter?color=blue)](https://github.com/ymettier/kopia_go_exporter/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Go Build](https://github.com/ymettier/kopia_go_exporter/actions/workflows/go.yml/badge.svg)](https://github.com/ymettier/kopia_go_exporter/actions/workflows/go.yml)
[![golangci-lint](https://github.com/ymettier/kopia_go_exporter/actions/workflows/golangci-lint.yml/badge.svg)](https://github.com/ymettier/kopia_go_exporter/actions/workflows/golangci-lint.yml)
[![codecov](https://img.shields.io/codecov/c/github/ymettier/kopia_go_exporter)](https://codecov.io/gh/ymettier/kopia_go_exporter)

# kopia-go-exporter

Kopia-go-exporter is a Prometheus exporter for [Kopia](https://kopia.io/) backup repositories. It connects to a Kopia API server, retrieves snapshot metrics (size, file/dir counts, errors, duration, timestamps), and exposes them via an HTTP `/metrics` endpoint for Prometheus scraping.

See the [Motivations](#motivations) section below for background on why this project exists.

## Getting Started

1. **Download**: Get the latest binary for your platform from the [GitHub releases page](https://github.com/ymettier/kopia_go_exporter/releases).
2. **Configure**: Create a `config.yaml` file with the following contents. All options are documented with comments in the [config.default.yaml](config.default.yaml) file.
   ```yaml
   exporter:
     port: 9090
     metrics:
       prefix: "kopia_go_exporter"
     interval: 300

   kopia:
     password: "you can also set me in KGE_KOPIA_PASSWORD env var"
     apiserver:
       repositoryURL: "https://some.url:port"
       hostname: "myhostname"
       username: "mybackup"
       fingerprint: "you can also set me in KGE_KOPIA_APISERVER_FINGERPRINT env var"
     retentionstoextract:
       - "latest-1"

   logger:
     log_level: "info"
   ```
3. **Run**:
   ```sh
   ./kopia-go-exporter --config config.yaml
   ```
4. **Verify**: Check that metrics are available at `http://localhost:9090/metrics` in your browser or via `curl`. You should see Kopia backup metrics such as `kopia_go_exporter_total_size` and `kopia_go_exporter_backup_duration`.

## Run with docker/podman

The container image is published on the GitHub Container Registry:

```
ghcr.io/ymettier/kopia_go_exporter:0.2.0-rc3
```

1. **Configure**: Create a `config.yaml` file as described in the [Getting Started](#getting-started) section.
2. **Run**:
   ```sh
   podman run -d \
     --name kopia-go-exporter \
     -p 9090:9090 \
     -e KGE_KOPIA_PASSWORD="your-kopia-password" \
     -e KGE_KOPIA_APISERVER_FINGERPRINT="your-server-fingerprint" \
     -v /path/to/config.yaml:/config.yaml:ro \
     ghcr.io/ymettier/kopia_go_exporter:0.2.0-rc3 \
     --config /config.yaml
   ```
   Sensitive values (password, fingerprint) are passed preferably as environment variables.
3. **Verify**: Open `http://localhost:9090/metrics` in your browser or use `curl http://localhost:9090/metrics` to confirm metrics are being exported.

## Kubernetes

The chart is located in `charts/kopia_go_exporter`.

**Configuration:**
1. Create the Kopia credentials Secret:
   ```sh
    kubectl create secret generic kopia-config \
     --from-literal=password='your-kopia-password' \
      --from-literal=fingerprint='your-server-fingerprint'
   ```
2. Create a custom values file with your configuration:

   ```sh
   cat <<EOF > myvalues.yaml
   config:
     kopia:
       apiserver:
         repositoryURL: "https://host:port"
         hostname: "myhost"
         username: "myuser"
   EOF
   ```

   See `charts/kopia_go_exporter/values.yaml` and `config.default.yaml` for all available options.

**Install:**
```sh
helm upgrade --install --rollback-on-failure kopia-go-exporter oci://ghcr.io/ymettier/charts/kopia-go-exporter --version 0.1.0 -f myvalues.yaml
```

Note: Helm 3 users need `--atomic` instead of `--rollback-on-failure`.

## Configuration

The file `config.default.yaml` contains all the configuration.

Kopia-go-exporter can use such a configuration file: run it with `--config /path/to/config.yaml` or `-c /path/to/config.yaml` where the `config.yaml` file is based on `config.default.yaml` and modified as you wish.

You can also override this configuration with environment variables. They are forged on the configuration items in `config.default.yaml`, with `KGE_` prefix, the path of the item, flattened, uppercase, with underscores as a separator. Example:
```
kopia:
  apiserver:
    repositoryURL: "https://some.url:port"
```
can be overridden with
```
KGE_KOPIA_APISERVER_REPOSITORYURL=https://some.url:port
```

## Building

To build from source:

```sh
echo dev > version.txt && go build
```

To run the tests:

```sh
go test ./...
```

## Motivations

**Kopia lacks per-backup metrics.** Kopia only exposes global metrics, not per-snapshot or per-source metrics. This is a known limitation ([kopia#609](https://github.com/kopia/kopia/issues/609), closed as not planned). Related discussions: [kopia#4100](https://github.com/kopia/kopia/pull/4100), [kopia#4685](https://github.com/kopia/kopia/pull/4685), [kopia#5014](https://github.com/kopia/kopia/issues/5014).

**Existing alternatives are unmaintained.** [kopia-exporter](https://github.com/alvistar/kopia-exporter) by alvistar works fine but it seems to be unmaintained.

**I prefer an exporter written in Go.** I wanted an exporter written in Go, with tests including one that launches a real Kopia server with sample backups.

I wrote this project and I maintain it on my free time for my personal use. Contributions are welcome and I will do my best to review them.

## License

Kopia-go-exporter is licensed under the [MIT License](https://opensource.org/licenses/MIT).

<!-- Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved. -->
<!-- SPDX-License-Identifier: MIT -->
