// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	"errors"
	"flag"
	"log/slog"
	"os"
	"runtime/debug"
	"testing"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clientDefaultName is the name of the single client used by the tests.
const clientDefaultName = "default"

// Parses CLI flags and checks that defaults, version/help error paths,
// and custom --config, --exporter-port and --log_level values are
// handled as expected.
func TestParseFlags(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantErr     bool
		wantErrHelp bool
	}{
		{
			name:    "default flags",
			args:    []string{},
			wantErr: false,
		},
		{
			name:        "version flag",
			args:        []string{"--version"},
			wantErr:     true,
			wantErrHelp: true,
		},
		{
			name:        "help flag",
			args:        []string{"--help"},
			wantErr:     true,
			wantErrHelp: true,
		},
		{
			name:    "custom config file",
			args:    []string{"--config", "custom.yaml"}, //nolint:goconst
			wantErr: false,
		},
		{
			name:    "custom exporter port",
			args:    []string{"--exporter-port", "9090"}, //nolint:goconst
			wantErr: false,
		},
		{
			name:        "invalid flag",
			args:        []string{"--nonexistent"},
			wantErr:     true,
			wantErrHelp: false,
		},
		{
			name:    "custom log level",
			args:    []string{"--log_level", "debug"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ParseFlags("test", tt.args)
			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrHelp {
					assert.ErrorIs(t, err, flag.ErrHelp)
				}
				return
			}
			require.NoError(t, err)
		})
	}
}

// Parses custom flag values and expects the returned config file path
// and parsed flags to match the provided arguments.
func TestParseFlags_CustomValues(t *testing.T) {
	configFile, _, err := ParseFlags("test", []string{"--config", "/tmp/custom.yaml", "--exporter-port", "8080", "--log_level", "warn"})
	require.NoError(t, err)
	assert.Equal(t, "/tmp/custom.yaml", configFile)
}

// Calls New with no --config flag and expects an error because the
// embedded default config still contains placeholder values.
func TestNew_NoConfigFile(t *testing.T) {
	err := New("test", []string{}, loadDefaultConfig(t))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "still has placeholder value")
}

// Calls New with no --config and the kopia.clients env vars, and
// expects success with the client read from those env vars.
func TestNew_NoConfigFile_WithClientsEnv(t *testing.T) {
	t.Setenv("KGE_KOPIA_APISERVER_REPOSITORYURL", "https://some.url:port")
	t.Setenv("KGE_KOPIA_APISERVER_FINGERPRINT", testRevision)
	t.Setenv("KGE_KOPIA_CLIENTS_DEFAULT_USERNAME", "myuser")
	t.Setenv("KGE_KOPIA_CLIENTS_DEFAULT_HOSTNAME", "myhost")
	t.Setenv("KGE_KOPIA_CLIENTS_DEFAULT_PASSWORD", "secret")

	err := New("test", []string{}, loadDefaultConfig(t))
	assert.NoError(t, err)
	require.Len(t, Cfg.Kopia.Clients, 1)
	assert.Equal(t, "secret", Cfg.Kopia.Clients[clientDefaultName].Password)
}

const (
	testRevision  = "abc123"
	vcsRevision   = "vcs.revision"
	vcsTime       = "vcs.time"
	missingKey    = "missing.key"
	missingKeyMsg = "missing key returns default"
)

