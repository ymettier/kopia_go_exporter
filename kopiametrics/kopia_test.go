package kopiametrics

import (
	"context"
	"reflect"
	"testing"

	"github.com/kopia/kopia/repo"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestNewKopiaClient(t *testing.T) {
	tests := []struct {
		name string
		want *KopiaClient
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewKopiaClient(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewKopiaClient() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestKopiaClient_RegisterKopiaMetrics(t *testing.T) {
	metricNames := []string{
		"total_size",
		"file_count",
		"dir_count",
		"error_count",
		"backup_duration",
		"backup_start_time",
		"backup_end_time",
	}
	t.Run("All metrics are registered", func(t *testing.T) {
		k := &KopiaClient{}
		reg := prometheus.NewRegistry()
		k.RegisterKopiaMetrics(reg)

		for _, mn := range metricNames {
			collector := prometheus.NewGauge(prometheus.GaugeOpts{
				Name: mn,
				Help: "whatever",
			})
			err := reg.Register(collector)
			assert.Error(t, err, "Expected metric '%s' not found in registry", mn)
		}
	})
}

func TestKopiaClient_GenerateConfigFile(t *testing.T) {
	type fields struct {
		Ctx         context.Context
		IsConnected bool
		Opts        repo.ConnectOptions
		ServerInfo  repo.APIServerInfo
		Repo        repo.Repository
		Metrics     KopiaMetrics
	}
	tests := []struct {
		name   string
		fields fields
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &KopiaClient{
				Ctx:         tt.fields.Ctx,
				IsConnected: tt.fields.IsConnected,
				Opts:        tt.fields.Opts,
				ServerInfo:  tt.fields.ServerInfo,
				Repo:        tt.fields.Repo,
				Metrics:     tt.fields.Metrics,
			}
			k.GenerateConfigFile()
		})
	}
}

func TestKopiaClient_Connect(t *testing.T) {
	type fields struct {
		Ctx         context.Context
		IsConnected bool
		Opts        repo.ConnectOptions
		ServerInfo  repo.APIServerInfo
		Repo        repo.Repository
		Metrics     KopiaMetrics
	}
	tests := []struct {
		name   string
		fields fields
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &KopiaClient{
				Ctx:         tt.fields.Ctx,
				IsConnected: tt.fields.IsConnected,
				Opts:        tt.fields.Opts,
				ServerInfo:  tt.fields.ServerInfo,
				Repo:        tt.fields.Repo,
				Metrics:     tt.fields.Metrics,
			}
			k.Connect()
		})
	}
}

func TestKopiaClient_RunOnce(t *testing.T) {
	type fields struct {
		Ctx         context.Context
		IsConnected bool
		Opts        repo.ConnectOptions
		ServerInfo  repo.APIServerInfo
		Repo        repo.Repository
		Metrics     KopiaMetrics
	}
	tests := []struct {
		name   string
		fields fields
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &KopiaClient{
				Ctx:         tt.fields.Ctx,
				IsConnected: tt.fields.IsConnected,
				Opts:        tt.fields.Opts,
				ServerInfo:  tt.fields.ServerInfo,
				Repo:        tt.fields.Repo,
				Metrics:     tt.fields.Metrics,
			}
			k.RunOnce()
		})
	}
}

func TestKopiaClient_Disconnect(t *testing.T) {
	type fields struct {
		Ctx         context.Context
		IsConnected bool
		Opts        repo.ConnectOptions
		ServerInfo  repo.APIServerInfo
		Repo        repo.Repository
		Metrics     KopiaMetrics
	}
	tests := []struct {
		name   string
		fields fields
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &KopiaClient{
				Ctx:         tt.fields.Ctx,
				IsConnected: tt.fields.IsConnected,
				Opts:        tt.fields.Opts,
				ServerInfo:  tt.fields.ServerInfo,
				Repo:        tt.fields.Repo,
				Metrics:     tt.fields.Metrics,
			}
			k.Disconnect()
		})
	}
}

func Test_main(t *testing.T) {
	tests := []struct {
		name string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			main()
		})
	}
}
