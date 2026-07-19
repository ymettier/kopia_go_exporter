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
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFlags() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErrHelp && err != flag.ErrHelp {
				t.Errorf("ParseFlags() error = %v, wantErrHelp flag.ErrHelp", err)
			}
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

// Calls New with no --config and only environment variables set, and
// expects success with the password read from the env vars.
func TestNew_NoConfigFile_WithEnv(t *testing.T) {
	// No --config flag but all required values via env vars
	t.Setenv("KGE_KOPIA_PASSWORD", "secret")
	t.Setenv("KGE_KOPIA_APISERVER_REPOSITORYURL", "https://some.url:port")
	t.Setenv("KGE_KOPIA_APISERVER_FINGERPRINT", "abc123")
	t.Setenv("KGE_KOPIA_APISERVER_HOSTNAME", "myhost")
	t.Setenv("KGE_KOPIA_APISERVER_USERNAME", "myuser")

	err := New("test", []string{}, loadDefaultConfig(t))
	assert.NoError(t, err)
	assert.Equal(t, "secret", Cfg.Kopia.Password)
}

// Builds version info and expects the given version and a non-empty Go
// version.
func TestGetVersionInfo(t *testing.T) {
	givenVersion = "testVersion"
	vi := GetVersionInfo()
	assert.Equal(t, "testVersion", vi.Version)
	assert.NotEmpty(t, vi.GoVersion)
}

// Builds version info with build info unavailable and expects the
// version to fall back to the given value.
func TestGetVersionInfo_ReturnsVCSData(t *testing.T) {
	givenVersion = "1.0.0" //nolint:goconst
	vi := GetVersionInfo()
	assert.Equal(t, "1.0.0", vi.Version)
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
	cfgFile := writeTestConfig(t, "exporter:\n  port: 9090\n  metrics:\n    prefix: test_prefix\n")
	k = koanf.New(".")
	err := k.Load(file.Provider(cfgFile), yaml.Parser())
	require.NoError(t, err)

	t.Run("lowercase key", func(t *testing.T) {
		val, ok := lookupConfigKey(k, "exporter.port")
		assert.True(t, ok)
		assert.Equal(t, "9090", val)
	})

	t.Run("camelCase key", func(t *testing.T) {
		val, ok := lookupConfigKey(k, "exporter.metrics.prefix")
		assert.True(t, ok)
		assert.Equal(t, "test_prefix", val)
	})

	t.Run("nonexistent key", func(t *testing.T) {
		_, ok := lookupConfigKey(k, "nonexistent.key")
		assert.False(t, ok)
	})
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
	cfgFile := writeTestConfig(t, "existing:\n  key: value\n")
	k = koanfNew(t, cfgFile)

	t.Run("existing key", func(t *testing.T) {
		val := getConfigString(k, "existing.key", "default")
		assert.Equal(t, "value", val)
	})

	t.Run("missing key returns default", func(t *testing.T) {
		val := getConfigString(k, "missing.key", "default_value")
		assert.Equal(t, "default_value", val)
	})
}

// Reads an int config value and expects the parsed value, or the default
// for a missing key or a non-numeric value.
func TestGetConfigInt(t *testing.T) {
	cfgFile := writeTestConfig(t, "port: 8080\ninvalid: not_a_number\n")
	k = koanfNew(t, cfgFile)

	t.Run("valid int", func(t *testing.T) {
		val := getConfigInt(k, "port", 9090)
		assert.Equal(t, 8080, val)
	})

	t.Run("missing key returns default", func(t *testing.T) {
		val := getConfigInt(k, "missing.port", 9090)
		assert.Equal(t, 9090, val)
	})

	t.Run("invalid int returns default", func(t *testing.T) {
		val := getConfigInt(k, "invalid", 9090)
		assert.Equal(t, 9090, val)
	})
}

