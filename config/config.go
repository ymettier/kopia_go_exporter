// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	"flag"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"

	"kopia-go-exporter/logger"
)

var k = koanf.New(".")

var givenVersion string

var ReadBuildInfo = debug.ReadBuildInfo

type ExporterConfig struct {
	Port    int
	Metrics struct {
		Prefix string
	}
	Interval int
}

type APIServerConfig struct {
	RepositoryURL string
	Hostname      string
	Username      string
	Fingerprint   string
}

type KopiaConfig struct {
	Password   string
	APIServer  APIServerConfig
	Retentions []string
}

type Config struct {
	Exporter ExporterConfig
	Kopia    KopiaConfig
	LogLevel string
}

var Cfg Config

// VersionInfo holds build version information extracted from Go's debug info.
type VersionInfo struct {
	Version   string
	Revision  string
	Time      string
	Dirty     bool
	GoVersion string
}

// GetVersionInfo returns build version, revision, and time from Go's debug info.
func GetVersionInfo() VersionInfo {
	info := VersionInfo{Version: givenVersion}

	buildInfo, ok := ReadBuildInfo()
	if !ok {
		return info
	}

	info.GoVersion = buildInfo.GoVersion
	for _, kv := range buildInfo.Settings {
		if kv.Value == "" {
			continue
		}
		switch kv.Key {
		case "vcs.revision": //nolint:goconst
			info.Revision = kv.Value
		case "vcs.time": //nolint:goconst
			info.Time = kv.Value
		case "vcs.modified": //nolint:goconst
			info.Dirty = kv.Value == "true" //nolint:goconst
		}
	}
	return info
}

// formatVersionInfo returns the formatted build version information.
func formatVersionInfo() string {
	vi := GetVersionInfo()

	output := fmt.Sprintf("%-15s: %s\n", "Version", vi.Version)
	output += fmt.Sprintf("%-15s: %s\n", "Revision", vi.Revision)
	output += fmt.Sprintf("%-15s: %v\n", "Dirty Build", vi.Dirty)
	output += fmt.Sprintf("%-15s: %s\n", "Last Commit", vi.Time)
	output += fmt.Sprintf("%-15s: %s\n", "Go Version", vi.GoVersion)
	return output
}

// ParseFlags parses command-line flags and returns the config file path and parsed flagset.
func ParseFlags(version string, args []string) (string, *pflag.FlagSet, error) {
	givenVersion = version

	fs := pflag.NewFlagSet("kopia-go-exporter", pflag.ContinueOnError)

	configFile := fs.StringP("config", "c", "config.yaml", "Path to YAML config file")
	fs.Int("exporter-port", 9090, "Exporter HTTP server port") //nolint:mnd
	fs.StringP("log_level", "l", "info", "Log level (debug, info, warn, error)")
	showVersion := fs.BoolP("version", "V", false, "Print version information and exit")
	showHelp := fs.BoolP("help", "h", false, "Print help")

	if err := fs.Parse(args); err != nil {
		return "", nil, err
	}

	if *showHelp {
		fs.PrintDefaults()
		return "", nil, flag.ErrHelp
	}

	if *showVersion {
		fmt.Print(formatVersionInfo())
		return "", nil, flag.ErrHelp
	}

	return *configFile, fs, nil
}

func lookupConfigKey(koanfInstance *koanf.Koanf, camelKey string) (string, bool) {
	envKey := strings.ToLower(camelKey)
	if koanfInstance.Exists(envKey) {
		return koanfInstance.String(envKey), true
	}
	if koanfInstance.Exists(camelKey) {
		return koanfInstance.String(camelKey), true
	}
	underscoreKey := strings.ReplaceAll(strings.ToLower(camelKey), ".", "_")
	if koanfInstance.Exists(underscoreKey) {
		return koanfInstance.String(underscoreKey), true
	}
	return "", false
}

func getConfigString(koanfInstance *koanf.Koanf, camelKey, defaultValue string) string {
	if val, ok := lookupConfigKey(koanfInstance, camelKey); ok {
		return val
	}
	return defaultValue
}

func getConfigInt(koanfInstance *koanf.Koanf, camelKey string, defaultValue int) int {
	if val, ok := lookupConfigKey(koanfInstance, camelKey); ok {
		var i int
		if _, err := fmt.Sscanf(val, "%d", &i); err == nil {
			return i
		}
	}
	return defaultValue
}

func getConfigBool(koanfInstance *koanf.Koanf, camelKey string, defaultValue bool) bool {
	if val, ok := lookupConfigKey(koanfInstance, camelKey); ok {
		return strings.EqualFold(val, "true") || val == "1"
	}
	return defaultValue
}

