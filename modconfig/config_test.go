package modconfig

import (
	"bytes"
	"os"
	"testing"

	flag "github.com/spf13/pflag"
)

func Test_usage(t *testing.T) {
	var buf bytes.Buffer
	type args struct {
		f *flag.FlagSet
	}
	tests := []struct {
		name     string
		args     args
		expected string
	}{
		{
			name: "usage",
			args: args{
				f: flag.NewFlagSet("kopia-go-exporter", flag.ContinueOnError),
			},
			expected: "Usage:\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage(&buf, tt.args.f)
			got := buf.String()
			if got != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestGetVersionFull(t *testing.T) {
	givenVersion = "testVersion"
	tests := []struct {
		name        string
		wantVersion string
	}{
		{
			name:        "check VCS data",
			wantVersion: givenVersion,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVersion, _, _, _, gotOk := GetVersionFull()
			if gotVersion != tt.wantVersion {
				t.Errorf("GetVersionFull() gotVersion = %v, want %v", gotVersion, tt.wantVersion)
			}
			if !gotOk {
				t.Errorf("GetVersionFull() ok should be true")
			}
		})
	}
}

func Test_print_version(t *testing.T) {
	tests := []struct {
		name string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			print_version()
		})
	}
}

func TestCheckConfig(t *testing.T) {
	tests := []struct {
		name string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			CheckConfig()
		})
	}
}

func TestLoadConfig(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{oldArgs[0], "--config", "../config.yaml.sample"}
	type args struct {
		version string
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "Check some sample args",
			args: args{
				version: "build",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			LoadConfig(tt.args.version)
			if Cfg.Exporter.Port != 9090 {
				t.Errorf("LoadConfig() export.port should be set to 9090, got %v", Cfg.Exporter.Port)
			}
		})
	}
}
