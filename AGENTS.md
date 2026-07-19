<!-- Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved. -->
<!-- SPDX-License-Identifier: MIT -->

# AGENTS.md

## Project Overview
kopia-go-exporter is a Prometheus exporter for Kopia backup repositories written in Go. It connects to a Kopia API server, retrieves snapshot metrics (size, file/dir counts, errors, duration, timestamps), and exposes them via an HTTP `/metrics` endpoint for Prometheus scraping.

## License
The project is licensed under the [MIT License](LICENSE).

## Technology Stack
- **Language**: Go 1.25.8+
- **Configuration**: `github.com/knadh/koanf` (YAML parsing)
- **CLI parsing**: `github.com/spf13/pflag`
- **Metrics**: `github.com/prometheus/client_golang`
- **Kopia client**: `github.com/kopia/kopia` (repo, snapshot, policy APIs)
- **Logging**: `log/slog` (stdlib)
- **Testing**: `testing` + `github.com/stretchr/testify/assert`
- **Integration testing**: a real Kopia API server started locally via the downloaded `kopia_test` binary
- **Test kopia binary version**: Version is `v0.23.1`.
- **Test kopia binary**: `kopiametrics/kopia_tests_helpers_test.go` downloads the kopia CLI (`kopia_test`) directly from GitHub releases so tests do not depend on a system-installed `kopia`. The binary is kept in `kopiametrics/test_assets/` between runs (name contains `test`). The version is **hardcoded** in `kopia_tests_helpers_test.go` (constant `kopiaTestVersion`) and must stay in sync with this document. The host OS/arch is detected at runtime to pick the correct release asset.
- **Build**: CGO_ENABLED=0, multi-stage Dockerfile (distroless runtime)
- Avoid `github.com/sirupsen/logrus` (indirect dependency only)

## Project Structure
```
.
├── main.go                  # Entry point, main loop with periodic RunOnce
├── main_test.go             # Tests for run(), config validation, context cancellation
├── logger/                  # Structured logging setup (slog, lumberjack rotation)
│   ├── logger.go
│   └── logger_test.go
├── config/
│   ├── config.go            # CLI flag parsing, Koanf config loading, env vars, validation
│   └── config_test.go
├── exporter/
│   ├── exporter.go          # Prometheus registry, HTTP /metrics endpoint, build_info metric
│   └── exporter_test.go
├── kopiametrics/
│   ├── kopia.go                  # Kopia API client, snapshot listing, metric registration
│   ├── kopia_test.go             # Integration tests
│   ├── kopia_tests_helpers_test.go  # Helpers to download/verify the kopia CLI binary for tests
│   └── test_assets/
│       └── kopia_test           # Downloaded kopia executable.
├── charts/
│   └── kopia_go_exporter/         # Helm chart for Kubernetes deployment
│       ├── Chart.yaml
│       ├── values.yaml
│       ├── .helmignore
│       └── templates/
│           ├── _helpers.tpl
│           ├── configmap.yaml
│           ├── deployment.yaml
│           ├── service.yaml
│           ├── serviceaccount.yaml
│           └── servicemonitor.yaml
├── .github/
│   ├── release.yml               # GitHub release changelog categories
│   └── workflows/
│       ├── go.yml                 # Go build and test
│       ├── golangci-lint.yml      # Linting
│       ├── docker-build.yml       # Container image build
│       ├── helm.yml               # Helm chart lint and release to GHCR
│       └── release.yml            # GoReleaser-based release
├── .gitignore
├── .golangci.yml             # golangci-lint v2 configuration
├── .goreleaser.yaml          # GoReleaser release configuration
├── config.default.yaml        # Example configuration (all options commented)
├── Dockerfile                # Multi-stage build (golang builder + distroless runtime)
├── go.mod / go.sum
├── version.txt               # Embedded at build time (//go:embed)
├── README.md
├── RELEASE.md                # Release workflow documentation
└── LICENSE                   # MIT License
```

## Key Components

