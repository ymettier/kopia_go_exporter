// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package exporter

import (
	"context"
	"net"
	"net/http"
	"runtime/debug"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"kopia-go-exporter/config"
	"kopia-go-exporter/logger"
)

func TestNewExporter(t *testing.T) {
	origReadBuildInfo := config.ReadBuildInfo
	defer func() { config.ReadBuildInfo = origReadBuildInfo }()

	logger.Reset(nil)

	config.ReadBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			GoVersion: "go1.25.0",
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abc123"},
				{Key: "vcs.time", Value: "2025-01-15T10:00:00Z"},
			},
		}, true
	}

	cfg := config.Config{
		Exporter: config.ExporterConfig{
			Port:    12346,
			Metrics: struct{ Prefix string }{Prefix: "test_prefix"},
		},
	}
	ex := NewExporter(cfg)
	if ex == nil {
		t.Fatal("NewExporter() returned nil")
	}
	if ex.Port != 12346 {
		t.Errorf("NewExporter() Port = %v, want %v", ex.Port, 12346)
	}
	if ex.Reg == nil {
		t.Error("NewExporter() Reg is nil")
	}
}

func TestNewExporter_BuildInfoUnavailable(t *testing.T) {
	origReadBuildInfo := config.ReadBuildInfo
	defer func() { config.ReadBuildInfo = origReadBuildInfo }()

	logger.Reset(nil)

	config.ReadBuildInfo = func() (*debug.BuildInfo, bool) {
		return nil, false
	}

	cfg := config.Config{
		Exporter: config.ExporterConfig{
			Port:    12347,
			Metrics: struct{ Prefix string }{Prefix: "test_prefix"},
		},
	}
	ex := NewExporter(cfg)
	if ex == nil {
		t.Fatal("NewExporter() returned nil")
	}
}

func TestExporter_SetBuildInfo(t *testing.T) {
	cfg := config.Config{
		Exporter: config.ExporterConfig{
			Metrics: struct{ Prefix string }{Prefix: "test_prefix"},
		},
	}
	type fields struct {
		Port int
		Reg  *prometheus.Registry
	}
	tests := []struct {
		name   string
		fields fields
	}{
		{
			name: "test registry after setting build_info metric",
			fields: fields{
				Port: 9090,
				Reg:  prometheus.NewRegistry(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := &Exporter{
				Port: tt.fields.Port,
				Reg:  tt.fields.Reg,
				cfg:  cfg,
			}
			labels := map[string]string{
				"version": "test_version",
				"commit":  "test_revision",
				"date":    "13:37",
			}
			ex.SetBuildInfo(labels["version"], labels["commit"], labels["date"])

			// Gather all metrics from the registry
			metrics, err := ex.Reg.Gather()
			if err != nil {
				t.Fatalf("error gathering metrics: %v", err)
			}

			found := false
			foundVersion := false
			foundRevision := false
			foundDate := false
			for _, mFamily := range metrics {
				if mFamily.GetName() == "test_prefix_build_info" {
					found = true
					for _, m := range mFamily.Metric {
						for _, label := range m.Label {
							labelName := label.GetName()
							labelValue := label.GetValue()
							if labelName == "version" {
								foundVersion = true
								if labelValue != labels[labelName] {
									t.Errorf("Found metric build_info and label %v, but got %v, wanted %v", labelName, labelValue, labels[labelName])
								}
							}
							if labelName == "commit" {
								foundRevision = true
								if labelValue != labels[labelName] {
									t.Errorf("Found metric build_info and label %v, but got %v, wanted %v", labelName, labelValue, labels[labelName])
								}
							}
							if labelName == "date" {
								foundDate = true
								if labelValue != labels[labelName] {
									t.Errorf("Found metric build_info and label %v, but got %v, wanted %v", labelName, labelValue, labels[labelName])
								}
							}
						}
						value := m.Gauge.GetValue()
						if value != 1 {
							t.Errorf("Found metric build_info but got value %v, wanted %v", value, 1)
						}
					}
				}
			}
			if !found {
				t.Errorf("Metric build_info was not found")
			} else {
				if !foundVersion {
					t.Errorf("Found metric build_info but label %v is missing", "version")
				}
				if !foundRevision {
					t.Errorf("Found metric build_info but label %v is missing", "commit")
				}
				if !foundDate {
					t.Errorf("Found metric build_info but label %v is missing", "date")
				}
			}
		})
	}
}

func TestExporter_Run(t *testing.T) {
	t.Run("Test the exporter on port 12345", func(t *testing.T) {
		logger.Reset(nil)
		ctx, cancel := context.WithCancel(context.Background())
		ex := Exporter{
			Port: 12345,
			Reg:  prometheus.NewRegistry(),
		}

		go ex.Run(ctx)

		// Wait briefly for the server to start
		time.Sleep(200 * time.Millisecond)

		resp, err := http.Get("http://localhost:12345/metrics")
		if err != nil {
			t.Fatalf("could not connect to exporter: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("unexpected status code: got %v, want %v", resp.StatusCode, http.StatusOK)
		}
		cancel()
	})
}

func TestExporter_Run_AlreadyInUse(t *testing.T) {
	logger.Reset(nil)

	// Block the port first
	blocker, err := net.Listen("tcp", ":12399")
	if err != nil {
		t.Fatalf("could not bind port: %v", err)
	}
	defer func() { _ = blocker.Close() }()

	ex := Exporter{
		Port: 12399,
		Reg:  prometheus.NewRegistry(),
	}

	done := make(chan struct{})
	go func() {
		ex.Run(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after server error")
	}
}
