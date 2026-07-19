// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"regexp"
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

// rawBytesProvider is a simple koanf.Provider that wraps raw bytes.
type rawBytesProvider struct {
	data []byte
}

func (r *rawBytesProvider) ReadBytes() ([]byte, error) {
	return r.data, nil
}

// Read implements koanf.Provider but is unused; koanf loads via ReadBytes() + yaml.Parser().
func (r *rawBytesProvider) Read() (map[string]any, error) {
	return nil, fmt.Errorf("Read() not implemented, use ReadBytes() with yaml.Parser()")
}

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

type LoggerConfig struct {
	Level      string
	JSON       bool
	Filename   string
	MaxSize    int
	MaxBackups int
	MaxAge     int
	Compress   bool
}

type FilterConfig struct {
	Path      []string
	PathRegex []*regexp.Regexp
}

type FiltersConfig struct {
	Include FilterConfig
	Exclude FilterConfig
}

type Config struct {
	Exporter ExporterConfig
	Filters  FiltersConfig
	Kopia    KopiaConfig
	Logger   LoggerConfig
}

var Cfg Config

// VersionInfo holds build version information extracted from Go's debug info.
type VersionInfo struct {
	Version   string
	Revision  string
	Time      string
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
		}
	}
	return info
}

// formatVersionInfo returns the formatted build version information.
func formatVersionInfo() string {
	vi := GetVersionInfo()

	output := fmt.Sprintf("%-15s: %s\n", "Version", vi.Version)
	output += fmt.Sprintf("%-15s: %s\n", "Revision", vi.Revision)
	output += fmt.Sprintf("%-15s: %s\n", "Last Commit", vi.Time)
	output += fmt.Sprintf("%-15s: %s\n", "Go Version", vi.GoVersion)
	return output
}

// ParseFlags parses command-line flags and returns the config file path and parsed flagset.
func ParseFlags(version string, args []string) (string, *pflag.FlagSet, error) {
	givenVersion = version

	fs := pflag.NewFlagSet("kopia-go-exporter", pflag.ContinueOnError)

	configFile := fs.StringP("config", "c", "", "Path to YAML config file")
	fs.Int("exporter-port", 9090, "Exporter HTTP server port") //nolint:mnd
	fs.StringP("log_level", "l", "info", "Log level (debug, info, warn, error)")
	showVersion := fs.BoolP("version", "V", false, "Print version information and exit")
	showHelp := fs.BoolP("help", "h", false, "Print help")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fs.PrintDefaults()
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

func readFiltersConfig(koanfInstance *koanf.Koanf, l *slog.Logger) (FiltersConfig, error) {
	var cfg FiltersConfig

	cfg.Include.Path = make([]string, 0)
	if koanfInstance.Exists("filters.include.path") {
		if err := koanfInstance.Unmarshal("filters.include.path", &cfg.Include.Path); err != nil {
			l.Warn("Failed to unmarshal filters.include.path", "err", err)
		}
	}
	l.Info("Config: filters.include.path", "path", cfg.Include.Path)
	includeRegex, err := compileRegexes(cfg.Include.Path)
	if err != nil {
		return cfg, fmt.Errorf("invalid filters.include.path regex: %w", err)
	}
	cfg.Include.PathRegex = includeRegex

	cfg.Exclude.Path = make([]string, 0)
	if koanfInstance.Exists("filters.exclude.path") {
		if err := koanfInstance.Unmarshal("filters.exclude.path", &cfg.Exclude.Path); err != nil {
			l.Warn("Failed to unmarshal filters.exclude.path", "err", err)
		}
	}
	l.Info("Config: filters.exclude.path", "path", cfg.Exclude.Path)
	excludeRegex, err := compileRegexes(cfg.Exclude.Path)
	if err != nil {
		return cfg, fmt.Errorf("invalid filters.exclude.path regex: %w", err)
	}
	cfg.Exclude.PathRegex = excludeRegex

	return cfg, nil
}

func compileRegexes(patterns []string) ([]*regexp.Regexp, error) {
	regexes := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, err
		}
		regexes = append(regexes, re)
	}
	return regexes, nil
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