### Configuration (config/config.go)
- The configuration file (read with `--config`/`-c`) is optional.
- CLI flag parsing via `ParseFlags()`: `--config`/`-c`, `--exporter-port`, `--log_level`/`-l`, `--version`/`-V`, `--help`/`-h`
- `New()` constructor: parses flags, loads YAML via Koanf, overlays env vars, validates, returns error
- `config.default.yaml` is embedded in the binary via `//go:embed` in `main.go` and provides all configuration defaults. It is loaded first as the base layer. The `--config` file, environment variables, and CLI flags overlay it.
- Koanf layered loading: embedded defaults (`config.default.yaml`) → YAML file → environment variables (KGE_ prefix) → pflag values
- Environment variable mapping: `KGE_KOPIA_PASSWORD` → `kopia.password` (uppercase, underscores → dots)
- Config validation in `CheckConfig()`: returns error on missing required fields
- Config struct: `config.Cfg` (global, populated at startup) with `Exporter`, `Filters`, `Kopia`, and `Logger` sub-structs
- `Filters` holds `Include`/`Exclude` `FilterConfig` entries, each with a `Path []string` list and the compiled `PathRegex []*regexp.Regexp`. Patterns are compiled to regexes at load time (see `readFiltersConfig`) so invalid patterns fail fast.
- `GetVersionInfo()` returns `VersionInfo` struct with version, revision, time, dirty, goVersion from build info
- `ReadBuildInfo` is an exported variable holding `debug.ReadBuildInfo`, mockable for testing
- Helper functions: `lookupConfigKey`, `getConfigString`, `getConfigInt`, `getConfigBool`
- After config is fully parsed, `logConfig()` logs every key at INFO level, one message per key. When `logger.redact_sensitive` is true (default), password and fingerprint values are replaced with `****`.

### Exporter (exporter/exporter.go)
- Constructor `NewExporter(cfg config.ExporterConfig)` receives the exporter config directly
- Creates a `prometheus.Registry` with Go and process collectors
- Registers `build_info` gauge with version/commit/date labels
- Starts HTTP server on configured port serving `/metrics`

### Kopia Metrics (kopiametrics/kopia.go)
- Constructor `NewKopiaClient(cfg *config.Config)` receives the full config directly
- `KopiaClient` manages connection lifecycle: `GenerateConfigFile` → `Connect` → `RunOnce` → `Disconnect`
- `RunOnce()` lists all snapshot manifests, groups by source, computes retention reasons, and sets gauge metrics
- Seven Prometheus gauge vectors: `total_size`, `file_count`, `dir_count`, `error_count`, `backup_duration`, `backup_start_time`, `backup_end_time`
- All metrics use labels: `host`, `path`, `user`, `retention`
- Metrics namespace prefix is configurable (default: `kopia_go_exporter`)
- Snapshot source paths are filtered before metrics are emitted: exclude regexes (`filters.exclude.path`) are checked first; an excluded path is still emitted if it matches an include regex (`filters.include.path`). Filtering happens in `matchPathFilters()` called from `setSnapshotMetrics()`.

### Main Loop (main.go)
- Configures logger, creates exporter and Kopia client
- Runs exporter HTTP server in a goroutine
- Main loop sleeps `interval` seconds between runs of `k.RunOnce()` (default 300)
- Graceful shutdown via SIGTERM/SIGINT → context cancellation → `k.Disconnect()`

### Helm Chart (charts/kopia_go_exporter/)
- Standard Helm chart for Kubernetes deployment
- `values.yaml` includes `kopiaConfigSecret: kopia-config` for the Kopia credentials Secret name
- Deployment template injects `KGE_KOPIA_PASSWORD` and `KGE_KOPIA_APISERVER_FINGERPRINT` from the Secret defined in `kopiaConfigSecret` (e.g. `kopia-config`) as environment variables
- ConfigMap serves the application config at `/config/config.yaml`
- ServiceMonitor supports `relabelings`, `metricRelabelings`, and `namespaceSelector`

## Development Guidelines

### Code Style
- Use structured logging (slog) instead of fmt.Printf for application output
- All public functions should have documentation comments
- Keep functions focused and under 50 lines when possible
- Use meaningful variable names
- Use `gofmt` / `goimports` formatting. Max line length 140.
- Group imports: stdlib first, third-party second, internal (`kopia-go-exporter/...`) last.
- Flags (like `-c` or `-p`) are never constants. When the linter complains, add `//nolint`.
- Copyright header on every source file. For `.go` files:
  ```go
  // Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
  // SPDX-License-Identifier: MIT
  ```
- Copyright year: `20XX-20YY` (creation year to current year), or `20XX` if same year.
  Derive `20XX` from `git log --diff-filter=A --follow <file>`.
- No copyright on `version.txt`. README.md copyright goes at end of file.
- Non-Go files use appropriate comment syntax: `//` for `.txt`, `#` for `.yaml`/`.yml`, `<!--` and `-->` for `.md`.
- Use `any` instead of `interface{}` (gofmt rewrites it)
- In markdown files, the indentation is 4 spaces.

### Naming
- Package names: single word, lowercase, matching directory name.
- Files: `package.go` and `package_test.go` (same package, not `_test` external).
- Constants: PascalCase or ALL_CAPS for string constants. Exported types: PascalCase. Unexported: camelCase.

### Patterns
- Constructors: `New()` returns a pointer for larger structs (e.g., `*KopiaClient`, `*Exporter`).
- Logger: each package calls `logger.Get()` locally instead of exposing a package-level variable.
- Context: pass `context.Context` to operations that may need cancellation.
- Global config: `config.Cfg` is the global populated at startup. Exporter and KopiaClient receive their config via constructors, not by reading the global.