// Reads a bool config value and expects true/false for
// "true"/"false"/"1", and the default for missing or invalid values.
func TestGetConfigBool(t *testing.T) {
	cfgFile := writeTestConfig(t, "enabled: true\ndisabled: \"false\"\none: \"1\"\ninvalid_bool: maybe\n")
	k = koanfNew(t, cfgFile)

	t.Run("true string", func(t *testing.T) {
		val := getConfigBool(k, "enabled", false)
		assert.True(t, val)
	})

	t.Run("false string", func(t *testing.T) {
		val := getConfigBool(k, "disabled", true)
		assert.False(t, val)
	})

	t.Run("1 string", func(t *testing.T) {
		val := getConfigBool(k, "one", false)
		assert.True(t, val)
	})

	t.Run("missing key returns default", func(t *testing.T) {
		val := getConfigBool(k, "missing.key", true)
		assert.True(t, val)
	})

	t.Run("invalid bool returns default", func(t *testing.T) {
		val := getConfigBool(k, "invalid_bool", false)
		assert.False(t, val)
	})
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
	cfgFile := writeTestConfig(t, `filters:
  include:
    path:
      - ".*"
  exclude:
    path:
      - "/tmp/.*"
`)
	k = koanfNew(t, cfgFile)

	l := slog.Default()
	cfg, err := readFiltersConfig(k, l)
	require.NoError(t, err)
	assert.Equal(t, []string{".*"}, cfg.Include.Path)
	assert.Len(t, cfg.Include.PathRegex, 1)
	assert.Equal(t, []string{"/tmp/.*"}, cfg.Exclude.Path)
	assert.Len(t, cfg.Exclude.PathRegex, 1)
}

// Reads an invalid include filter regex and expects an error mentioning
// the include path regex.
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

// Loads a config with an invalid include regex via readConfig and
// expects a config error mentioning the include path regex.
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

// Reads the kopia config section and expects password, apiserver fields,
// and retentions to match the YAML.
func TestReadKopiaConfig(t *testing.T) {
	cfgFile := writeTestConfig(t, `kopia:
  password: secret
  apiserver:
    repositoryURL: "https://example.com:51515"
    hostname: myhost
    username: myuser
    fingerprint: abc123
  retentionstoextract:
    - daily
    - weekly
`)
	k = koanfNew(t, cfgFile)

	l := slog.Default()
	cfg := readKopiaConfig(k, l)
	assert.Equal(t, "secret", cfg.Password)
	assert.Equal(t, "https://example.com:51515", cfg.APIServer.RepositoryURL)
	assert.Equal(t, "myhost", cfg.APIServer.Hostname)
	assert.Equal(t, "myuser", cfg.APIServer.Username)
	assert.Equal(t, "abc123", cfg.APIServer.Fingerprint)
	assert.Equal(t, []string{"daily", "weekly"}, cfg.Retentions)
}

// Reads the kopia config with no values set and expects all fields to be
// empty and retentions empty.
func TestReadKopiaConfig_Defaults(t *testing.T) {
	k = koanf.New(".")
	l := slog.Default()

	cfg := readKopiaConfig(k, l)
	assert.Equal(t, "", cfg.Password)
	assert.Equal(t, "", cfg.APIServer.RepositoryURL)
	assert.Equal(t, "", cfg.APIServer.Hostname)
	assert.Equal(t, "", cfg.APIServer.Username)
	assert.Equal(t, "", cfg.APIServer.Fingerprint)
	assert.Equal(t, []string{}, cfg.Retentions)
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
	k := koanf.New(".")
	loadConfigLayer(k, stubConfigProvider{}, "failed to load stub")
	assert.Equal(t, 9090, k.Int("exporter.port"))

	k2 := koanf.New(".")
	loadConfigLayer(k2, stubConfigProvider{err: errors.New("boom")}, "failed to load stub")
}

// Checks a fully populated config and expects no validation error.
func TestCheckConfig_ValidConfig(t *testing.T) {
	origCfg := Cfg
	defer func() { Cfg = origCfg }()

	k = koanf.New(".")
	Cfg = Config{
		Kopia: KopiaConfig{
			Password: "test",
			APIServer: APIServerConfig{
				RepositoryURL: "https://example.com:51515",
				Hostname:      "localhost",
				Username:      "kopia",
				Fingerprint:   "abc123", //nolint:goconst
			},
		},
	}
	assert.NoError(t, CheckConfig(nil))
}

