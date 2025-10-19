package modconfig

import (
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	flag "github.com/spf13/pflag"
)

var k = koanf.New(".")

var givenVersion string

type Config struct {
	Exporter struct {
		Port    int    `koanf:"port"`
		Name    string `koanf:"name"`
		Metrics struct {
			Prefix string `koanf:"prefix"`
		} `koanf:"metrics"`
	} `koanf:"exporter"`
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

func LoadConfig(version string) {
	givenVersion = version

	// Configure CLI
	f := flag.NewFlagSet("kopia-go-exporter", flag.ContinueOnError)
	f.String("config", "config.yaml", "Path to YAML config file")
	f.String("exporter.name", "kopia-go-exporter", "Name of the exporter")
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
		log.Fatalf("Failed to load config file")
	}

	// Load CLI flags to override config
	if err := k.Load(posflag.Provider(f, ".", k), nil); err != nil {
		log.Fatalf("Failed to load command line flags")
	}

	if err := k.Unmarshal("", &Cfg); err != nil {
		fmt.Printf("Error unmarshalling config: %v\n", err)
		os.Exit(1)
	}
}