// Builds version info with different build info states.
func TestGetVersionInfo(t *testing.T) {
	tests := []struct {
		name    string
		version string
		setup   func(version string)
		check   func(t *testing.T, vi VersionInfo, version string)
	}{
		{
			name:    "given version",
			version: "testVersion",
			setup: func(version string) {
				givenVersion = version
				orig := ReadBuildInfo
				ReadBuildInfo = func() (*debug.BuildInfo, bool) {
					return &debug.BuildInfo{
						GoVersion: "go1.25.0",
						Settings: []debug.BuildSetting{
							{Key: vcsRevision, Value: testRevision},
							{Key: vcsTime, Value: "2025-01-15T10:00:00Z"},
						},
					}, true
				}
				_ = orig
			},
			check: func(t *testing.T, vi VersionInfo, version string) {
				assert.Equal(t, version, vi.Version)
				assert.NotEmpty(t, vi.GoVersion)
			},
		},
		{
			name:    "returns vcs data",
			version: "1.0.0",
			setup: func(version string) {
				givenVersion = version
			},
			check: func(t *testing.T, vi VersionInfo, version string) {
				assert.Equal(t, version, vi.Version)
			},
		},
		{
			name:    "build info unavailable",
			version: "2.0.0",
			setup: func(version string) {
				orig := ReadBuildInfo
				ReadBuildInfo = func() (*debug.BuildInfo, bool) {
					return nil, false
				}
				_ = orig
				givenVersion = version
			},
			check: func(t *testing.T, vi VersionInfo, version string) {
				assert.Equal(t, version, vi.Version)
				assert.Empty(t, vi.Revision)
				assert.Empty(t, vi.Time)
			},
		},
		{
			name:    "with vcs settings",
			version: "3.0.0",
			setup: func(version string) {
				orig := ReadBuildInfo
				ReadBuildInfo = func() (*debug.BuildInfo, bool) {
					return &debug.BuildInfo{
						Settings: []debug.BuildSetting{
							{Key: vcsRevision, Value: "deadbeef"},
							{Key: vcsTime, Value: "2025-06-01T12:00:00Z"},
						},
					}, true
				}
				_ = orig
				givenVersion = version
			},
			check: func(t *testing.T, vi VersionInfo, version string) {
				assert.Equal(t, version, vi.Version)
				assert.Equal(t, "deadbeef", vi.Revision)
				assert.Equal(t, "2025-06-01T12:00:00Z", vi.Time)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origReadBuildInfo := ReadBuildInfo
			defer func() { ReadBuildInfo = origReadBuildInfo }()
			if tt.setup != nil {
				tt.setup(tt.version)
			}
			vi := GetVersionInfo()
			if tt.check != nil {
				tt.check(t, vi, tt.version)
			}
		})
	}
}

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	tmpFile := t.TempDir() + "/test.yaml"
	err := os.WriteFile(tmpFile, []byte(content), 0o600)
	require.NoError(t, err)
	return tmpFile
}

func loadDefaultConfig(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../config.default.yaml")
	require.NoError(t, err, "failed to read config.default.yaml for test")
	return data
}

// Looks up lowercase, camelCase, and nonexistent config keys and expects
// the value (or absence) to be reported correctly.
func TestLookupConfigKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
		ok   bool
	}{
		{
			name: "lowercase key",
			key:  "exporter.port",
			want: "9090",
			ok:   true,
		},
		{
			name: "camelCase key",
			key:  "exporter.metrics.prefix",
			want: "test_prefix",
			ok:   true,
		},
		{
			name: "nonexistent key",
			key:  "nonexistent.key",
			want: "",
			ok:   false,
		},
	}
	cfgFile := writeTestConfig(t, "exporter:\n  port: 9090\n  metrics:\n    prefix: test_prefix\n")
	k = koanf.New(".")
	err := k.Load(file.Provider(cfgFile), yaml.Parser())
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, ok := lookupConfigKey(k, tt.key)
			assert.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.Equal(t, tt.want, val)
			}
		})
	}
}

// Looks up a key written with an underscore format in the config and
// expects it to resolve to the dotted config path.
func TestLookupConfigKey_UnderscoreFormat(t *testing.T) {
	cfgFile := writeTestConfig(t, "exporter_metrics_prefix: underscore_prefix\n")
	k = koanfNew(t, cfgFile)

	val, ok := lookupConfigKey(k, "exporter.metrics.prefix")
	assert.True(t, ok)
	assert.Equal(t, "underscore_prefix", val)
}

