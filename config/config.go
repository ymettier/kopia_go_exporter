// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	"errors"
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
	"github.com/knadh/koanf/providers/rawbytes"
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

type LoggerConfig struct {
	Level           string
	JSON            bool
	Filename        string
	MaxSize         int
	MaxBackups      int
	MaxAge          int
	Compress        bool
	RedactSensitive bool
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
		l := logger.Get()
		l.Warn("Invalid integer value, using default", "key", camelKey, "value", val, "default", defaultValue)
	}
	return defaultValue
}

func getConfigBool(koanfInstance *koanf.Koanf, camelKey string, defaultValue bool) bool {
	if val, ok := lookupConfigKey(koanfInstance, camelKey); ok {
		return strings.EqualFold(val, "true") || val == "1"
	}
	return defaultValue
}

func readExporterConfig(koanfInstance *koanf.Koanf) ExporterConfig {
	var cfg ExporterConfig

	cfg.Port = getConfigInt(koanfInstance, "exporter.port", 9090) //nolint:mnd

	cfg.Metrics.Prefix = getConfigString(koanfInstance, "exporter.metrics.prefix", "kopia_go_exporter")

	cfg.Interval = getConfigInt(koanfInstance, "exporter.interval", 300) //nolint:mnd

	return cfg
}

func readFiltersConfig(koanfInstance *koanf.Koanf, l *slog.Logger) (FiltersConfig, error) {
	var cfg FiltersConfig
	var err error

	cfg.Include, err = readFilterGroup(koanfInstance, "filters.include", l)
	if err != nil {
		return cfg, fmt.Errorf("invalid filters.include.path regex: %w", err)
	}

	cfg.Exclude, err = readFilterGroup(koanfInstance, "filters.exclude", l)
	if err != nil {
		return cfg, fmt.Errorf("invalid filters.exclude.path regex: %w", err)
	}

	return cfg, nil
}

func readFilterGroup(koanfInstance *koanf.Koanf, key string, l *slog.Logger) (FilterConfig, error) {
	var cfg FilterConfig

	cfg.Path = make([]string, 0)
	if koanfInstance.Exists(key + ".path") {
		if err := koanfInstance.Unmarshal(key+".path", &cfg.Path); err != nil {
			l.Warn("Failed to unmarshal "+key+".path", "err", err)
		}
	}

	regexes, err := compileRegexes(cfg.Path)
	if err != nil {
		return cfg, err
	}
	cfg.PathRegex = regexes

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

	cfg.APIServer.RepositoryURL = getConfigString(koanfInstance, "kopia.apiserver.repositoryURL", "")

	cfg.APIServer.Hostname = getConfigString(koanfInstance, "kopia.apiserver.hostname", "")

	cfg.APIServer.Username = getConfigString(koanfInstance, "kopia.apiserver.username", "")

	cfg.APIServer.Fingerprint = getConfigString(koanfInstance, "kopia.apiserver.fingerprint", "")

	// Read retentions list
	cfg.Retentions = make([]string, 0)
	if koanfInstance.Exists("kopia.retentionstoextract") {
		if err := koanfInstance.Unmarshal("kopia.retentionstoextract", &cfg.Retentions); err != nil {
			l.Warn("Failed to unmarshal retentions", "err", err)
		}
	}

	return cfg
}

func readLoggerConfig(koanfInstance *koanf.Koanf) LoggerConfig {
	var cfg LoggerConfig

	cfg.Level = getConfigString(koanfInstance, "logger.log_level", "info")
	if envLevel := os.Getenv("KGE_LOGGER_LOG_LEVEL"); envLevel != "" {
		cfg.Level = envLevel
	}

	cfg.JSON = getConfigBool(koanfInstance, "logger.json", false)

	cfg.Filename = getConfigString(koanfInstance, "logger.filename", "")

	cfg.MaxSize = getConfigInt(koanfInstance, "logger.maxsize", 100) //nolint:mnd

	cfg.MaxBackups = getConfigInt(koanfInstance, "logger.maxbackups", 3)

	cfg.MaxAge = getConfigInt(koanfInstance, "logger.maxage", 28) //nolint:mnd

	cfg.Compress = getConfigBool(koanfInstance, "logger.compress", false)

	cfg.RedactSensitive = getConfigBool(koanfInstance, "logger.redact_sensitive", true)

	return cfg
}