### Environment Variables
- Use optional environment variables for configuration (e.g., `KGE_KOPIA_PASSWORD`...)
- Environment variables should override values from the config file
- Environment variable names should be in uppercase with underscores (e.g., `KGE_KOPIA_PASSWORD`)
- Environment variables should not be documented in the `config.default.yaml` except `KGE_KOPIA_APISERVER_FINGERPRINT` and `KGE_KOPIA_PASSWORD`.
- Environment variables should be prefixed with `KGE_` (e.g., `KGE_EXPORTER_PORT`, `KGE_KOPIA_PASSWORD`, `KGE_LOGGER_LOG_LEVEL`)
- The mapping converts uppercase underscores to dots: `KGE_KOPIA_APISERVER_FINGERPRINT` → `kopia.apiserver.fingerprint`
- README.md explains how to forge an environment variable from its definition on config.default.yaml (e.g. a `KGE_` prefix, flatten the path of the variable and replace dots with underscores).
- Config keys that contain an underscore as part of their name (not a path separator) must be mapped explicitly. The generic flattening turns `KGE_LOGGER_LOG_LEVEL` into `logger.log.level` and `KGE_LOGGER_REDACT_SENSITIVE` into `logger.redact.sensitive`, which do not match the real keys `logger.log_level` / `logger.redact_sensitive`. `kgeKeyMapper` (config/config.go) restores these keys after flattening; any new config key with an internal underscore must be added there too, and the manual `os.Getenv` override in `readLoggerConfig` must NOT be reintroduced (the env provider already handles it).

### config.default.yaml
- Every option must be present and commented with a short inline comment
- Comments should be as short as possible (a few words)
- Group related options under section comments
- Avoid dead options
- Values wrapped in `< >` (e.g., `<set me in KGE_KOPIA_PASSWORD env var>`) are **placeholders** that must be overridden by the user via config file, environment variables, or CLI flags. The binary embeds `config.default.yaml` at build time; at startup, `checkPlaceholders()` parses the embedded defaults to find keys whose values match `^<.*>$`, then verifies those keys have been overridden in the final config. Values like `xx<xx>xx` are intentionally allowed — only the `^<.*>$` pattern triggers the check. When adding a new placeholder, use `<description>` format as the value in `config.default.yaml`. Placeholders that were not initially in `config.default.yaml` are also allowed.

### Metrics
- Use `prometheus.NewGaugeVec` for snapshot-derived metrics
- Register metrics immediately after creation with `reg.MustRegister()`
- Assign labels consistently: `host`, `path`, `user`, `retention`
- Document what each metric represents in the Help text
- Metrics namespace prefix is configurable via `exporter.metrics.prefix`

### Error Handling
- Use slog for error logging with context
- Return errors explicitly, don't panic
- Log errors with relevant context (config file paths, URLs)
- Gracefully handle missing or corrupted configuration
- Exit with `os.Exit(1)` happens only in `main.go`

### Logging (logger/logger.go)
- Structured logging using log/slog with text or JSON handler
- File rotation via lumberjack.v2
- Configurable levels: INFO, DEBUG, ERROR, WARN
- JSON and text output formats
- Configuration through config.yaml: `logger.log_level`, `logger.json`, `logger.filename`, `logger.log_file.maxsize`, `logger.log_file.maxbackups`, `logger.log_file.maxage`, `logger.log_file.compress`

### Testing Conventions
- Write tests alongside features in `*_test.go` files
- Use testify assertions (`assert.NoError`, `assert.True`, `assert.Equal`)
- Integration tests in `kopiametrics/kopia_test.go` start a real Kopia API server locally via the downloaded `kopia_test` binary
- The downloaded kopia CLI binary is kept in `kopiametrics/test_assets/` between runs
- Unused test data files must be removed
- Table-driven tests with struct-based test cases
- Each `Test*` function is preceded by a short comment describing what it tests and the expected outcome; lines are wrapped at about 70 characters.
- For integration tests requiring a Kopia API server, use `setupTestKopia()`. For tests using a local filesystem repository without a server, use `setupTestRepo()`. Both helpers manage the kopia test binary automatically.

## Common Tasks

### Adding a New Configuration Option
1. Add field to `Config` struct in `config/config.go`
2. Add parsing logic in `readConfig()` or `CheckConfig()` (per-value reader functions like `readFiltersConfig()` may return an error for load-time validation, e.g. regex compilation)
3. Add test case in `config/config_test.go`
4. Update `config.default.yaml` with example value and short comment

### Adding a New Metric
1. Define in `KopiaMetrics` struct in `kopiametrics/kopia.go`
2. Register in `RegisterKopiaMetrics()` with `prometheus.NewGaugeVec`
3. Set/increment in `RunOnce()` after snapshot iteration
4. Update metrics documentation comments