// Reads a string config value and expects the existing value, or the
// default when the key is missing.
func TestGetConfigString(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue string
		want         string
	}{
		{
			name:         "existing key",
			key:          "existing.key",
			defaultValue: "default",
			want:         "value",
		},
		{
			name:         missingKeyMsg,
			key:          "missing.port",
			defaultValue: "default_value",
			want:         "default_value",
		},
	}
	cfgFile := writeTestConfig(t, "existing:\n  key: value\n")
	k = koanfNew(t, cfgFile)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := getConfigString(k, tt.key, tt.defaultValue)
			assert.Equal(t, tt.want, val)
		})
	}
}

// Reads an int config value and expects the parsed value, or the default
// for a missing key or a non-numeric value.
func TestGetConfigInt(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue int
		want         int
	}{
		{
			name:         "valid int",
			key:          "port",
			defaultValue: 9090,
			want:         8080,
		},
		{
			name:         missingKeyMsg,
			key:          missingKey,
			defaultValue: 9090,
			want:         9090,
		},
		{
			name:         "invalid int returns default",
			key:          "invalid",
			defaultValue: 9090,
			want:         9090,
		},
	}
	cfgFile := writeTestConfig(t, "port: 8080\ninvalid: not_a_number\n")
	k = koanfNew(t, cfgFile)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := getConfigInt(k, tt.key, tt.defaultValue)
			assert.Equal(t, tt.want, val)
		})
	}
}

// Reads a bool config value and expects true/false for
// "true"/"false"/"1", and the default for missing or invalid values.
func TestGetConfigBool(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue bool
		want         bool
	}{
		{
			name:         "true string",
			key:          "enabled",
			defaultValue: false,
			want:         true,
		},
		{
			name:         "false string",
			key:          "disabled",
			defaultValue: true,
			want:         false,
		},
		{
			name:         "1 string",
			key:          "one",
			defaultValue: false,
			want:         true,
		},
		{
			name:         missingKeyMsg,
			key:          missingKey,
			defaultValue: true,
			want:         true,
		},
		{
			name:         "invalid bool returns default",
			key:          "invalid_bool",
			defaultValue: false,
			want:         false,
		},
	}
	cfgFile := writeTestConfig(t, "enabled: true\ndisabled: \"false\"\none: \"1\"\ninvalid_bool: maybe\n")
	k = koanfNew(t, cfgFile)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := getConfigBool(k, tt.key, tt.defaultValue)
			assert.Equal(t, tt.want, val)
		})
	}
}

// Reads the exporter config section and expects port, metrics prefix,
// and interval to match the YAML.
func TestReadExporterConfig(t *testing.T) {
	cfgFile := writeTestConfig(t, "exporter:\n  port: 8080\n  metrics:\n    prefix: custom_prefix\n  interval: 60\n")
	k = koanfNew(t, cfgFile)

	cfg := readExporterConfig(k)
	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, "custom_prefix", cfg.Metrics.Prefix)
	assert.Equal(t, 60, cfg.Interval)
}

// Reads the exporter config with a flag override and expects the flag
// value to take precedence over the YAML value.
func TestReadExporterConfig_FlagOverride(t *testing.T) {
	cfgFile := writeTestConfig(t, "exporter:\n  port: 8080\n")

	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.Int("exporter-port", 9090, "Exporter HTTP server port")
	require.NoError(t, fs.Parse([]string{"--exporter-port", "7777"}))

	k = koanfNew(t, cfgFile)
	require.NoError(t, k.Load(
		posflag.ProviderWithValue(fs, ".", k, flagKeyMapper), nil))

	cfg := readExporterConfig(k)
	assert.Equal(t, 7777, cfg.Port)
}

// Reads the exporter config with no values set and expects the
// documented default port, prefix, and interval.
func TestReadExporterConfig_Defaults(t *testing.T) {
	k = koanf.New(".")

	cfg := readExporterConfig(k)
	assert.Equal(t, 9090, cfg.Port)
	assert.Equal(t, "kopia_go_exporter", cfg.Metrics.Prefix)
	assert.Equal(t, 300, cfg.Interval)
}

