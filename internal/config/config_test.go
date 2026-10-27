// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	"errors"
	"flag"
	"os"
	"runtime/debug"
	"testing"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/providers/rawbytes"
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

// Parses default flags and expects their default values to match the
// embedded YAML defaults.
func TestParseFlags_DefaultsFromYAML(t *testing.T) {
	_, fs, err := ParseFlags("test", nil)
	require.NoError(t, err)
	assert.Equal(t, "9090", fs.Lookup("exporter-port").DefValue)
	assert.Equal(t, "debug", fs.Lookup("log_level").DefValue)
}

// Parses custom flag values and expects the returned config file path
// and parsed flags to match the provided arguments.
func TestParseFlags_CustomValues(t *testing.T) {
	configFile, _, err := ParseFlags("test", []string{"--config", "/tmp/custom.yaml", "--exporter-port", "8080", "--log_level", "warn"})
	require.NoError(t, err)
	assert.Equal(t, "/tmp/custom.yaml", configFile)
}

// Reads flag defaults from the embedded config and expects the port
// and log level to match the YAML defaults.
func TestReadFlagDefaults(t *testing.T) {
	port, logLevel, err := readFlagDefaults()
	require.NoError(t, err)
	assert.Equal(t, 9090, port)
	assert.Equal(t, "debug", logLevel)
}

// Reads flag defaults from a broken embedded config and expects an error.
func TestReadFlagDefaults_BrokenConfig(t *testing.T) {
	origDefaultConfig := defaultConfig
	defer func() { defaultConfig = origDefaultConfig }()

	defaultConfig = []byte("exporter:\n  port: not-a-number\n")
	_, _, err := readFlagDefaults()
	require.Error(t, err)
	assert.Contains(t, err.Error(), errInvalidPort)
}

// Reads flag defaults from an unparseable embedded config and expects an error.
func TestReadFlagDefaults_UnparseableConfig(t *testing.T) {
	origDefaultConfig := defaultConfig
	defer func() { defaultConfig = origDefaultConfig }()

	defaultConfig = []byte("{{{{not valid yaml")
	_, _, err := readFlagDefaults()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load embedded default configuration")
}

// Fails to read flag defaults from an invalid embedded config and
// expects ParseFlags to return the error.
func TestParseFlags_BrokenDefaultConfig(t *testing.T) {
	tests := []struct {
		name       string
		cfg        string
		wantErrSub string
	}{
		{
			name:       "invalid YAML",
			cfg:        "{{{{not valid yaml",
			wantErrSub: "failed to load embedded default configuration",
		},
		{
			name:       "invalid port",
			cfg:        "exporter:\n  port: not-a-number\n",
			wantErrSub: errInvalidPort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origDefaultConfig := defaultConfig
			defer func() { defaultConfig = origDefaultConfig }()

			defaultConfig = []byte(tt.cfg)

			_, _, err := ParseFlags("test", nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrSub)
		})
	}
}

// Calls New with no --config flag and expects an error because the
// embedded default config still contains placeholder values.
func TestNew_NoConfigFile(t *testing.T) {
	_, err := New("test", []string{})
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

	cfg, err := New("test", []string{})
	require.NoError(t, err)
	require.Len(t, cfg.Kopia.Clients, 1)
	assert.Equal(t, "secret", cfg.Kopia.Clients[clientDefaultName].Password)
}