// TestCheckConfig_MissingFields verifies that CheckConfig reports an
// error when each required Kopia field is missing.
func TestCheckConfig_MissingFields(t *testing.T) {
	validAPIServer := func() APIServerConfig {
		return APIServerConfig{
			RepositoryURL: "https://example.com:51515",
			Hostname:      "localhost",
			Username:      "kopia",
			Fingerprint:   "abc123",
		}
	}

	tests := []struct {
		name        string
		mutate      func(*Config)
		expectedErr string
	}{
		{
			name:        "missing password",
			mutate:      func(c *Config) { c.Kopia.Password = "" },
			expectedErr: "kopia.password is not set",
		},
		{
			name: "missing repositoryURL",
			mutate: func(c *Config) {
				c.Kopia.APIServer.RepositoryURL = ""
			},
			expectedErr: "kopia.apiserver.repositoryURL is not set",
		},
		{
			name: "missing fingerprint",
			mutate: func(c *Config) {
				c.Kopia.APIServer.Fingerprint = ""
			},
			expectedErr: "kopia.apiserver.fingerprint is not set",
		},
		{
			name: "missing hostname",
			mutate: func(c *Config) {
				c.Kopia.APIServer.Hostname = ""
			},
			expectedErr: "kopia.apiserver.hostname is not set",
		},
		{
			name: "missing username",
			mutate: func(c *Config) {
				c.Kopia.APIServer.Username = ""
			},
			expectedErr: "kopia.apiserver.username is not set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origCfg := Cfg
			defer func() { Cfg = origCfg }()

			Cfg = Config{
				Kopia: KopiaConfig{
					Password:  "test",
					APIServer: validAPIServer(),
				},
			}
			tt.mutate(&Cfg)

			err := CheckConfig(nil)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

// Calls New with a nonexistent --config file and expects an error
// mentioning the failure to read the configuration file.
func TestNew_MissingFile(t *testing.T) {
	err := New("test", []string{"--config", "/nonexistent/file.yaml"}, loadDefaultConfig(t))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read configuration file")
}

// Calls New with an unparseable embedded default config and expects an
// error from the placeholder check.
func TestNew_BrokenDefaultConfig(t *testing.T) {
	cfgFile := writeTestConfig(t, `kopia:
  password: "test"
  apiserver:
    repositoryURL: "https://example.com:51515"
    hostname: "myhost"
    username: "myuser"
    fingerprint: "abc123"
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
  password: "test"
  apiserver:
    repositoryURL: "https://example.com:51515"
    hostname: "myhost"
    username: "myuser"
    fingerprint: "abc123"
`)
	err := New("test", []string{"--config", cfgFile}, loadDefaultConfig(t))
	assert.NoError(t, err)
	assert.Equal(t, 9090, Cfg.Exporter.Port)
}

// Calls New with an include path wrapped in angle brackets and expects
// it to be accepted (not treated as a placeholder).
func TestNew_FilterIncludeWithAngleBrackets(t *testing.T) {
	cfgFile := writeTestConfig(t, `kopia:
  password: "test"
  apiserver:
    repositoryURL: "https://example.com:51515"
    hostname: "myhost"
    username: "myuser"
    fingerprint: "abc123"
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

// Formats version info with build info unavailable and expects the given
// version present and the Go version absent.
func TestVersionInfo_BuildInfoUnavailable(t *testing.T) {
	origReadBuildInfo := ReadBuildInfo
	defer func() { ReadBuildInfo = origReadBuildInfo }()

	givenVersion = "1.0.0"
	ReadBuildInfo = func() (*debug.BuildInfo, bool) {
		return nil, false
	}

	output := formatVersionInfo()
	assert.Contains(t, output, "1.0.0")
	assert.NotContains(t, output, "go1.25.0")
}

// Formats version info with VCS settings and expects the revision and Go
// version to appear in the output.
func TestVersionInfo_WithVCSSettings(t *testing.T) {
	origReadBuildInfo := ReadBuildInfo
	defer func() { ReadBuildInfo = origReadBuildInfo }()

	givenVersion = "1.0.0"
	ReadBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			GoVersion: "go1.25.0",
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abc123"},           //nolint:goconst
				{Key: "vcs.time", Value: "2025-01-15T10:00:00Z"}, //nolint:goconst
			},
		}, true
	}

	output := formatVersionInfo()
	assert.Contains(t, output, "abc123")
	assert.Contains(t, output, "go1.25.0")
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
						{Key: "vcs.revision", Value: tc.revision},
						{Key: "vcs.time", Value: tc.time},
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

// Loads the logger config with KGE_LOGGER_LOG_LEVEL set and expects the
// level to come from the env var.
func TestReadLoggerConfig_EnvVarOverride(t *testing.T) {
	k = koanf.New(".")
	t.Setenv("KGE_LOGGER_LOG_LEVEL", "debug")

	loadConfigLayer(k, env.Provider("KGE_", ".", kgeKeyMapper), "Failed to load environment variable overrides")

	cfg := readLoggerConfig(k)
	assert.Equal(t, "debug", cfg.Level)
}

// Loads the logger config with KGE_LOGGER_REDACT_SENSITIVE=false and
// expects RedactSensitive to be false.
func TestReadLoggerConfig_EnvVarOverrideRedactSensitive(t *testing.T) {
	k = koanf.New(".")
	t.Setenv("KGE_LOGGER_REDACT_SENSITIVE", "false")

	loadConfigLayer(k, env.Provider("KGE_", ".", kgeKeyMapper), "Failed to load environment variable overrides")

	cfg := readLoggerConfig(k)
	assert.False(t, cfg.RedactSensitive)
}
