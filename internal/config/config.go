// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"

	"kopia-go-exporter/internal/logger"
)

const maxTCPPort = 65535

var ReadBuildInfo = debug.ReadBuildInfo

var prometheusMetricName = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)

//go:embed config.default.yaml
var defaultConfig []byte

type ExporterConfig struct {
	Port    int
	Metrics struct {
		Prefix string
	}
	Interval int
}

type APIServerConfig struct {
	RepositoryURL string
	Fingerprint   string
}

// ClientConfig is a Kopia client identity (username@hostname) with its
// server user password. Each entry in KopiaConfig.Clients connects to the
// same API server under a different identity.
type ClientConfig struct {
	Username string
	Hostname string
	Password string
}

type KopiaConfig struct {
	APIServer  APIServerConfig
	Clients    map[string]ClientConfig
	Retentions []string
}

type LogFileConfig struct {
	MaxSize    int
	MaxBackups int
	MaxAge     int
	Compress   bool
}

type LoggerConfig struct {
	Level           string
	JSON            bool
	Filename        string
	RedactSensitive bool
	LogFile         LogFileConfig
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

// VersionInfo holds build version information extracted from Go's debug info.
type VersionInfo struct {
	Version   string
	Revision  string
	Time      string
	GoVersion string
}

// GetVersionInfo returns build version, revision, and time from Go's debug info.
func GetVersionInfo(version string) VersionInfo {
	info := VersionInfo{Version: version}

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
		case "vcs.revision":
			info.Revision = kv.Value
		case "vcs.time":
			info.Time = kv.Value
		}
	}
	return info
}

// formatVersionInfo returns the formatted build version information.
func formatVersionInfo(version string) string {
	vi := GetVersionInfo(version)

	output := fmt.Sprintf("%-15s: %s\n", "Version", vi.Version)
	output += fmt.Sprintf("%-15s: %s\n", "Revision", vi.Revision)
	output += fmt.Sprintf("%-15s: %s\n", "Last Commit", vi.Time)
	output += fmt.Sprintf("%-15s: %s\n", "Go Version", vi.GoVersion)
	return output
}

// readFlagDefaults reads flag defaults from the embedded config so the
// command-line help always matches the effective YAML defaults.
func readFlagDefaults() (port int, logLevel string, err error) {
	defaultK := koanf.New(".")
	if err := defaultK.Load(rawbytes.Provider(defaultConfig), yaml.Parser()); err != nil {
		return 0, "", fmt.Errorf("failed to load embedded default configuration: %w", err)
	}

	port, err = configInt(defaultK, "exporter.port")
	if err != nil {
		return 0, "", err
	}
	return port, configString(defaultK, "logger.log_level"), nil
}

// ParseFlags parses command-line flags and returns the config file path and parsed flagset.
func ParseFlags(version string, args []string) (string, *pflag.FlagSet, error) {
	portDefault, logLevelDefault, err := readFlagDefaults()
	if err != nil {
		return "", nil, err
	}

	fs := pflag.NewFlagSet("kopia-go-exporter", pflag.ContinueOnError)

	configFile := fs.StringP("config", "c", "", "Path to YAML config file")
	fs.Int("exporter-port", portDefault, "Exporter HTTP server port")
	fs.StringP("log_level", "l", logLevelDefault, "Log level (debug, info, warn, error)")
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
		fmt.Print(formatVersionInfo(version))
		return "", nil, flag.ErrHelp
	}

	return *configFile, fs, nil
}

func lookupConfigKey(k *koanf.Koanf, key string) (string, bool) {
	lowerKey := strings.ToLower(key)
	for _, candidate := range []string{
		key,
		lowerKey,
		strings.ReplaceAll(lowerKey, ".", "_"),
	} {
		if k.Exists(candidate) {
			return k.String(candidate), true
		}
	}

	return "", false
}

func configString(k *koanf.Koanf, key string) string {
	value, _ := lookupConfigKey(k, key)
	return value
}

func configInt(k *koanf.Koanf, key string) (int, error) {
	v, ok := lookupConfigKey(k, key)
	if !ok {
		return 0, nil
	}

	i, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid integer for %s: %q: %w", key, v, err)
	}
	return i, nil
}

