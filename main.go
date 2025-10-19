package main

import (
	"fmt"
	"log"
	"os"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	flag "github.com/spf13/pflag"
)

var k = koanf.New(".")

type Config struct {
	Server struct {
		Port int    `koanf:"port"`
		Name string `koanf:"name"`
	} `koanf:"server"`
	LogLevel string `koanf:"log_level"`
}

var Cfg Config

func main() {
	// Configure CLI
	f := flag.NewFlagSet("kopia-go-exporter", flag.ContinueOnError)
	f.String("config", "config.yaml", "Path to YAML config file")
	f.String("server.name", "kopia-go-exporter", "Name of the server")
	f.Int("server.port", 8080, "Port to run the server on")
	f.String("log_level", "info", "Log level (debug, info, warn, error)")

	f.Usage = func() {
		fmt.Fprintln(os.Stdout, "Usage:")
		f.PrintDefaults()
		os.Exit(0)
	}

	// Parse flags from CLI args
	if err := f.Parse(os.Args[1:]); err != nil {
		// Display help on parse errors (including --help)
		f.Usage()
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

	logger := new_logger()
	logger.Debug().Msg("Debug logging enabled")
}