func readExporterConfig(koanfInstance *koanf.Koanf, l *slog.Logger) ExporterConfig {
	var cfg ExporterConfig

	cfg.Port = getConfigInt(koanfInstance, "exporter.port", 9090) //nolint:mnd
	l.Info("Config: exporter.port", "port", cfg.Port)

	cfg.Metrics.Prefix = getConfigString(koanfInstance, "exporter.metrics.prefix", "kopia_go_exporter")
	l.Info("Config: exporter.metrics.prefix", "prefix", cfg.Metrics.Prefix)

	cfg.Interval = getConfigInt(koanfInstance, "exporter.interval", 300) //nolint:mnd
	l.Info("Config: exporter.interval", "interval", cfg.Interval)

	return cfg
}

func readKopiaConfig(koanfInstance *koanf.Koanf, l *slog.Logger) KopiaConfig {
	var cfg KopiaConfig

	cfg.Password = getConfigString(koanfInstance, "kopia.password", "")
	l.Info("Config: kopia.password", "password", "****")

	cfg.APIServer.RepositoryURL = getConfigString(koanfInstance, "kopia.apiserver.repositoryURL", "")
	l.Info("Config: kopia.apiserver.repositoryURL", "repositoryURL", cfg.APIServer.RepositoryURL)

	cfg.APIServer.Hostname = getConfigString(koanfInstance, "kopia.apiserver.hostname", "")
	l.Info("Config: kopia.apiserver.hostname", "hostname", cfg.APIServer.Hostname)

	cfg.APIServer.Username = getConfigString(koanfInstance, "kopia.apiserver.username", "")
	l.Info("Config: kopia.apiserver.username", "username", cfg.APIServer.Username)

	cfg.APIServer.Fingerprint = getConfigString(koanfInstance, "kopia.apiserver.fingerprint", "")
	l.Info("Config: kopia.apiserver.fingerprint", "fingerprint", "****")

	// Read retentions list
	cfg.Retentions = make([]string, 0)
	if koanfInstance.Exists("kopia.retentionstoextract") {
		if err := koanfInstance.Unmarshal("kopia.retentionstoextract", &cfg.Retentions); err != nil {
			l.Warn("Failed to unmarshal retentions", "err", err)
		}
	}
	l.Info("Config: kopia.retentionstoextract", "retentions", cfg.Retentions)

	return cfg
}

func readConfig(filename string, fs *pflag.FlagSet) error {
	l := logger.Get()

	k = koanf.New(".")
	if err := k.Load(file.Provider(filename), yaml.Parser()); err != nil {
		return fmt.Errorf("failed to read configuration file %s: %w", filename, err)
	}

	// Load environment variables with KGE_ prefix (overrides YAML values)
	loadConfigLayer(k, env.Provider("KGE_", ".", func(s string) string {
		s = strings.TrimPrefix(s, "KGE_")
		s = strings.ToLower(s)
		s = strings.ReplaceAll(s, "_", ".")
		return s
	}), "Failed to load environment variable overrides")

	// Load pflag values, converting dashes to dots for koanf key matching.
	// Only flags explicitly set by the user override YAML/env values.
	if fs != nil {
		loadConfigLayer(k, posflag.ProviderWithValue(fs, ".", k, func(key, value string) (string, interface{}) {
			return strings.ReplaceAll(key, "-", "."), value
		}), "Failed to load flag overrides")
	}

	Cfg.Exporter = readExporterConfig(k, l)
	Cfg.Kopia = readKopiaConfig(k, l)
	Cfg.LogLevel = getConfigString(k, "log_level", "info")
	l.Info("Config: log_level", "log_level", Cfg.LogLevel)

	return nil
}

// loadConfigLayer loads a koanf provider, logging a warning instead of failing
// when the provider errors, since env/flag overrides are best-effort.
func loadConfigLayer(k *koanf.Koanf, loader koanf.Provider, msg string) {
	l := logger.Get()
	if err := k.Load(loader, nil); err != nil {
		l.Warn(msg, "err", err)
	}
}

// CheckConfig validates that all required configuration fields are set.
func CheckConfig() error {
	if Cfg.Kopia.Password == "" {
		return fmt.Errorf("kopia.password is not set")
	}
	if Cfg.Kopia.APIServer.RepositoryURL == "" {
		return fmt.Errorf("kopia.apiserver.repositoryURL is not set")
	}
	if Cfg.Kopia.APIServer.Fingerprint == "" {
		return fmt.Errorf("kopia.apiserver.fingerprint is not set")
	}
	if Cfg.Kopia.APIServer.Hostname == "" {
		return fmt.Errorf("kopia.apiserver.hostname is not set")
	}
	if Cfg.Kopia.APIServer.Username == "" {
		return fmt.Errorf("kopia.apiserver.username is not set")
	}
	return nil
}

// New parses flags, loads the config file, and validates all required fields.
func New(version string, args []string) error {
	configFile, fs, err := ParseFlags(version, args)
	if err != nil {
		return err
	}
	if err := readConfig(configFile, fs); err != nil {
		return err
	}
	return CheckConfig()
}