func configBool(k *koanf.Koanf, key string) (bool, error) {
	v, ok := lookupConfigKey(k, key)
	if !ok {
		return false, nil
	}

	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid boolean for %s: %q: %w", key, v, err)
	}
	return b, nil
}

func readExporterConfig(koanfInstance *koanf.Koanf) (ExporterConfig, error) {
	var cfg ExporterConfig
	var errs []error
	var err error

	cfg.Port, err = configInt(koanfInstance, "exporter.port")
	errs = append(errs, err)

	cfg.Metrics.Prefix = configString(koanfInstance, "exporter.metrics.prefix")

	cfg.Interval, err = configInt(koanfInstance, "exporter.interval")
	errs = append(errs, err)

	return cfg, errors.Join(errs...)
}

func readFiltersConfig(koanfInstance *koanf.Koanf) (FiltersConfig, error) {
	var cfg FiltersConfig
	var err error

	cfg.Include, err = readFilterGroup(koanfInstance, "filters.include")
	if err != nil {
		return cfg, err
	}

	cfg.Exclude, err = readFilterGroup(koanfInstance, "filters.exclude")
	if err != nil {
		return cfg, err
	}

	return cfg, nil
}

func readFilterGroup(koanfInstance *koanf.Koanf, key string) (FilterConfig, error) {
	var cfg FilterConfig

	cfg.Path = make([]string, 0)
	if koanfInstance.Exists(key + ".path") {
		if err := koanfInstance.Unmarshal(key+".path", &cfg.Path); err != nil {
			return cfg, fmt.Errorf("invalid %s.path: %w", key, err)
		}
	}

	regexes, err := compileRegexes(cfg.Path)
	if err != nil {
		return cfg, fmt.Errorf("invalid %s.path regex: %w", key, err)
	}
	cfg.PathRegex = regexes

	return cfg, nil
}

func compileRegexes(patterns []string) ([]*regexp.Regexp, error) {
	regexes := make([]*regexp.Regexp, 0, len(patterns))
	for i, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("pattern[%d] %q: %w", i, p, err)
		}
		regexes = append(regexes, re)
	}
	return regexes, nil
}

func readKopiaConfig(koanfInstance *koanf.Koanf) (KopiaConfig, error) {
	var cfg KopiaConfig

	cfg.APIServer.RepositoryURL = configString(koanfInstance, "kopia.apiserver.repositoryURL")

	cfg.APIServer.Fingerprint = configString(koanfInstance, "kopia.apiserver.fingerprint")

	cfg.Clients = make(map[string]ClientConfig)
	if koanfInstance.Exists("kopia.clients") {
		if err := koanfInstance.Unmarshal("kopia.clients", &cfg.Clients); err != nil {
			return cfg, fmt.Errorf("invalid kopia.clients: %w", err)
		}
	}

	// Read retentions list
	cfg.Retentions = make([]string, 0)
	if koanfInstance.Exists("kopia.retentionstoextract") {
		if err := koanfInstance.Unmarshal("kopia.retentionstoextract", &cfg.Retentions); err != nil {
			return cfg, fmt.Errorf("invalid kopia.retentionstoextract: %w", err)
		}
	}

	return cfg, nil
}

func readLoggerConfig(koanfInstance *koanf.Koanf) (LoggerConfig, error) {
	var cfg LoggerConfig
	var errs []error
	var err error

	cfg.Level = configString(koanfInstance, "logger.log_level")

	cfg.JSON, err = configBool(koanfInstance, "logger.json")
	errs = append(errs, err)

	cfg.Filename = configString(koanfInstance, "logger.filename")

	cfg.RedactSensitive, err = configBool(koanfInstance, "logger.redact_sensitive")
	errs = append(errs, err)

	cfg.LogFile.MaxSize, err = configInt(koanfInstance, "logger.log_file.maxsize")
	errs = append(errs, err)

	cfg.LogFile.MaxBackups, err = configInt(koanfInstance, "logger.log_file.maxbackups")
	errs = append(errs, err)

	cfg.LogFile.MaxAge, err = configInt(koanfInstance, "logger.log_file.maxage")
	errs = append(errs, err)

	cfg.LogFile.Compress, err = configBool(koanfInstance, "logger.log_file.compress")
	errs = append(errs, err)

	return cfg, errors.Join(errs...)
}

