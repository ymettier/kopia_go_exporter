// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	"flag"
	"log/slog"
	"os"
	"runtime/debug"
	"testing"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			args:    []string{"--config", "custom.yaml"},
			wantErr: false,
		},
		{
			name:    "custom exporter port",
			args:    []string{"--exporter-port", "9090"},
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
			flags, err := ParseFlags("test", tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFlags() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErrHelp && err != flag.ErrHelp {
				t.Errorf("ParseFlags() error = %v, wantErrHelp flag.ErrHelp", err)
			}
			if !tt.wantErr {
				if flags.ConfigFile == "" {
					t.Errorf("ParseFlags() ConfigFile should not be empty")
				}
			}
		})
	}
}

func TestParseFlags_CustomValues(t *testing.T) {
	flags, err := ParseFlags("test", []string{"--config", "/tmp/custom.yaml", "--exporter-port", "8080", "--log_level", "warn"})
	require.NoError(t, err)
	assert.Equal(t, "/tmp/custom.yaml", flags.ConfigFile)
	assert.Equal(t, 8080, flags.ExporterPort)
	assert.Equal(t, "warn", flags.LogLevel)
}

func TestGetVersionInfo(t *testing.T) {
	givenVersion = "testVersion"
	vi := GetVersionInfo()
	assert.Equal(t, "testVersion", vi.Version)
	assert.NotEmpty(t, vi.GoVersion)
}

func TestGetVersionInfo_ReturnsVCSData(t *testing.T) {
	givenVersion = "1.0.0"
	vi := GetVersionInfo()
	assert.Equal(t, "1.0.0", vi.Version)
}

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	tmpFile := t.TempDir() + "/test.yaml"
	err := os.WriteFile(tmpFile, []byte(content), 0o644)
	require.NoError(t, err)
	return tmpFile
}

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

func TestLookupConfigKey_UnderscoreFormat(t *testing.T) {
	cfgFile := writeTestConfig(t, "exporter_metrics_prefix: underscore_prefix\n")
	k = koanfNew(t, cfgFile)

	val, ok := lookupConfigKey(k, "exporter.metrics.prefix")
	assert.True(t, ok)
	assert.Equal(t, "underscore_prefix", val)
}

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

func TestReadExporterConfig(t *testing.T) {
	cfgFile := writeTestConfig(t, "exporter:\n  port: 8080\n  metrics:\n    prefix: custom_prefix\n  interval: 60\n")
	k = koanfNew(t, cfgFile)

	l := slog.Default()
	flags := CLIFlags{ExporterPort: 9090}

	cfg := readExporterConfig(k, l, flags)
	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, "custom_prefix", cfg.Metrics.Prefix)
	assert.Equal(t, 60, cfg.Interval)
}

func TestReadExporterConfig_FlagOverride(t *testing.T) {
	cfgFile := writeTestConfig(t, "exporter:\n  port: 8080\n")
	k = koanfNew(t, cfgFile)

	l := slog.Default()
	flags := CLIFlags{ExporterPort: 7777}

	cfg := readExporterConfig(k, l, flags)
	assert.Equal(t, 7777, cfg.Port)
}

func TestReadExporterConfig_Defaults(t *testing.T) {
	k = koanf.New(".")
	l := slog.Default()
	flags := CLIFlags{ExporterPort: 9090}

	cfg := readExporterConfig(k, l, flags)
	assert.Equal(t, 9090, cfg.Port)
	assert.Equal(t, "kopia_go_exporter", cfg.Metrics.Prefix)
	assert.Equal(t, 300, cfg.Interval)
}

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

func TestReadConfig_MissingFile(t *testing.T) {
	k = koanf.New(".")
	flags := CLIFlags{
		ConfigFile:   "/nonexistent/config.yaml",
		ExporterPort: 9090,
		LogLevel:     "info",
	}
	err := readConfig("/nonexistent/config.yaml", flags)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read configuration file")
}

func TestReadConfig_EnvOverride(t *testing.T) {
	tmpFile := writeTestConfig(t, "exporter:\n  port: 8080\n")

	t.Setenv("KGE_EXPORTER_PORT", "7777")

	k = koanf.New(".")
	flags := CLIFlags{
		ConfigFile:   tmpFile,
		ExporterPort: 9090,
		LogLevel:     "info",
	}
	err := readConfig(tmpFile, flags)
	require.NoError(t, err)
	assert.Equal(t, 7777, Cfg.Exporter.Port)
}

func TestCheckConfig_ValidConfig(t *testing.T) {
	origCfg := Cfg
	defer func() { Cfg = origCfg }()

	Cfg = Config{
		Kopia: KopiaConfig{
			Password: "test",
			APIServer: APIServerConfig{
				RepositoryURL: "https://example.com:51515",
				Hostname:      "localhost",
				Username:      "kopia",
				Fingerprint:   "abc123",
			},
		},
	}
	assert.NoError(t, CheckConfig())
}

func TestCheckConfig_MissingPassword(t *testing.T) {
	origCfg := Cfg
	defer func() { Cfg = origCfg }()

	Cfg = Config{
		Kopia: KopiaConfig{
			Password: "",
			APIServer: APIServerConfig{
				RepositoryURL: "https://example.com:51515",
				Hostname:      "localhost",
				Username:      "kopia",
				Fingerprint:   "abc123",
			},
		},
	}
	err := CheckConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "kopia.password is not set")
}