func readConfig(filename string, fs *pflag.FlagSet, defaultConfig []byte) error {
	l := logger.Get()

	k = koanf.New(".")

	// Load embedded default configuration first
	if len(defaultConfig) > 0 {
		if err := k.Load(rawbytes.Provider(defaultConfig), yaml.Parser()); err != nil {
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

	Cfg.Exporter = readExporterConfig(k)
	var err error
	Cfg.Filters, err = readFiltersConfig(k, l)
	if err != nil {
		return err
	}
	Cfg.Kopia = readKopiaConfig(k, l)
	Cfg.Logger = readLoggerConfig(k)

	logConfig(l)

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

// logConfig logs every configuration key at INFO level, one message per key.
// When logger.redact is true (the default), password and fingerprint values
// are replaced with "****".
func logConfig(l *slog.Logger) {
	redact := func(val string) string {
		if Cfg.Logger.RedactSensitive {
			return "****"
		}
		return val
	}
	l.Info("Config: exporter.port", "port", Cfg.Exporter.Port)
	l.Info("Config: exporter.metrics.prefix", "prefix", Cfg.Exporter.Metrics.Prefix)
	l.Info("Config: exporter.interval", "interval", Cfg.Exporter.Interval)
	l.Info("Config: filters.include.path", "path", Cfg.Filters.Include.Path)
	l.Info("Config: filters.exclude.path", "path", Cfg.Filters.Exclude.Path)
	l.Info("Config: kopia.password", "password", redact(Cfg.Kopia.Password))
	l.Info("Config: kopia.apiserver.repositoryURL", "repositoryURL", Cfg.Kopia.APIServer.RepositoryURL)
	l.Info("Config: kopia.apiserver.hostname", "hostname", Cfg.Kopia.APIServer.Hostname)
	l.Info("Config: kopia.apiserver.username", "username", Cfg.Kopia.APIServer.Username)
	l.Info("Config: kopia.apiserver.fingerprint", "fingerprint", redact(Cfg.Kopia.APIServer.Fingerprint))
	l.Info("Config: kopia.retentionstoextract", "retentions", Cfg.Kopia.Retentions)
	l.Info("Config: logger.log_level", "log_level", Cfg.Logger.Level)
	l.Info("Config: logger.json", "json", Cfg.Logger.JSON)
	l.Info("Config: logger.filename", "filename", Cfg.Logger.Filename)
	l.Info("Config: logger.maxsize", "maxsize", Cfg.Logger.MaxSize)
	l.Info("Config: logger.maxbackups", "maxbackups", Cfg.Logger.MaxBackups)
	l.Info("Config: logger.maxage", "maxage", Cfg.Logger.MaxAge)
	l.Info("Config: logger.compress", "compress", Cfg.Logger.Compress)
	l.Info("Config: logger.redact_sensitive", "redact_sensitive", Cfg.Logger.RedactSensitive)
}

// flagKeyMapper converts a pflag key (using dashes) into the dotted koanf key
// format used throughout the configuration tree.
func flagKeyMapper(key, value string) (mapped string, mappedValue any) {
	mapped = strings.ReplaceAll(key, "-", ".")
	mappedValue = value
	return mapped, mappedValue
}
func CheckConfig(defaultConfig []byte) error {
	var errs []error
	if Cfg.Kopia.Password == "" {
		errs = append(errs, fmt.Errorf("kopia.password is not set"))
	}
	if Cfg.Kopia.APIServer.RepositoryURL == "" {
		errs = append(errs, fmt.Errorf("kopia.apiserver.repositoryURL is not set"))
	}
	if Cfg.Kopia.APIServer.Fingerprint == "" {
		errs = append(errs, fmt.Errorf("kopia.apiserver.fingerprint is not set"))
	}
	if Cfg.Kopia.APIServer.Hostname == "" {
		errs = append(errs, fmt.Errorf("kopia.apiserver.hostname is not set"))
	}
	if Cfg.Kopia.APIServer.Username == "" {
		errs = append(errs, fmt.Errorf("kopia.apiserver.username is not set"))
	}
	if err := checkPlaceholders(defaultConfig); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
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
	if err := defaultK.Load(rawbytes.Provider(defaultConfig), yaml.Parser()); err != nil {
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
