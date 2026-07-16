[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Go Build](https://github.com/ymettier/kopia_go_exporter/actions/workflows/go.yml/badge.svg)](https://github.com/ymettier/kopia_go_exporter/actions/workflows/go.yml)
[![golangci-lint](https://github.com/ymettier/kopia_go_exporter/actions/workflows/golangci-lint.yml/badge.svg)](https://github.com/ymettier/kopia_go_exporter/actions/workflows/golangci-lint.yml)

# kopia-go-exporter

Kopia-go-exporter is a Prometheus exporter for [Kopia](https://kopia.io/) backup repositories. It connects to a Kopia API server, retrieves snapshot metrics (size, file/dir counts, errors, duration, timestamps), and exposes them via an HTTP `/metrics` endpoint for Prometheus scraping.

See the [Motivations](#motivations) section below for background on why this project exists.

## Getting Started

1. **Download**: Get the latest binary for your platform from the [GitHub releases page](https://github.com/ymettier/kopia_go_exporter/releases).
2. **Configure**: Create a `config.yaml` file with the following contents. All options are documented with comments in the [config.yaml.sample](config.yaml.sample) file.
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
ghcr.io/ymettier/kopia_go_exporter:0.1.0-rc1
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
     ghcr.io/ymettier/kopia_go_exporter:0.1.0-rc1 \
     --config /config.yaml
   ```
   Sensitive values (password, fingerprint) are passed as environment variables using the `KGE_` prefix. See the [config.yaml.sample](config.yaml.sample) for the full list of options and their corresponding environment variable names.
3. **Verify**: Open `http://localhost:9090/metrics` in your browser or use `curl http://localhost:9090/metrics` to confirm metrics are being exported.

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
