// Copyright 2025-2026 The kopia-go-exporter Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package exporter

import (
	"context"
	"errors"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kopia-go-exporter/config"
	"kopia-go-exporter/logger"
)

// Constructs a new exporter with build info available and expects a
// non-nil exporter with the configured port and registry.
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

// Constructs a new exporter when build info is unavailable and expects a
// non-nil exporter to be returned.
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

// Sets the build_info metric and expects it to be registered with the
// provided version/commit/date labels and a value of 1.
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
				"version": "test_version",  //nolint:goconst
				"commit":  "test_revision", //nolint:goconst
				"date":    "13:37",         //nolint:goconst
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

// Starts the exporter HTTP server on a free port and expects a 200 OK
// response from the /metrics endpoint.
func TestExporter_Run(t *testing.T) {
	t.Run("Test the exporter on a free port", func(t *testing.T) {
		logger.Reset(nil)
		port := freePort(t)
		ctx, cancel := context.WithCancel(context.Background())
		ex := Exporter{
			Port: port,
			Reg:  prometheus.NewRegistry(),
		}

		go ex.Run(ctx)

		time.Sleep(200 * time.Millisecond)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:"+strconv.Itoa(port)+"/metrics", http.NoBody)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		cancel()
	})
}

// Starts the exporter on a port already bound by another listener and
// expects Run to return promptly with a server error.
func TestExporter_Run_AlreadyInUse(t *testing.T) {
	logger.Reset(nil)

	port := freePort(t)
	blocker, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:"+strconv.Itoa(port))
	require.NoError(t, err)
	defer func() { _ = blocker.Close() }()

	ex := Exporter{
		Port: port,
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

// freePort returns a currently-free TCP port by binding to :0 and
// releasing it.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

type fakeShutdowner struct {
	err             error
	called          bool
	captured        error
	capturedContext context.Context
}

func (f *fakeShutdowner) Shutdown(ctx context.Context) error {
	f.called = true
	f.captured = f.err
	f.capturedContext = ctx
	return f.err
}

// Shuts down a fake server and expects Shutdown to be called, errors to
// be observed, and a 5s timeout context to be passed.
func TestShutdownServer(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		f := &fakeShutdowner{}
		shutdownServer(f, 9090)
		assert.True(t, f.called, "Shutdown should be called")
		assert.NoError(t, f.captured)
	})

	t.Run("error is logged", func(t *testing.T) {
		f := &fakeShutdowner{err: errors.New("boom")} //nolint:err113
		shutdownServer(f, 9090)
		assert.True(t, f.called, "Shutdown should be called even on error")
		assert.Error(t, f.captured, "the shutdown error should be observed by the helper")
	})

	t.Run("context has timeout", func(t *testing.T) {
		f := &fakeShutdowner{}
		shutdownServer(f, 9090)
		require.NotNil(t, f.capturedContext, "context should be passed to Shutdown")
		deadline, ok := f.capturedContext.Deadline()
		assert.True(t, ok, "context should have a deadline")
		assert.WithinDuration(t, deadline, time.Now(), 6*time.Second,
			"deadline should be approximately now + 5s")
	})
}