func readLoggerConfig(koanfInstance *koanf.Koanf, l *slog.Logger) LoggerConfig {
	var cfg LoggerConfig

	cfg.Level = getConfigString(koanfInstance, "logger.log_level", "info")
	if envLevel := os.Getenv("KGE_LOGGER_LOG_LEVEL"); envLevel != "" {
		cfg.Level = envLevel
	}
	l.Info("Config: log_level", "log_level", cfg.Level)

	cfg.JSON = getConfigBool(koanfInstance, "logger.json", false)
	l.Info("Config: logger.json", "json", cfg.JSON)

	cfg.Filename = getConfigString(koanfInstance, "logger.filename", "")
	l.Info("Config: logger.filename", "filename", cfg.Filename)

	cfg.MaxSize = getConfigInt(koanfInstance, "logger.maxsize", 100) //nolint:mnd
	l.Info("Config: logger.maxsize", "maxsize", cfg.MaxSize)

	cfg.MaxBackups = getConfigInt(koanfInstance, "logger.maxbackups", 3)
	l.Info("Config: logger.maxbackups", "maxbackups", cfg.MaxBackups)

	cfg.MaxAge = getConfigInt(koanfInstance, "logger.maxage", 28) //nolint:mnd
	l.Info("Config: logger.maxage", "maxage", cfg.MaxAge)

	cfg.Compress = getConfigBool(koanfInstance, "logger.compress", false)
	l.Info("Config: logger.compress", "compress", cfg.Compress)

	return cfg
}

func readConfig(filename string, fs *pflag.FlagSet, defaultConfig []byte) error {
	l := logger.Get()

	k = koanf.New(".")

	// Load embedded default configuration first
	if len(defaultConfig) > 0 {
		if err := k.Load(&rawBytesProvider{data: defaultConfig}, yaml.Parser()); err != nil {
			l.Warn("Failed to load embedded default configuration", "err", err)
		}
	}

	// Load user configuration file (overrides defaults)
	if filename != "" {
		if err := k.Load(file.Provider(filename), yaml.Parser()); err != nil {
			return fmt.Errorf("failed to read configuration file %s: %w", filename, err)
		}
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
		loadConfigLayer(k,
			posflag.ProviderWithValue(fs, ".", k, flagKeyMapper),
			"Failed to load flag overrides",
		)
	}

	Cfg.Exporter = readExporterConfig(k, l)
	var err error
	Cfg.Filters, err = readFiltersConfig(k, l)
	if err != nil {
		return err
	}
	Cfg.Kopia = readKopiaConfig(k, l)
	Cfg.Logger = readLoggerConfig(k, l)

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

// flagKeyMapper converts a pflag key (using dashes) into the dotted koanf key
// format used throughout the configuration tree.
func flagKeyMapper(key, value string) (mapped string, mappedValue any) {
	mapped = strings.ReplaceAll(key, "-", ".")
	mappedValue = value
	return mapped, mappedValue
}
func CheckConfig(defaultConfig []byte) error {
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
	if err := checkPlaceholders(defaultConfig); err != nil {
		return err
	}
	return nil
}

// isPlaceholder returns true if val matches the ^<.*>$ pattern used in
// config.default.yaml for values that must be overridden by the user.
func isPlaceholder(val string) bool {
	return strings.HasPrefix(val, "<") && strings.HasSuffix(val, ">")
}

// checkPlaceholders parses the embedded default config to find keys whose
// values match the ^<.*>$ placeholder pattern, then verifies that those
// same keys have been overridden (no longer matching ^<.*>$) in the final
// configuration. Values like "xx<xx>xx" are intentionally allowed.
func checkPlaceholders(defaultConfig []byte) error {
	if len(defaultConfig) == 0 {
		return nil
	}

	defaultK := koanf.New(".")
	if err := defaultK.Load(&rawBytesProvider{data: defaultConfig}, yaml.Parser()); err != nil {
		return fmt.Errorf("failed to parse default config for placeholder check: %w", err)
	}

	for _, key := range defaultK.Keys() {
		defaultVal := defaultK.String(key)
		if !isPlaceholder(defaultVal) {
			continue
		}
		currentVal := getConfigString(k, key, "")
		if isPlaceholder(currentVal) {
			return fmt.Errorf(
				"configuration key %q still has placeholder value %q; override it in config file, env var, or CLI flag",
				key, currentVal,
			)
		}
	}
	return nil
}

// New parses flags, loads the config file, and validates all required fields.
func New(version string, args []string, defaultConfig []byte) error {
	configFile, fs, err := ParseFlags(version, args)
	if err != nil {
		return err
	}
	if err := readConfig(configFile, fs, defaultConfig); err != nil {
		return err
	}
	return CheckConfig(defaultConfig)
}
