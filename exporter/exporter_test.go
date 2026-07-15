// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package exporter

import (
	"context"
	"errors"
	"net"
	"net/http"
	"runtime/debug"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

	cfg := config.ExporterConfig{
		Port:    12346,
		Metrics: struct{ Prefix string }{Prefix: "test_prefix"}, //nolint:goconst
	}
	ex := NewExporter(cfg)
	require.NotNil(t, ex)
	assert.Equal(t, 12346, ex.Port)
	assert.NotNil(t, ex.Reg)
}

func TestNewExporter_BuildInfoUnavailable(t *testing.T) {
	origReadBuildInfo := config.ReadBuildInfo
	defer func() { config.ReadBuildInfo = origReadBuildInfo }()

	logger.Reset(nil)

	config.ReadBuildInfo = func() (*debug.BuildInfo, bool) {
		return nil, false
	}

	cfg := config.ExporterConfig{
		Port:    12347,
		Metrics: struct{ Prefix string }{Prefix: "test_prefix"},
	}
	ex := NewExporter(cfg)
	require.NotNil(t, ex)
}

func TestExporter_SetBuildInfo(t *testing.T) {
	cfg := config.ExporterConfig{
		Metrics: struct{ Prefix string }{Prefix: "test_prefix"},
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
				"version": "test_version", //nolint:goconst
				"commit":  "test_revision", //nolint:goconst
				"date":    "13:37", //nolint:goconst
			}
			ex.SetBuildInfo(labels["version"], labels["commit"], labels["date"])

			metrics, err := ex.Reg.Gather()
			require.NoError(t, err)

			familyMap := make(map[string]*prometheus.GaugeVec)
			for _, mFamily := range metrics {
				if mFamily.GetName() == "test_prefix_build_info" {
					familyMap[mFamily.GetName()] = nil
					for _, m := range mFamily.Metric {
						for _, label := range m.GetLabel() {
							switch label.GetName() {
							case "version":
								assert.Equal(t, labels["version"], label.GetValue())
							case "commit":
								assert.Equal(t, labels["commit"], label.GetValue())
							case "date":
								assert.Equal(t, labels["date"], label.GetValue())
							}
						}
						assert.Equal(t, float64(1), m.GetGauge().GetValue())
					}
				}
			}
			assert.Contains(t, familyMap, "test_prefix_build_info", "metric test_prefix_build_info was not found")
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

		time.Sleep(200 * time.Millisecond)

		resp, err := http.Get("http://localhost:12345/metrics")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		cancel()
	})
}

func TestExporter_Run_AlreadyInUse(t *testing.T) {
	logger.Reset(nil)

	blocker, err := net.Listen("tcp", ":12399")
	require.NoError(t, err)
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

type fakeShutdowner struct {
	err error
}

func (f fakeShutdowner) Shutdown(_ context.Context) error {
	return f.err
}

func TestShutdownServer(_ *testing.T) {
	shutdownServer(fakeShutdowner{}, 9090)
	shutdownServer(fakeShutdowner{err: errors.New("boom")}, 9090)
}