// Reads valid include/exclude filter paths and expects the compiled
// regex lists to be built.
func TestReadFiltersConfig(t *testing.T) {
	tests := []struct {
		name       string
		cfg        string
		useKoanf   bool
		wantErrSub string
		check      func(t *testing.T, cfg FiltersConfig)
	}{
		{
			name: "valid include and exclude",
			cfg: `filters:
  include:
    path:
      - ".*"
  exclude:
    path:
      - "/tmp/.*"
`,
			check: func(t *testing.T, cfg FiltersConfig) {
				assert.Equal(t, []string{".*"}, cfg.Include.Path)
				assert.Len(t, cfg.Include.PathRegex, 1)
				assert.Equal(t, []string{"/tmp/.*"}, cfg.Exclude.Path)
				assert.Len(t, cfg.Exclude.PathRegex, 1)
			},
		},
		{
			name:       "invalid include regex",
			cfg:        "filters:\n  include:\n    path:\n      - \"[\"\n",
			wantErrSub: "invalid filters.include.path regex",
		},
		{
			name:       "invalid exclude regex",
			cfg:        "filters:\n  exclude:\n    path:\n      - \"[\"\n",
			wantErrSub: "invalid filters.exclude.path regex",
		},
		{
			name: "invalid include structure",
			cfg:  "filters:\n  include:\n    path:\n      key: value\n",
			check: func(t *testing.T, cfg FiltersConfig) {
				assert.Len(t, cfg.Include.PathRegex, 1)
			},
		},
		{
			name: "invalid exclude structure",
			cfg:  "filters:\n  exclude:\n    path:\n      key: value\n",
			check: func(t *testing.T, cfg FiltersConfig) {
				assert.Len(t, cfg.Exclude.PathRegex, 1)
			},
		},
		{
			name:     "defaults",
			useKoanf: true,
			check: func(t *testing.T, cfg FiltersConfig) {
				assert.Equal(t, []string{}, cfg.Include.Path)
				assert.Empty(t, cfg.Include.PathRegex)
				assert.Equal(t, []string{}, cfg.Exclude.Path)
				assert.Empty(t, cfg.Exclude.PathRegex)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := slog.Default()
			var k *koanf.Koanf
			if tt.useKoanf {
				k = koanf.New(".")
			} else {
				cfgFile := writeTestConfig(t, tt.cfg)
				k = koanfNew(t, cfgFile)
			}
			cfg, err := readFiltersConfig(k, l)
			if tt.wantErrSub != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrSub)
				return
			}
			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}
func TestReadFiltersConfig_InvalidRegex(t *testing.T) {
	cfgFile := writeTestConfig(t, "filters:\n  include:\n    path:\n      - \"[\"\n")
	k = koanfNew(t, cfgFile)

	l := slog.Default()
	_, err := readFiltersConfig(k, l)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid filters.include.path regex")
}

// Reads an invalid exclude filter regex and expects an error mentioning
// the exclude path regex.
func TestReadFiltersConfig_InvalidExcludeRegex(t *testing.T) {
	cfgFile := writeTestConfig(t, "filters:\n  exclude:\n    path:\n      - \"[\"\n")
	k = koanfNew(t, cfgFile)

	l := slog.Default()
	_, err := readFiltersConfig(k, l)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid filters.exclude.path regex")
}

// Reads an include filter path given as a map instead of a list and
// expects it to be handled (one regex, no error).
func TestReadFiltersConfig_InvalidIncludeStructure(t *testing.T) {
	cfgFile := writeTestConfig(t, "filters:\n  include:\n    path:\n      key: value\n")
	k = koanfNew(t, cfgFile)

	l := slog.Default()
	cfg, err := readFiltersConfig(k, l)
	require.NoError(t, err)
	assert.Len(t, cfg.Include.PathRegex, 1)
}

// Reads an exclude filter path given as a map instead of a list and
// expects it to be handled (one regex, no error).
func TestReadFiltersConfig_InvalidExcludeStructure(t *testing.T) {
	cfgFile := writeTestConfig(t, "filters:\n  exclude:\n    path:\n      key: value\n")
	k = koanfNew(t, cfgFile)

	l := slog.Default()
	cfg, err := readFiltersConfig(k, l)
	require.NoError(t, err)
	assert.Len(t, cfg.Exclude.PathRegex, 1)
}

// Loads a config with invalid filters via readConfig and expects a
// config error mentioning the include path regex.
func TestReadConfig_InvalidFilters(t *testing.T) {
	tmpFile := writeTestConfig(t, "filters:\n  include:\n    path:\n      - \"[\"\n")

	err := readConfig(tmpFile, nil, loadDefaultConfig(t))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid filters.include.path regex")
}

// Reads filters with no values set and expects empty path lists and no
// compiled regexes.
func TestReadFiltersConfig_Defaults(t *testing.T) {
	k = koanf.New(".")
	l := slog.Default()

	cfg, err := readFiltersConfig(k, l)
	require.NoError(t, err)
	assert.Equal(t, []string{}, cfg.Include.Path)
	assert.Empty(t, cfg.Include.PathRegex)
	assert.Equal(t, []string{}, cfg.Exclude.Path)
	assert.Empty(t, cfg.Exclude.PathRegex)
}

// Loads a config whose include path is overridden by env vars and
// expects the env values to be used.
func TestReadFiltersConfig_EnvOverride(t *testing.T) {
	tmpFile := writeTestConfig(t, "filters:\n  include:\n    path:\n      - ignored\n")
	t.Setenv("KGE_FILTERS_INCLUDE_PATH", ".*")
	t.Setenv("KGE_FILTERS_EXCLUDE_PATH", "/tmp/.*")

	err := readConfig(tmpFile, nil, loadDefaultConfig(t))
	require.NoError(t, err)
	assert.Equal(t, []string{".*"}, Cfg.Filters.Include.Path)
	assert.Len(t, Cfg.Filters.Include.PathRegex, 1)
	assert.Equal(t, []string{"/tmp/.*"}, Cfg.Filters.Exclude.Path)
	assert.Len(t, Cfg.Filters.Exclude.PathRegex, 1)
}

// Reads the kopia config section and expects apiserver fields, named
// clients, and retentions to match the YAML.
func TestReadKopiaConfig(t *testing.T) {
	cfgFile := writeTestConfig(t, `kopia:
  apiserver:
    repositoryURL: "https://example.com:51515"
    fingerprint: abc123
  clients:
    host1:
      username: user1
      hostname: host1
      password: pwd1
    host2:
      username: user2
      hostname: host2
      password: pwd2
  retentionstoextract:
    - daily
    - weekly
`)
	k = koanfNew(t, cfgFile)

	l := slog.Default()
	cfg := readKopiaConfig(k, l)
	assert.Equal(t, "https://example.com:51515", cfg.APIServer.RepositoryURL)
	assert.Equal(t, testRevision, cfg.APIServer.Fingerprint)
	require.Len(t, cfg.Clients, 2)
	assert.Equal(t, ClientConfig{Username: "user1", Hostname: "host1", Password: "pwd1"}, cfg.Clients["host1"])
	assert.Equal(t, ClientConfig{Username: "user2", Hostname: "host2", Password: "pwd2"}, cfg.Clients["host2"])
	assert.Equal(t, []string{"daily", "weekly"}, cfg.Retentions)
}

// Reads the kopia config with no values set and expects all fields to be
// empty and retentions empty.
func TestReadKopiaConfigScenarios(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) *koanf.Koanf
		check func(t *testing.T, cfg KopiaConfig)
	}{
		{
			name: "defaults",
			setup: func(t *testing.T) *koanf.Koanf {
				t.Helper()
				return koanf.New(".")
			},
			check: func(t *testing.T, cfg KopiaConfig) {
				assert.Equal(t, "", cfg.APIServer.RepositoryURL)
				assert.Equal(t, "", cfg.APIServer.Fingerprint)
				assert.Empty(t, cfg.Clients)
				assert.Equal(t, []string{}, cfg.Retentions)
			},
		},
		{
			name: "clients unmarshal error",
			setup: func(t *testing.T) *koanf.Koanf {
				k := koanf.New(".")
				require.NoError(t, k.Set("kopia.clients", "not-a-map"))
				return k
			},
			check: func(t *testing.T, cfg KopiaConfig) {
				assert.Empty(t, cfg.Clients)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := tt.setup(t)
			cfg := readKopiaConfig(k, slog.Default())
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

// Loads a nonexistent config file and expects an error mentioning the
// failure to read the configuration file.
func TestReadConfig_MissingFile(t *testing.T) {
	k = koanf.New(".")
	err := readConfig("/nonexistent/config.yaml", nil, loadDefaultConfig(t))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read configuration file")
}

// Loads a config whose exporter port is overridden by an env var and
// expects the env value to be used.
func TestReadConfig_EnvOverride(t *testing.T) {
	tmpFile := writeTestConfig(t, "exporter:\n  port: 8080\n")

	t.Setenv("KGE_EXPORTER_PORT", "7777")

	k = koanf.New(".")
	err := readConfig(tmpFile, nil, loadDefaultConfig(t))
	require.NoError(t, err)
	assert.Equal(t, 7777, Cfg.Exporter.Port)
}

type stubConfigProvider struct {
	err error
}

func (s stubConfigProvider) ReadBytes() ([]byte, error) {
	return nil, s.err
}

func (s stubConfigProvider) Read() (map[string]any, error) {
	if s.err != nil {
		return nil, s.err
	}
	return map[string]any{"exporter": map[string]any{"port": 9090}}, nil
}

// Loads a config layer from a provider and expects values to be applied,
// and a provider error to be ignored without failure.
func TestLoadConfigLayer(t *testing.T) {
	tests := []struct {
		name     string
		provider stubConfigProvider
		wantPort int
	}{
		{
			name:     "success",
			provider: stubConfigProvider{},
			wantPort: 9090,
		},
		{
			name:     "provider error ignored",
			provider: stubConfigProvider{err: errors.New("boom")}, //nolint:err113
			wantPort: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := koanf.New(".")
			loadConfigLayer(k, tt.provider, "failed to load stub")
			if tt.wantPort != 0 {
				assert.Equal(t, tt.wantPort, k.Int("exporter.port"))
				return
			}
			_, exists := lookupConfigKey(k, "exporter.port")
			assert.False(t, exists)
		})
	}
}

// Checks a fully populated config and expects no validation error.
func TestCheckConfig_ValidConfig(t *testing.T) {
	origCfg := Cfg
	defer func() { Cfg = origCfg }()

	k = koanf.New(".")
	Cfg = Config{
		Kopia: KopiaConfig{
			APIServer: APIServerConfig{
				RepositoryURL: "https://example.com:51515",
				Fingerprint:   testRevision,
			},
			Clients: map[string]ClientConfig{
				clientDefaultName: {
					Username: "kopia",
					Hostname: "localhost",
					Password: "test",
				},
			},
		},
	}
	assert.NoError(t, CheckConfig(nil))
}

// TestCheckConfig_MissingFields verifies that CheckConfig reports an
// error when each required Kopia field is missing.
func TestCheckConfig_MissingFields(t *testing.T) {
	validClients := func() map[string]ClientConfig {
		return map[string]ClientConfig{
			clientDefaultName: {
				Username: "kopia",
				Hostname: "localhost",
				Password: "test",
			},
		}
	}

	tests := []struct {
		name        string
		mutate      func(*Config)
		expectedErr string
	}{
		{
			name:        "missing repositoryURL",
			mutate:      func(c *Config) { c.Kopia.APIServer.RepositoryURL = "" },
			expectedErr: "kopia.apiserver.repositoryURL is not set",
		},
		{
			name:        "missing fingerprint",
			mutate:      func(c *Config) { c.Kopia.APIServer.Fingerprint = "" },
			expectedErr: "kopia.apiserver.fingerprint is not set",
		},
		{
			name:        "missing clients",
			mutate:      func(c *Config) { c.Kopia.Clients = map[string]ClientConfig{} },
			expectedErr: "kopia.clients is empty",
		},
		{
			name: "missing username",
			mutate: func(c *Config) {
				client := c.Kopia.Clients[clientDefaultName]
				client.Username = ""
				c.Kopia.Clients[clientDefaultName] = client
			},
			expectedErr: "kopia.clients.default.username is not set",
		},
		{
			name: "missing hostname",
			mutate: func(c *Config) {
				client := c.Kopia.Clients[clientDefaultName]
				client.Hostname = ""
				c.Kopia.Clients[clientDefaultName] = client
			},
			expectedErr: "kopia.clients.default.hostname is not set",
		},
		{
			name: "missing password",
			mutate: func(c *Config) {
				client := c.Kopia.Clients[clientDefaultName]
				client.Password = ""
				c.Kopia.Clients[clientDefaultName] = client
			},
			expectedErr: "kopia.clients.default.password is not set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origCfg := Cfg
			defer func() { Cfg = origCfg }()

			Cfg = Config{
				Kopia: KopiaConfig{
					APIServer: APIServerConfig{
						RepositoryURL: "https://example.com:51515",
						Fingerprint:   testRevision,
					},
					Clients: validClients(),
				},
			}
			tt.mutate(&Cfg)

			err := CheckConfig(nil)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

// Tests missing file scenarios.
func TestMissingConfigFile(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T) error
	}{
		{
			name: "readConfig",
			run: func(t *testing.T) error {
				k := koanf.New(".")
				_ = k
				return readConfig("/nonexistent/config.yaml", nil, loadDefaultConfig(t))
			},
		},
		{
			name: "New",
			run: func(t *testing.T) error {
				return New("test", []string{"--config", "/nonexistent/file.yaml"}, loadDefaultConfig(t))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run(t)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "failed to read configuration file")
		})
	}
}

// Calls New with an unparseable embedded default config and expects an
// error from the placeholder check.
func TestNew_BrokenDefaultConfig(t *testing.T) {
	cfgFile := writeTestConfig(t, `kopia:
  apiserver:
    repositoryURL: "https://example.com:51515"
    fingerprint: "abc123"
  clients:
    default:
      username: "myuser"
      hostname: "myhost"
      password: "test"
`)
	broken := []byte("{{{{not valid yaml")
	err := New("test", []string{"--config", cfgFile}, broken)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse default config for placeholder check")
}

// Calls New with a valid config file and expects success and the default
// exporter port to be applied.
func TestNew_ValidConfig(t *testing.T) {
	cfgFile := writeTestConfig(t, `kopia:
  apiserver:
    repositoryURL: "https://example.com:51515"
    fingerprint: "abc123"
  clients:
    default:
      username: "myuser"
      hostname: "myhost"
      password: "test"
`)
	err := New("test", []string{"--config", cfgFile}, loadDefaultConfig(t))
	assert.NoError(t, err)
	assert.Equal(t, 9090, Cfg.Exporter.Port)
}

// Calls New with an include path wrapped in angle brackets and expects
// it to be accepted (not treated as a placeholder).
func TestNew_FilterIncludeWithAngleBrackets(t *testing.T) {
	cfgFile := writeTestConfig(t, `kopia:
  apiserver:
    repositoryURL: "https://example.com:51515"
    fingerprint: "abc123"
  clients:
    default:
      username: "myuser"
      hostname: "myhost"
      password: "test"
filters:
  include:
    path:
      - "<ok>"
`)
	err := New("test", []string{"--config", cfgFile}, loadDefaultConfig(t))
	assert.NoError(t, err)
	assert.Equal(t, []string{"<ok>"}, Cfg.Filters.Include.Path)
}

// Calls New with --version and expects flag.ErrHelp to be returned.
func TestNew_VersionFlag(t *testing.T) {
	err := New("test", []string{"--version"}, loadDefaultConfig(t))
	assert.Equal(t, flag.ErrHelp, err)
}

// Calls New with --help and expects flag.ErrHelp to be returned.
func TestNew_HelpFlag(t *testing.T) {
	err := New("test", []string{"--help"}, loadDefaultConfig(t))
	assert.Equal(t, flag.ErrHelp, err)
}

func koanfNew(t *testing.T, cfgFile string) *koanf.Koanf {
	t.Helper()
	k = koanf.New(".")
	err := k.Load(file.Provider(cfgFile), yaml.Parser())
	require.NoError(t, err)
	return k
}

// Gets version info with build info unavailable and expects revision and
// time to be empty.
func TestGetVersionInfo_BuildInfoUnavailable(t *testing.T) {
	origReadBuildInfo := ReadBuildInfo
	defer func() { ReadBuildInfo = origReadBuildInfo }()

	givenVersion = "2.0.0"
	ReadBuildInfo = func() (*debug.BuildInfo, bool) {
		return nil, false
	}

	vi := GetVersionInfo()
	assert.Equal(t, "2.0.0", vi.Version)
	assert.Empty(t, vi.Revision)
	assert.Empty(t, vi.Time)
}

// Gets version info with VCS settings and expects version, revision, and
// time to match the build info.
func TestGetVersionInfo_WithVCSSettings(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		revision string
		time     string
	}{
		{
			name:     "with vcs settings",
			version:  "3.0.0",
			revision: "deadbeef",
			time:     "2025-06-01T12:00:00Z",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origReadBuildInfo := ReadBuildInfo
			defer func() { ReadBuildInfo = origReadBuildInfo }()

			givenVersion = tc.version
			ReadBuildInfo = func() (*debug.BuildInfo, bool) {
				return &debug.BuildInfo{
					Settings: []debug.BuildSetting{
						{Key: vcsRevision, Value: tc.revision},
						{Key: vcsTime, Value: tc.time},
					},
				}, true
			}

			vi := GetVersionInfo()
			assert.Equal(t, tc.version, vi.Version)
			assert.Equal(t, tc.revision, vi.Revision)
			assert.Equal(t, tc.time, vi.Time)
		})
	}
}