func TestCheckConfig_MissingRepositoryURL(t *testing.T) {
	origCfg := Cfg
	defer func() { Cfg = origCfg }()

	Cfg = Config{
		Kopia: KopiaConfig{
			Password: "test",
			APIServer: APIServerConfig{
				Hostname:    "localhost",
				Username:    "kopia",
				Fingerprint: "abc123",
			},
		},
	}
	err := CheckConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "kopia.repositoryURL is not set")
}

func TestCheckConfig_MissingFingerprint(t *testing.T) {
	origCfg := Cfg
	defer func() { Cfg = origCfg }()

	Cfg = Config{
		Kopia: KopiaConfig{
			Password: "test",
			APIServer: APIServerConfig{
				RepositoryURL: "https://example.com:51515",
				Hostname:      "localhost",
				Username:      "kopia",
			},
		},
	}
	err := CheckConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "kopia.fingerprint is not set")
}

func TestCheckConfig_MissingHostname(t *testing.T) {
	origCfg := Cfg
	defer func() { Cfg = origCfg }()

	Cfg = Config{
		Kopia: KopiaConfig{
			Password: "test",
			APIServer: APIServerConfig{
				RepositoryURL: "https://example.com:51515",
				Username:      "kopia",
				Fingerprint:   "abc123",
			},
		},
	}
	err := CheckConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "kopia.hostname is not set")
}

func TestCheckConfig_MissingUsername(t *testing.T) {
	origCfg := Cfg
	defer func() { Cfg = origCfg }()

	Cfg = Config{
		Kopia: KopiaConfig{
			Password: "test",
			APIServer: APIServerConfig{
				RepositoryURL: "https://example.com:51515",
				Fingerprint:   "abc123",
				Hostname:      "localhost",
			},
		},
	}
	err := CheckConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "kopia.username is not set")
}

func TestNew_MissingFile(t *testing.T) {
	err := New("test", []string{"--config", "/nonexistent/file.yaml"})
	assert.Error(t, err)
}

func TestNew_ValidConfig(t *testing.T) {
	err := New("test", []string{"--config", "../config.yaml.sample"})
	assert.NoError(t, err)
	assert.Equal(t, 9090, Cfg.Exporter.Port)
}

func TestNew_VersionFlag(t *testing.T) {
	err := New("test", []string{"--version"})
	assert.Equal(t, flag.ErrHelp, err)
}

func TestNew_HelpFlag(t *testing.T) {
	err := New("test", []string{"--help"})
	assert.Equal(t, flag.ErrHelp, err)
}

func koanfNew(t *testing.T, cfgFile string) *koanf.Koanf {
	t.Helper()
	k = koanf.New(".")
	err := k.Load(file.Provider(cfgFile), yaml.Parser())
	require.NoError(t, err)
	return k
}

func TestVersionInfo_BuildInfoUnavailable(t *testing.T) {
	origReadBuildInfo := ReadBuildInfo
	defer func() { ReadBuildInfo = origReadBuildInfo }()

	ReadBuildInfo = func() (*debug.BuildInfo, bool) {
		return nil, false
	}

	output := versionInfo("1.0.0")
	assert.Contains(t, output, "1.0.0")
	assert.NotContains(t, output, "Revision")
}

func TestVersionInfo_WithVCSSettings(t *testing.T) {
	origReadBuildInfo := ReadBuildInfo
	defer func() { ReadBuildInfo = origReadBuildInfo }()

	ReadBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			GoVersion: "go1.25.0",
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abc123"},
				{Key: "vcs.time", Value: "2025-01-15T10:00:00Z"},
				{Key: "vcs.modified", Value: "true"},
			},
		}, true
	}

	output := versionInfo("1.0.0")
	assert.Contains(t, output, "abc123")
	assert.Contains(t, output, "true")
	assert.Contains(t, output, "go1.25.0")
}

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
	assert.False(t, vi.Dirty)
}

func TestGetVersionInfo_WithVCSSettings_Dirty(t *testing.T) {
	origReadBuildInfo := ReadBuildInfo
	defer func() { ReadBuildInfo = origReadBuildInfo }()

	givenVersion = "3.0.0"
	ReadBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "deadbeef"},
				{Key: "vcs.time", Value: "2025-06-01T12:00:00Z"},
				{Key: "vcs.modified", Value: "true"},
			},
		}, true
	}

	vi := GetVersionInfo()
	assert.Equal(t, "3.0.0", vi.Version)
	assert.Equal(t, "deadbeef", vi.Revision)
	assert.Equal(t, "2025-06-01T12:00:00Z", vi.Time)
	assert.True(t, vi.Dirty)
}

func TestGetVersionInfo_WithVCSSettings_Clean(t *testing.T) {
	origReadBuildInfo := ReadBuildInfo
	defer func() { ReadBuildInfo = origReadBuildInfo }()

	givenVersion = "4.0.0"
	ReadBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "face0ff"},
				{Key: "vcs.time", Value: "2025-07-01T08:00:00Z"},
				{Key: "vcs.modified", Value: "false"},
			},
		}, true
	}

	vi := GetVersionInfo()
	assert.Equal(t, "4.0.0", vi.Version)
	assert.Equal(t, "face0ff", vi.Revision)
	assert.Equal(t, "2025-07-01T08:00:00Z", vi.Time)
	assert.False(t, vi.Dirty)
}

func TestReadKopiaConfig_InvalidRetentions(t *testing.T) {
	cfgFile := writeTestConfig(t, "kopia:\n  retentionstoextract:\n    a:\n      b: c\n")
	k = koanfNew(t, cfgFile)

	l := slog.Default()
	cfg := readKopiaConfig(k, l)
	assert.Len(t, cfg.Retentions, 1)
	assert.Empty(t, cfg.Retentions[0])
}
