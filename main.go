package main

import (
	_ "embed"
	"kopia-go-exporter/exporter"
	"kopia-go-exporter/modconfig"
)

//go:embed version.txt
var version string

func main() {
	modconfig.LoadConfig(version)

	logger := new_logger()
	logger.Debug().Msg("Debug logging enabled")

	exporter.Logger = logger
	ex := exporter.NewExporter()

	go ex.Run()
	select {} // block forever to keep main alive
}