// Reads a kopia retentions list given as a map and expects it to be
// parsed as a single empty string entry.
func TestReadKopiaConfig_InvalidRetentions(t *testing.T) {
	cfgFile := writeTestConfig(t, "kopia:\n  retentionstoextract:\n    a:\n      b: c\n")
	k = koanfNew(t, cfgFile)

	l := slog.Default()
	cfg := readKopiaConfig(k, l)
	assert.Len(t, cfg.Retentions, 1)
	assert.Empty(t, cfg.Retentions[0])
}

// Loads the logger config from environment variables.
func TestReadLoggerConfig_EnvVarOverrides(t *testing.T) {
	tests := []struct {
		name  string
		env   map[string]string
		check func(t *testing.T, cfg LoggerConfig)
	}{
		{
			name: "log level",
			env:  map[string]string{"KGE_LOGGER_LOG_LEVEL": "debug"},
			check: func(t *testing.T, cfg LoggerConfig) {
				assert.Equal(t, "debug", cfg.Level)
			},
		},
		{
			name: "redact sensitive false",
			env:  map[string]string{"KGE_LOGGER_REDACT_SENSITIVE": "false"},
			check: func(t *testing.T, cfg LoggerConfig) {
				assert.False(t, cfg.RedactSensitive)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k = koanf.New(".")
			for ek, ev := range tt.env {
				t.Setenv(ek, ev)
			}
			loadConfigLayer(k, env.Provider("KGE_", ".", kgeKeyMapper), "Failed to load environment variable overrides")
			cfg := readLoggerConfig(k)
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

// Loads a config with logger.redact_sensitive set to false and expects
// readConfig (and its logConfig call) to run the non-redacting branch
// without error.
func TestLogConfig_NoRedact(t *testing.T) {
	tmpFile := writeTestConfig(t, `kopia:
  apiserver:
    repositoryURL: "https://example.com:51515"
    fingerprint: abc123
  clients:
    default:
      username: myuser
      hostname: myhost
      password: secret
logger:
  redact_sensitive: false
`)

	err := readConfig(tmpFile, nil, loadDefaultConfig(t))
	require.NoError(t, err)
	assert.False(t, Cfg.Logger.RedactSensitive)
}
