package modconfig

import (
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	flag "github.com/spf13/pflag"
)

var k = koanf.New(".")

var givenVersion string

type Config struct {
	Exporter struct {
		Port    int `koanf:"port"`
		Metrics struct {
			Prefix string `koanf:"prefix"`
		} `koanf:"metrics"`
		Interval int `koanf:"interval"`
	} `koanf:"exporter"`
	Kopia struct {
		ConfigFile            string `koanf:"configfile"`
		ConnectWithConfigFile bool   `koanf:"connectwithconfigfile"`
		Password              string `koanf:"password"`
		APIServer             struct {
			RepositoryURL string `koanf:"repositoryURL"`
			Hostname      string `koanf:"hostname"`
			Username      string `koanf:"username"`
			Fingerprint   string `koanf:"fingerprint"`
		} `koanf:"apiserver"`
		Retentions []string `koanf:"retentions"`
	} `koanf:"kopia"`
	LogLevel string `koanf:"log_level"`
}

var Cfg Config

func usage(f *flag.FlagSet) {
	fmt.Fprintln(os.Stdout, "Usage:")
	f.PrintDefaults()
	os.Exit(0)
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

func print_version() {
	version, revision, time, dirty, ok := GetVersionFull()
	fmt.Println("Version:", version)
	if !ok {
		fmt.Println("Build info not available")
	}

	dirtyString := ""
	if dirty {
		dirtyString = " (dirty build)"
	}
	fmt.Printf("Commit Hash: %s%s\nCommit Time: %s\n", revision, dirtyString, time)
	os.Exit(0)
}

func CheckConfig() {
	// Check Kopia config
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

func LoadConfig(version string) {
	givenVersion = version

	// Configure CLI
	f := flag.NewFlagSet("kopia-go-exporter", flag.ContinueOnError)
	f.String("config", "config.yaml", "Path to YAML config file")
	f.Int("exporter.port", 8080, "Port to run the exporter on")
	f.String("log_level", "info", "Log level (debug, info, warn, error)")
	versionFlag := f.Bool("version", false, "Print version information and exit")

	f.Usage = func() { usage(f) }

	// Parse flags from CLI args
	if err := f.Parse(os.Args[1:]); err != nil {
		// Display help on parse errors (including --help)
		f.Usage()
	}

	if *versionFlag {
		print_version()
	}

	// Load config from YAML file first
	configPath, _ := f.GetString("config")
	if err := k.Load(file.Provider(configPath), yaml.Parser()); err != nil {
		slog.Error("Failed to load config file")
		os.Exit(1)
	}

	// Load CLI flags to override config
	if err := k.Load(posflag.Provider(f, ".", k), nil); err != nil {
		slog.Error("Failed to load command line flags")
		os.Exit(1)
	}

	k.Load(env.Provider("KGE_", ".", func(key string) string {
		key = strings.ToLower(strings.TrimPrefix(key, "KGE_"))
		key = strings.ReplaceAll(key, "_", ".")
		return key
	}), nil)

	if err := k.Unmarshal("", &Cfg); err != nil {
		fmt.Printf("Error unmarshalling config: %v\n", err)
		os.Exit(1)
	}
}
