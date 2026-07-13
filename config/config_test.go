// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	"flag"
	"testing"
)

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantErr     bool
		wantErrHelp bool
	}{
		{
			name:    "default flags",
			args:    []string{},
			wantErr: false,
		},
		{
			name:        "version flag",
			args:        []string{"--version"},
			wantErr:     true,
			wantErrHelp: true,
		},
		{
			name:        "help flag",
			args:        []string{"--help"},
			wantErr:     true,
			wantErrHelp: true,
		},
		{
			name:    "custom config file",
			args:    []string{"--config", "custom.yaml"},
			wantErr: false,
		},
		{
			name:    "custom exporter port",
			args:    []string{"--exporter-port", "9090"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flags, err := ParseFlags("test", tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFlags() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErrHelp && err != flag.ErrHelp {
				t.Errorf("ParseFlags() error = %v, wantErrHelp flag.ErrHelp", err)
			}
			if !tt.wantErr {
				if flags.ConfigFile == "" {
					t.Errorf("ParseFlags() ConfigFile should not be empty")
				}
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

func TestCheckConfig(t *testing.T) {
	// Save original config
	origCfg := Cfg
	defer func() { Cfg = origCfg }()

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid config with API server",
			cfg: Config{
				Kopia: KopiaConfig{
					Password: "test",
					APIServer: APIServerConfig{
						RepositoryURL: "https://example.com:51515",
						Hostname:      "localhost",
						Username:      "kopia",
						Fingerprint:   "abc123",
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Cfg = tt.cfg
			// CheckConfig calls os.Exit(1) on validation failure,
			// so we can't easily test failure cases without subprocess testing
			CheckConfig()
		})
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "load sample config",
			args:    []string{"--config", "../config.yaml.sample"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := New("test", tt.args); (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && Cfg.Exporter.Port != 9090 {
				t.Errorf("New() exporter.port should be 9090, got %v", Cfg.Exporter.Port)
			}
		})
	}
}
