package proxy

import (
	"github.com/etclabscore/core-pool/api"
	"github.com/etclabscore/core-pool/payouts"
	"github.com/etclabscore/core-pool/policy"
	"github.com/etclabscore/core-pool/storage"
	"time"
)

type Config struct {
	Name                  string        `json:"name"`
	RunLevel              string        `json:"runlevel"`
	MaxRoutine            int           `json:"maxRoutine"`
	Proxy                 Proxy         `json:"proxy"`
	Api                   api.ApiConfig `json:"api"`
	Upstream              []Upstream    `json:"upstream"`
	UpstreamCheckInterval string        `json:"upstreamCheckInterval"`

	Threads int `json:"threads"`

	Network string         `json:"network"`
	Coin    string         `json:"coin"`
	Redis   storage.Config `json:"redis"`

	BlockUnlocker payouts.UnlockerConfig `json:"unlocker"`
	Payouts       payouts.PayoutsConfig  `json:"payouts"`

	Logger Logger `json:"logger"`

	NewrelicName    string `json:"newrelicName"`
	NewrelicKey     string `json:"newrelicKey"`
	NewrelicVerbose bool   `json:"newrelicVerbose"`
	NewrelicEnabled bool   `json:"newrelicEnabled"`
}

type Proxy struct {
	Enabled              bool   `json:"enabled"`
	Listen               string `json:"listen"`
	LimitHeadersSize     int    `json:"limitHeadersSize"`
	LimitBodySize        int64  `json:"limitBodySize"`
	BehindReverseProxy   bool   `json:"behindReverseProxy"`
	BlockRefreshInterval string `json:"blockRefreshInterval"`
	Difficulty           int64  `json:"difficulty"`
	StateUpdateInterval  string `json:"stateUpdateInterval"`
	HashrateExpiration   string `json:"hashrateExpiration"`

	Policy policy.Config `json:"policy"`

	MaxFails    int64 `json:"maxFails"`
	HealthCheck bool  `json:"healthCheck"`

	Stratum Stratum `json:"stratum"`
}

type Stratum struct {
	Enabled  bool   `json:"enabled"`
	Listen   string `json:"listen"`
	Timeout  string `json:"timeout"`
	MaxConn  int    `json:"maxConn"`
	TLS      bool   `json:"tls"`
	CertFile string `json:"certFile"`
	KeyFile  string `json:"keyFile"`
}

type Upstream struct {
	Name    string `json:"name"`
	Url     string `json:"url"`
	Timeout string `json:"timeout"`
}

type Logger struct {
	LogPath     string        `json:"logPath"`
	ErrLogPath  string        `json:"errLogPath"`
	SaveDays    time.Duration `json:"saveDays"`
	CutInterval time.Duration `json:"cutInterval"`
}