### Modifying CLI Flags
1. Update flag definitions in `ParseFlags()` in `config/config.go`
2. Add both short and long form support
3. Update help text
4. Test with `--help` flag

### Updating Go Version
1. Update `go 1.xx.x` in `go.mod`
2. Update Go version reference in AGENTS.md
3. Update `FROM golang:1.xx` in Dockerfile
4. Run `go mod tidy` after updating

### Modifying Docker Image
1. Update the builder base image in Dockerfile
2. Update the runtime base image in Dockerfile
3. Test with `podman build -t kopia-go-exporter .`

### Modifying Helm Chart
1. Edit templates in `charts/kopia_go_exporter/templates/`
2. Update `values.yaml` for new configuration options
3. Test with `helm template ./charts/kopia_go_exporter`

## CI/CD (GitHub Actions)

- `go.yml`: Builds and tests on push/PR.
- `golangci-lint.yml`: Runs golangci-lint on push/PR.
- `docker-build.yml`: Builds and pushes the container image to `ghcr.io/ymettier/kopia_go_exporter:<version>`.
- `helm.yml`: Lints the Helm chart (Helm 3 & 4) and pushes to GHCR as OCI on `helm-v*` tags.
- `release.yml`: Uses GoReleaser to create a draft release with binary artifacts on tag push. Changelog categories are configured in `.github/release.yml`.

## Dependencies
- `github.com/knadh/koanf/v2` - Configuration management
- `github.com/prometheus/client_golang` - Prometheus metrics
- `github.com/kopia/kopia` - Kopia backup repository client
- `gopkg.in/natefinch/lumberjack.v2` - Log rotation
- `github.com/spf13/pflag` - CLI flag parsing
- `github.com/stretchr/testify` - Testing utilities

## Commits
- Never commit, never stage (`git add`), never run `git commit` — except if explicitly asked and confirmed by the user. Always suggest the command for the user to run.
- Never work in or commit to the `main` branch.
- Commit message: clear, descriptive, lowercase, no capital start.
- Follow [Conventional Commits](https://www.conventionalcommits.org/): `<type>: <description>`.
- Before commit, always check the copyright in the files to commit.

## Build & Run
- Build: `echo dev > version.txt && go build`
- Test: `echo dev > version.txt && go test ./...`
- Docker: `podman build -t kopia-go-exporter .`
- Run: `./kopia-go-exporter --config config.yaml`
- CLI flags: `--config` (config file), `--exporter-port` (exporter HTTP server port), `--log_level` (log level), `--version` (print version), `--help` (print help).
- When `version.txt` may have been modified. Always run `git restore version.txt` after each command.

## Helm Chart
- Chart location: `charts/kopia_go_exporter/`
- Template test: `helm template ./charts/kopia_go_exporter`
- The chart requires a pre-created Secret named `kopia-config` (configurable via `kopiaConfigSecret` in values.yaml) containing `password` and `fingerprint` keys

## Code coverage
- Packages `config`, `exporter` and `logger` should be covered at 100%.
- Package `main` should be covered at least at 75%.
- Package `kopiametrics` should be covered at 100%. Coverage for `kopiametrics` relies on integration tests that require a real Kopia server. Run `go test ./kopiametrics/` without `-short` to measure real coverage; `-short` mode skips them.

## Linting
- Run: `golangci-lint run ./...`
- Fallback (version mismatch):
  `podman run -t --rm -v $(pwd):/app:z -w /app golangci/golangci-lint:v2.12.2 golangci-lint run ./...`

## Version Management
- `version.txt` is embedded at build time via `//go:embed` and should contain the version string (e.g., `build` for dev, `1.0.0` for releases).
- `go.mod` sets the minimum Go version. Only bump when the code requires a newer toolchain feature.
- `Dockerfile` sets the latest stable Go version as 1.xx (example: 1.26).
- When updating a version, check all references across the project (go.mod, Dockerfile, AGENTS.md).

## Important Notes
- The Kopia password and API server fingerprint are sensitive — they should be provided via environment variables (`KGE_KOPIA_PASSWORD`, `KGE_KOPIA_APISERVER_FINGERPRINT`), not committed to the repository.
- The main loop sleeps 1 second at a time in a busy-wait pattern, counting down `sleepInterval` to the next `RunOnce()` call. This is intentional and must not be replaced with `time.After` or `time.Ticker` — the 1-second granularity allows the shutdown signal to be checked frequently and keeps the countdown logic simple and deterministic.
- Always use locally installed tools (e.g. `git`, `golangci-lint`, `go`, `gofmt`, `goimports`, `helm`...). Only `golangci-lint` may be run in a container if the local version mismatches with the local version of `go`.