const (
	testRevision  = "abc123"
	vcsRevision   = "vcs.revision"
	vcsTime       = "vcs.time"
	missingKey    = "missing.key"
	missingKeyMsg = "missing key returns zero value"

	testURL          = "https://example.com:51515"
	testURLWithCreds = "https://user:secret@example.com:51515"       //nolint:gosec // test fixture, not a real credential
	testRedactedURL  = "https://redacted:redacted@example.com:51515" //nolint:gosec // test fixture, not a real credential
	errInvalidPort   = "invalid integer for exporter.port"
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
			setup: func(_ string) {
				orig := ReadBuildInfo
				ReadBuildInfo = func() (*debug.BuildInfo, bool) {
					return &debug.BuildInfo{
						GoVersion: "go1.27.3",
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
			setup:   func(_ string) {},
			check: func(t *testing.T, vi VersionInfo, version string) {
				assert.Equal(t, version, vi.Version)
			},
		},
		{
			name:    "build info unavailable",
			version: "2.0.0",
			setup: func(_ string) {
				orig := ReadBuildInfo
				ReadBuildInfo = func() (*debug.BuildInfo, bool) {
					return nil, false
				}
				_ = orig
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
			setup: func(_ string) {
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
			vi := GetVersionInfo(tt.version)
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
	k := koanf.New(".")
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
	k := koanfNew(t, cfgFile)

	val, ok := lookupConfigKey(k, "exporter.metrics.prefix")
	assert.True(t, ok)
	assert.Equal(t, "underscore_prefix", val)
}

// Reads a string config value and expects the existing value, or an
// empty string when the key is missing.
func TestGetConfigString(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{
			name: "existing key",
			key:  "existing.key",
			want: "value",
		},
		{
			name: missingKeyMsg,
			key:  "missing.port",
			want: "",
		},
	}
	cfgFile := writeTestConfig(t, "existing:\n  key: value\n")
	k := koanfNew(t, cfgFile)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := getConfigString(k, tt.key)
			assert.Equal(t, tt.want, val)
		})
	}
}

// Reads an int config value and expects the parsed value, zero for a
// missing key, and an error for a non-numeric value.
func TestGetConfigInt(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		want    int
		wantErr bool
	}{
		{
			name: "valid int",
			key:  "port",
			want: 8080,
		},
		{
			name: missingKeyMsg,
			key:  missingKey,
			want: 0,
		},
		{
			name:    "invalid int returns error",
			key:     "invalid",
			wantErr: true,
		},
	}
	cfgFile := writeTestConfig(t, "port: 8080\ninvalid: not_a_number\n")
	k := koanfNew(t, cfgFile)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := getConfigInt(k, tt.key)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid integer for invalid")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, val)
		})
	}
}

// Reads a bool config value and expects true/false for
// "true"/"false"/"1", false for a missing key, and an error for an
// invalid value.
func TestGetConfigBool(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		want    bool
		wantErr bool
	}{
		{
			name: "true string",
			key:  "enabled",
			want: true,
		},
		{
			name: "false string",
			key:  "disabled",
			want: false,
		},
		{
			name: "1 string",
			key:  "one",
			want: true,
		},
		{
			name: missingKeyMsg,
			key:  missingKey,
			want: false,
		},
		{
			name:    "invalid bool returns error",
			key:     "invalid_bool",
			wantErr: true,
		},
	}
	cfgFile := writeTestConfig(t, "enabled: true\ndisabled: \"false\"\none: \"1\"\ninvalid_bool: maybe\n")
	k := koanfNew(t, cfgFile)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := getConfigBool(k, tt.key)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid boolean for invalid_bool")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, val)
		})
	}
}

// Reads the exporter config section and expects port, metrics prefix,
// and interval to match the YAML.
func TestReadExporterConfig(t *testing.T) {
	cfgFile := writeTestConfig(t, "exporter:\n  port: 8080\n  metrics:\n    prefix: custom_prefix\n  interval: 60\n")
	k := koanfNew(t, cfgFile)

	cfg, err := readExporterConfig(k)
	require.NoError(t, err)
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

	k := koanfNew(t, cfgFile)
	require.NoError(t, k.Load(
		posflag.ProviderWithValue(fs, ".", k, flagKeyMapper), nil))

	cfg, err := readExporterConfig(k)
	require.NoError(t, err)
	assert.Equal(t, 7777, cfg.Port)
}

// Loads the embedded default config and expects the documented default
// port, prefix, and interval.
func TestReadExporterConfig_Defaults(t *testing.T) {
	k := koanf.New(".")
	require.NoError(t, k.Load(rawbytes.Provider(defaultConfig), yaml.Parser()))

	cfg, err := readExporterConfig(k)
	require.NoError(t, err)
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
			name:       "invalid include structure",
			cfg:        "filters:\n  include:\n    path:\n      key: value\n",
			wantErrSub: "invalid filters.include.path",
		},
		{
			name:       "invalid exclude structure",
			cfg:        "filters:\n  exclude:\n    path:\n      key: value\n",
			wantErrSub: "invalid filters.exclude.path",
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
			var k *koanf.Koanf
			if tt.useKoanf {
				k = koanf.New(".")
			} else {
				cfgFile := writeTestConfig(t, tt.cfg)
				k = koanfNew(t, cfgFile)
			}
			cfg, err := readFiltersConfig(k)
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
	k := koanfNew(t, cfgFile)

	_, err := readFiltersConfig(k)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid filters.include.path regex")
}

