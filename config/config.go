// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
)

var k = koanf.New(".")

var givenVersion string

type CLIFlags struct {
	ConfigFile string
	Port       int
	LogLevel   string
	ShowVersion bool
}

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
	ConfigFile            string
	ConnectWithConfigFile bool
	Password              string
	APIServer             APIServerConfig
	Retentions            []string
}

type Config struct {
	Exporter ExporterConfig
	Kopia    KopiaConfig
	LogLevel string
}

var Cfg Config

func versionInfo(version string) string {
	output := fmt.Sprintf("%-15s: %s\n", "Version", version)

	var lastCommit time.Time
	revision := "unknown"
	dirtyBuild := true

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return output
	}

	for _, kv := range info.Settings {
		if kv.Value == "" {
			continue
		}
		switch kv.Key {
		case "vcs.revision":
			revision = kv.Value
		case "vcs.time":
			lastCommit, _ = time.Parse(time.RFC3339, kv.Value)
		case "vcs.modified":
			dirtyBuild = kv.Value == "true"
		}
	}

	output += fmt.Sprintf("%-15s: %s\n", "Revision", revision)
	output += fmt.Sprintf("%-15s: %v\n", "Dirty Build", dirtyBuild)
	output += fmt.Sprintf("%-15s: %s\n", "Last Commit", lastCommit)
	output += fmt.Sprintf("%-15s: %s\n", "Go Version", info.GoVersion)
	return output
}

func ParseFlags(version string, args []string) (CLIFlags, error) {
	givenVersion = version

	fs := pflag.NewFlagSet("kopia-go-exporter", pflag.ContinueOnError)

	configFile := fs.StringP("config", "c", "config.yaml", "Path to YAML config file")
	port := fs.IntP("port", "p", 8080, "Port to run the exporter on")
	logLevel := fs.StringP("log_level", "l", "info", "Log level (debug, info, warn, error)")
	showVersion := fs.BoolP("version", "V", false, "Print version information and exit")
	showHelp := fs.BoolP("help", "h", false, "Print help")

	if err := fs.Parse(args); err != nil {
		return CLIFlags{}, err
	}

	if *showHelp {
		fs.PrintDefaults()
		return CLIFlags{}, flag.ErrHelp
	}

	if *showVersion {
		output := versionInfo(version)
		fmt.Print(output)
		return CLIFlags{}, flag.ErrHelp
	}

	return CLIFlags{
		ConfigFile:  *configFile,
		Port:        *port,
		LogLevel:    *logLevel,
		ShowVersion: *showVersion,
	}, nil
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
		return strings.ToLower(val) == "true" || val == "1"
	}
	return defaultValue
}

func getConfigDuration(koanfInstance *koanf.Koanf, camelKey, defaultDuration string) (time.Duration, error) {
	durationStr := defaultDuration
	if val, ok := lookupConfigKey(koanfInstance, camelKey); ok {
		durationStr = val
	}
	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return 0, err
	}
	return duration, nil
}

func readExporterConfig(koanfInstance *koanf.Koanf, l *slog.Logger) ExporterConfig {
	var cfg ExporterConfig

	cfg.Port = getConfigInt(koanfInstance, "exporter.port", 8080)
	l.Info("Config: exporter.port", "port", cfg.Port)

	cfg.Metrics.Prefix = getConfigString(koanfInstance, "exporter.metrics.prefix", "kopia_go_exporter")
	l.Info("Config: exporter.metrics.prefix", "prefix", cfg.Metrics.Prefix)

	cfg.Interval = getConfigInt(koanfInstance, "exporter.interval", 300)
	l.Info("Config: exporter.interval", "interval", cfg.Interval)

	return cfg
}

func readKopiaConfig(koanfInstance *koanf.Koanf, l *slog.Logger) KopiaConfig {
	var cfg KopiaConfig

	cfg.ConfigFile = getConfigString(koanfInstance, "kopia.configfile", "/tmp/kopia.cfg")
	l.Info("Config: kopia.configfile", "configfile", cfg.ConfigFile)

	cfg.ConnectWithConfigFile = getConfigBool(koanfInstance, "kopia.connectwithconfigfile", false)
	l.Info("Config: kopia.connectwithconfigfile", "connectwithconfigfile", cfg.ConnectWithConfigFile)

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

func readConfig(filename string, flags CLIFlags) error {
	l := slog.Default()

	if err := k.Load(file.Provider(filename), yaml.Parser()); err != nil {
		return fmt.Errorf("failed to read configuration file %s: %w", filename, err)
	}

	// Load environment variables with KGE_ prefix (overrides YAML values)
	if err := k.Load(env.Provider("KGE_", ".", func(s string) string {
		s = strings.TrimPrefix(s, "KGE_")
		s = strings.ToLower(s)
		s = strings.ReplaceAll(s, "_", ".")
		return s
	}), nil); err != nil {
		return fmt.Errorf("failed to load environment variables: %w", err)
	}

	Cfg.Exporter = readExporterConfig(k, l)
	Cfg.Kopia = readKopiaConfig(k, l)
	Cfg.LogLevel = getConfigString(k, "log_level", flags.LogLevel)
	l.Info("Config: log_level", "log_level", Cfg.LogLevel)

	return nil
}

func CheckConfig() {
	if Cfg.Kopia.ConfigFile == "" {
		Cfg.Kopia.ConfigFile = "/tmp/kopia.cfg"
		slog.Warn("Kopia.configfile was not specified. Using /tmp/kopia.cfg")
	}
	if Cfg.Kopia.Password == "" {
		slog.Error("kopia.password is not set (needed when kopia.configfile is provided)")
		os.Exit(1)
	}
	if !Cfg.Kopia.ConnectWithConfigFile {
		if Cfg.Kopia.APIServer.RepositoryURL == "" {
			slog.Error("kopia.repositoryURL is not set (needed when kopia.configfile is not provided)")
			os.Exit(1)
		}
		if Cfg.Kopia.APIServer.Fingerprint == "" {
			slog.Error("kopia.fingerprint is not set (needed when kopia.configfile is not provided)")
			os.Exit(1)
		}
		if Cfg.Kopia.APIServer.Hostname == "" {
			slog.Error("kopia.hostname is not set (needed when kopia.configfile is not provided)")
			os.Exit(1)
		}
		if Cfg.Kopia.APIServer.Username == "" {
			slog.Error("kopia.username is not set (needed when kopia.configfile is not provided)")
			os.Exit(1)
		}
	} else {
		slog.Error("kopia.connectwithconfigfile is not supported yet")
		os.Exit(1)
	}
}

func New(version string, args []string) error {
	flags, err := ParseFlags(version, args)
	if err != nil {
		return err
	}

	if err := readConfig(flags.ConfigFile, flags); err != nil {
		return err
	}

	return nil
}

func GetVersionFull() (version, revision, time string, dirty, ok bool) {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return strings.TrimSpace(givenVersion), "", "", false, false
	}

	var modified string
	for _, setting := range buildInfo.Settings {
		if setting.Key == "vcs.revision" {
			revision = setting.Value
		} else if setting.Key == "vcs.modified" {
			modified = setting.Value
		} else if setting.Key == "vcs.time" {
			time = setting.Value
		}
	}

	dirty = false
	if modified == "true" {
		dirty = true
	}

	return strings.TrimSpace(givenVersion), revision, time, dirty, true
}