func readConfig(filename string, fs *pflag.FlagSet) (Config, error) {
	l := logger.Get()
	var cfg Config

	koanfInstance := koanf.New(".")

	// Load embedded default configuration first
	if len(defaultConfig) > 0 {
		if err := koanfInstance.Load(rawbytes.Provider(defaultConfig), yaml.Parser()); err != nil {
			l.Warn("Failed to load embedded default configuration", "err", err)
		}
	}

	// Load user configuration file (overrides defaults)
	if filename != "" {
		if err := koanfInstance.Load(file.Provider(filename), yaml.Parser()); err != nil {
			return cfg, fmt.Errorf("failed to read configuration file %s: %w", filename, err)
		}
	}

	// Load environment variables with KGE_ prefix (overrides YAML values)
	loadConfigLayer(koanfInstance, env.Provider("KGE_", ".", func(name string) string {
		return kgeKeyMapper(koanfInstance, name)
	}), "Failed to load environment variable overrides")

	// Load pflag values, converting dashes to dots for koanf key matching.
	// Only flags explicitly set by the user override YAML/env values.
	if fs != nil {
		loadConfigLayer(koanfInstance,
			posflag.ProviderWithValue(fs, ".", koanfInstance, flagKeyMapper),
			"Failed to load flag overrides",
		)
	}

	var err error
	cfg.Exporter, err = readExporterConfig(koanfInstance)
	if err != nil {
		return cfg, err
	}
	cfg.Filters, err = readFiltersConfig(koanfInstance)
	if err != nil {
		return cfg, err
	}
	cfg.Kopia, err = readKopiaConfig(koanfInstance)
	if err != nil {
		return cfg, err
	}
	cfg.Logger, err = readLoggerConfig(koanfInstance)
	if err != nil {
		return cfg, err
	}

	if err := checkPlaceholders(koanfInstance); err != nil {
		return cfg, err
	}

	logConfig(l, &cfg)

	return cfg, nil
}

// kgeKeyMapper maps a KGE_-prefixed environment variable name to its
// corresponding koanf config key. It first tries to match a known config key
// so keys containing underscores (for example logger.log_file.maxsize) keep
// their exact name. Unknown keys fall back to lowercased dots.
func kgeKeyMapper(koanfInstance *koanf.Koanf, s string) string {
	name := strings.TrimPrefix(s, "KGE_")
	for _, key := range koanfInstance.Keys() {
		if strings.EqualFold(strings.ReplaceAll(key, ".", "_"), name) {
			return key
		}
	}
	return strings.ReplaceAll(strings.ToLower(name), "_", ".")
}

// loadConfigLayer loads a koanf provider, logging a warning instead of failing
// when the provider errors, since env/flag overrides are best-effort.
func loadConfigLayer(k *koanf.Koanf, loader koanf.Provider, msg string) {
	l := logger.Get()
	if err := k.Load(loader, nil); err != nil {
		l.Warn(msg, "err", err)
	}
}

// RedactURL removes credentials from rawURL when redact is true. Invalid URLs
// are replaced entirely to avoid leaking malformed credentials.
func RedactURL(rawURL string, redact bool) string {
	if !redact {
		return rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "****"
	}
	if parsed.User == nil {
		return rawURL
	}
	parsed.User = url.UserPassword("redacted", "redacted")
	return parsed.String()
}