// Reads an invalid exclude filter regex and expects an error mentioning
// the exclude path regex.
func TestReadFiltersConfig_InvalidExcludeRegex(t *testing.T) {
	cfgFile := writeTestConfig(t, "filters:\n  exclude:\n    path:\n      - \"[\"\n")
	k := koanfNew(t, cfgFile)

	_, err := readFiltersConfig(k)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid filters.exclude.path regex")
}

// Loads a config with invalid filters via readConfig and expects a
// config error mentioning the include path regex.
func TestReadConfig_InvalidFilters(t *testing.T) {
	tmpFile := writeTestConfig(t, "filters:\n  include:\n    path:\n      - \"[\"\n")

	_, err := readConfig(tmpFile, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid filters.include.path regex")
}

// Loads configs with malformed values and expects readConfig to fail
// instead of silently using zero values or partial nested configs.
func TestReadConfig_InvalidValues(t *testing.T) {
	tests := []struct {
		name       string
		cfg        string
		wantErrSub []string
	}{
		{
			name:       "invalid exporter int",
			cfg:        "exporter:\n  port: not-a-number\n",
			wantErrSub: []string{errInvalidPort},
		},
		{
			name:       "multiple invalid exporter ints",
			cfg:        "exporter:\n  port: not-a-number\n  interval: also-bad\n",
			wantErrSub: []string{errInvalidPort, "invalid integer for exporter.interval"},
		},
		{
			name:       "invalid logger bool",
			cfg:        "logger:\n  json: maybe\n",
			wantErrSub: []string{"invalid boolean for logger.json"},
		},
		{
			name:       "invalid kopia clients",
			cfg:        "kopia:\n  clients: not-a-map\n",
			wantErrSub: []string{"invalid kopia.clients"},
		},
		{
			name:       "invalid kopia retentions",
			cfg:        "kopia:\n  retentionstoextract:\n    a:\n      b: c\n",
			wantErrSub: []string{"invalid kopia.retentionstoextract"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile := writeTestConfig(t, tt.cfg)

			_, err := readConfig(tmpFile, nil)
			require.Error(t, err)
			for _, wantErrSub := range tt.wantErrSub {
				assert.Contains(t, err.Error(), wantErrSub)
			}
		})
	}
}

// Reads filters with no values set and expects empty path lists and no
// compiled regexes.
func TestReadFiltersConfig_Defaults(t *testing.T) {
	k := koanf.New(".")

	cfg, err := readFiltersConfig(k)
	require.NoError(t, err)
	assert.Equal(t, []string{}, cfg.Include.Path)
	assert.Empty(t, cfg.Include.PathRegex)
	assert.Equal(t, []string{}, cfg.Exclude.Path)
	assert.Empty(t, cfg.Exclude.PathRegex)
}

// Loads a config whose include path is overridden by env vars and
// expects the env values to be used.
func TestReadFiltersConfig_EnvOverride(t *testing.T) {
	tmpFile := writeTestConfig(t, `kopia:
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
      - ignored
`)
	t.Setenv("KGE_FILTERS_INCLUDE_PATH", ".*")
	t.Setenv("KGE_FILTERS_EXCLUDE_PATH", "/tmp/.*")

	cfg, err := readConfig(tmpFile, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{".*"}, cfg.Filters.Include.Path)
	assert.Len(t, cfg.Filters.Include.PathRegex, 1)
	assert.Equal(t, []string{"/tmp/.*"}, cfg.Filters.Exclude.Path)
	assert.Len(t, cfg.Filters.Exclude.PathRegex, 1)
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
	k := koanfNew(t, cfgFile)

	cfg, err := readKopiaConfig(k)
	require.NoError(t, err)
	assert.Equal(t, testURL, cfg.APIServer.RepositoryURL)
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
		name       string
		setup      func(t *testing.T) *koanf.Koanf
		wantErrSub string
		check      func(t *testing.T, cfg KopiaConfig)
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
			wantErrSub: "invalid kopia.clients",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := tt.setup(t)
			cfg, err := readKopiaConfig(k)
			if tt.wantErrSub != "" {
				require.Error(t, err)
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

// Loads a nonexistent config file and expects an error mentioning the
// failure to read the configuration file.
func TestReadConfig_MissingFile(t *testing.T) {
	_, err := readConfig("/nonexistent/config.yaml", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read configuration file")
}

// Loads a config whose exporter port is overridden by an env var and
// expects the env value to be used.
func TestReadConfig_EnvOverride(t *testing.T) {
	tmpFile := writeTestConfig(t, `kopia:
  apiserver:
    repositoryURL: "https://example.com:51515"
    fingerprint: "abc123"
  clients:
    default:
      username: "myuser"
      hostname: "myhost"
      password: "test"
exporter:
  port: 8080
`)

	t.Setenv("KGE_EXPORTER_PORT", "7777")

	cfg, err := readConfig(tmpFile, nil)
	require.NoError(t, err)
	assert.Equal(t, 7777, cfg.Exporter.Port)
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
	cfg := validCheckConfig()
	assert.NoError(t, CheckConfig(&cfg))
}

// Builds a config that passes all CheckConfig validations.
func validCheckConfig() Config {
	cfg := Config{
		Exporter: ExporterConfig{
			Port:     9090,
			Interval: 300,
		},
		Kopia: KopiaConfig{
			APIServer: APIServerConfig{
				RepositoryURL: testURL,
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
	cfg.Exporter.Metrics.Prefix = "kopia_go_exporter"
	return cfg
}

// Checks invalid exporter values and expects each to be rejected.
func TestCheckConfig_ExporterValidation(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*Config)
		expectedErr string
	}{
		{
			name:        "port zero",
			mutate:      func(c *Config) { c.Exporter.Port = 0 },
			expectedErr: "exporter.port must be between 1 and 65535, got 0",
		},
		{
			name:        "port too high",
			mutate:      func(c *Config) { c.Exporter.Port = 65536 },
			expectedErr: "exporter.port must be between 1 and 65535, got 65536",
		},
		{
			name:        "interval zero",
			mutate:      func(c *Config) { c.Exporter.Interval = 0 },
			expectedErr: "exporter.interval must be positive, got 0",
		},
		{
			name:        "negative interval",
			mutate:      func(c *Config) { c.Exporter.Interval = -1 },
			expectedErr: "exporter.interval must be positive, got -1",
		},
		{
			name:        "invalid metrics prefix",
			mutate:      func(c *Config) { c.Exporter.Metrics.Prefix = "bad-prefix" },
			expectedErr: `exporter.metrics.prefix "bad-prefix" is not a valid Prometheus metric name`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validCheckConfig()
			tt.mutate(&cfg)

			err := CheckConfig(&cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
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
			cfg := validCheckConfig()
			cfg.Kopia.Clients = validClients()
			tt.mutate(&cfg)

			err := CheckConfig(&cfg)
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
			run: func(_ *testing.T) error {
				_, err := readConfig("/nonexistent/config.yaml", nil)
				return err
			},
		},
		{
			name: "New",
			run: func(_ *testing.T) error {
				_, err := New("test", []string{"--config", "/nonexistent/file.yaml"})
				return err
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

// Calls the placeholder check with an unparseable default config and
// expects an error.
func TestCheckPlaceholders_BrokenDefaultConfig(t *testing.T) {
	origDefaultConfig := defaultConfig
	defer func() { defaultConfig = origDefaultConfig }()

	defaultConfig = []byte("{{{{not valid yaml")
	err := checkPlaceholders(koanf.New("."))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse default config for placeholder check")
}

// Calls the placeholder check with no embedded defaults and expects no
// error.
func TestCheckPlaceholders_EmptyDefaultConfig(t *testing.T) {
	origDefaultConfig := defaultConfig
	defer func() { defaultConfig = origDefaultConfig }()

	defaultConfig = nil
	err := checkPlaceholders(koanf.New("."))
	require.NoError(t, err)
}

// Loads an unparseable default config and expects readConfig to fail.
func TestReadConfig_BrokenDefaultConfig(t *testing.T) {
	origDefaultConfig := defaultConfig
	defer func() { defaultConfig = origDefaultConfig }()

	cfgFile := writeTestConfig(t, "exporter:\n  port: 8080\n")
	defaultConfig = []byte("{{{{not valid yaml")

	_, err := readConfig(cfgFile, nil)
	require.Error(t, err)
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
	cfg, err := New("test", []string{"--config", cfgFile})
	require.NoError(t, err)
	assert.Equal(t, 9090, cfg.Exporter.Port)
}

// Calls New with a config that loads but fails validation and expects
// the validation error.
func TestNew_CheckConfigError(t *testing.T) {
	cfgFile := writeTestConfig(t, `kopia:
  apiserver:
    repositoryURL: "https://example.com:51515"
    fingerprint: ""
  clients:
    default:
      username: "myuser"
      hostname: "myhost"
      password: "test"
`)
	_, err := New("test", []string{"--config", cfgFile})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kopia.apiserver.fingerprint is not set")
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
	cfg, err := New("test", []string{"--config", cfgFile})
	require.NoError(t, err)
	assert.Equal(t, []string{"<ok>"}, cfg.Filters.Include.Path)
}

// Calls New with --version and expects flag.ErrHelp to be returned.
func TestNew_VersionFlag(t *testing.T) {
	_, err := New("test", []string{"--version"})
	assert.Equal(t, flag.ErrHelp, err)
}

// Calls New with --help and expects flag.ErrHelp to be returned.
func TestNew_HelpFlag(t *testing.T) {
	_, err := New("test", []string{"--help"})
	assert.Equal(t, flag.ErrHelp, err)
}

func koanfNew(t *testing.T, cfgFile string) *koanf.Koanf {
	t.Helper()
	k := koanf.New(".")
	err := k.Load(file.Provider(cfgFile), yaml.Parser())
	require.NoError(t, err)
	return k
}

// Gets version info with build info unavailable and expects revision and
// time to be empty.
func TestGetVersionInfo_BuildInfoUnavailable(t *testing.T) {
	origReadBuildInfo := ReadBuildInfo
	defer func() { ReadBuildInfo = origReadBuildInfo }()

	ReadBuildInfo = func() (*debug.BuildInfo, bool) {
		return nil, false
	}

	vi := GetVersionInfo("2.0.0")
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

			ReadBuildInfo = func() (*debug.BuildInfo, bool) {
				return &debug.BuildInfo{
					Settings: []debug.BuildSetting{
						{Key: vcsRevision, Value: tc.revision},
						{Key: vcsTime, Value: tc.time},
					},
				}, true
			}

			vi := GetVersionInfo(tc.version)
			assert.Equal(t, tc.version, vi.Version)
			assert.Equal(t, tc.revision, vi.Revision)
			assert.Equal(t, tc.time, vi.Time)
		})
	}
}

// Reads a kopia retentions list given as a map and expects an error.
func TestReadKopiaConfig_InvalidRetentions(t *testing.T) {
	cfgFile := writeTestConfig(t, "kopia:\n  retentionstoextract:\n    a:\n      b: c\n")
	k := koanfNew(t, cfgFile)

	_, err := readKopiaConfig(k)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid kopia.retentionstoextract")
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
		{
			name: "log file max size",
			env:  map[string]string{"KGE_LOGGER_LOG_FILE_MAXSIZE": "500"},
			check: func(t *testing.T, cfg LoggerConfig) {
				assert.Equal(t, 500, cfg.LogFile.MaxSize)
			},
		},
		{
			name: "log file max backups",
			env:  map[string]string{"KGE_LOGGER_LOG_FILE_MAXBACKUPS": "7"},
			check: func(t *testing.T, cfg LoggerConfig) {
				assert.Equal(t, 7, cfg.LogFile.MaxBackups)
			},
		},
		{
			name: "log file max age",
			env:  map[string]string{"KGE_LOGGER_LOG_FILE_MAXAGE": "30"},
			check: func(t *testing.T, cfg LoggerConfig) {
				assert.Equal(t, 30, cfg.LogFile.MaxAge)
			},
		},
		{
			name: "log file compress",
			env:  map[string]string{"KGE_LOGGER_LOG_FILE_COMPRESS": "true"},
			check: func(t *testing.T, cfg LoggerConfig) {
				assert.True(t, cfg.LogFile.Compress)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := koanf.New(".")
			require.NoError(t, k.Load(rawbytes.Provider(defaultConfig), yaml.Parser()))
			for ek, ev := range tt.env {
				t.Setenv(ek, ev)
			}
			loadConfigLayer(k, env.Provider("KGE_", ".", func(name string) string {
				return kgeKeyMapper(k, name)
			}), "Failed to load environment variable overrides")
			cfg, err := readLoggerConfig(k)
			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

// Redacts URL credentials only when requested and handles invalid URLs.
func TestRedactURL(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
		redact bool
		want   string
	}{
		{
			name:   "redaction disabled",
			rawURL: testURLWithCreds,
			want:   testURLWithCreds,
		},
		{
			name:   "credentials removed",
			rawURL: testURLWithCreds,
			redact: true,
			want:   testRedactedURL,
		},
		{
			name:   "no credentials",
			rawURL: "https://example.com:51515",
			redact: true,
			want:   "https://example.com:51515",
		},
		{
			name:   "invalid URL",
			rawURL: "https://user:secret@[::1",
			redact: true,
			want:   "****",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, redactURL(tt.rawURL, tt.redact))
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

	cfg, err := readConfig(tmpFile, nil)
	require.NoError(t, err)
	assert.False(t, cfg.Logger.RedactSensitive)
}