// logConfig logs every configuration key at INFO level, one message per key.
// When logger.redact_sensitive is true (the default), passwords and
// fingerprints are replaced with "****", and URL credentials are redacted.
func logConfig(l *slog.Logger, cfg *Config) {
	redact := func(val string) string {
		if cfg.Logger.RedactSensitive {
			return "****"
		}
		return val
	}
	l.Info("Config: exporter.port", "port", cfg.Exporter.Port)
	l.Info("Config: exporter.metrics.prefix", "prefix", cfg.Exporter.Metrics.Prefix)
	l.Info("Config: exporter.interval", "interval", cfg.Exporter.Interval)
	l.Info("Config: filters.include.path", "path", cfg.Filters.Include.Path)
	l.Info("Config: filters.exclude.path", "path", cfg.Filters.Exclude.Path)
	l.Info("Config: kopia.apiserver.repositoryURL", "repositoryURL", RedactURL(cfg.Kopia.APIServer.RepositoryURL, cfg.Logger.RedactSensitive))
	l.Info("Config: kopia.apiserver.fingerprint", "fingerprint", redact(cfg.Kopia.APIServer.Fingerprint))
	for _, name := range sortedClientNames(cfg) {
		client := cfg.Kopia.Clients[name]
		l.Info("Config: kopia.clients."+name+".username", "username", client.Username)
		l.Info("Config: kopia.clients."+name+".hostname", "hostname", client.Hostname)
		l.Info("Config: kopia.clients."+name+".password", "password", redact(client.Password))
	}
	l.Info("Config: kopia.retentionstoextract", "retentions", cfg.Kopia.Retentions)
	l.Info("Config: logger.log_level", "log_level", cfg.Logger.Level)
	l.Info("Config: logger.json", "json", cfg.Logger.JSON)
	l.Info("Config: logger.filename", "filename", cfg.Logger.Filename)
	l.Info("Config: logger.redact_sensitive", "redact_sensitive", cfg.Logger.RedactSensitive)
	l.Info("Config: logger.log_file.maxsize", "maxsize", cfg.Logger.LogFile.MaxSize)
	l.Info("Config: logger.log_file.maxbackups", "maxbackups", cfg.Logger.LogFile.MaxBackups)
	l.Info("Config: logger.log_file.maxage", "maxage", cfg.Logger.LogFile.MaxAge)
	l.Info("Config: logger.log_file.compress", "compress", cfg.Logger.LogFile.Compress)
}

// flagKeyMapper converts a pflag key (using dashes) into the dotted koanf key
// format used throughout the configuration tree.
func flagKeyMapper(key, value string) (mapped string, mappedValue any) {
	mapped = strings.ReplaceAll(key, "-", ".")
	mappedValue = value
	return mapped, mappedValue
}

// CheckConfig validates required and critical configuration values.
func CheckConfig(cfg *Config) error {
	var errs []error

	if cfg.Exporter.Port < 1 || cfg.Exporter.Port > maxTCPPort {
		errs = append(errs, fmt.Errorf("exporter.port must be between 1 and %d, got %d", maxTCPPort, cfg.Exporter.Port))
	}
	if cfg.Exporter.Interval <= 0 {
		errs = append(errs, fmt.Errorf("exporter.interval must be positive, got %d", cfg.Exporter.Interval))
	}
	if cfg.Exporter.Metrics.Prefix != "" && !prometheusMetricName.MatchString(cfg.Exporter.Metrics.Prefix) {
		errs = append(errs, fmt.Errorf("exporter.metrics.prefix %q is not a valid Prometheus metric name", cfg.Exporter.Metrics.Prefix))
	}
	if cfg.Kopia.APIServer.RepositoryURL == "" {
		errs = append(errs, errors.New("kopia.apiserver.repositoryURL is not set"))
	}
	if cfg.Kopia.APIServer.Fingerprint == "" {
		errs = append(errs, errors.New("kopia.apiserver.fingerprint is not set"))
	}
	if len(cfg.Kopia.Clients) == 0 {
		errs = append(errs, errors.New("kopia.clients is empty; configure at least one client under kopia.clients"))
	}
	for _, name := range sortedClientNames(cfg) {
		client := cfg.Kopia.Clients[name]
		if client.Username == "" {
			errs = append(errs, fmt.Errorf("kopia.clients.%s.username is not set", name))
		}
		if client.Hostname == "" {
			errs = append(errs, fmt.Errorf("kopia.clients.%s.hostname is not set", name))
		}
		if client.Password == "" {
			errs = append(errs, fmt.Errorf("kopia.clients.%s.password is not set", name))
		}
	}
	return errors.Join(errs...)
}

// sortedClientNames returns the names of all configured Kopia clients in
// lexicographic order, so validation errors and config logs are stable.
func sortedClientNames(cfg *Config) []string {
	names := make([]string, 0, len(cfg.Kopia.Clients))
	for name := range cfg.Kopia.Clients {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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
func checkPlaceholders(koanfInstance *koanf.Koanf) error {
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
		currentVal := configString(koanfInstance, key)
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
func New(version string, args []string) (*Config, error) {
	configFile, fs, err := ParseFlags(version, args)
	if err != nil {
		return nil, err
	}
	cfg, err := readConfig(configFile, fs)
	if err != nil {
		return nil, err
	}
	if err := CheckConfig(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
