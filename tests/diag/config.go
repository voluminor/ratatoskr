package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// // // // // // // // // //

const (
	// Loopback defaults keep a directly launched binary unreachable from the
	// network; explicit config binds (e.g. docker compose) still override these.
	defaultHTTPListen   = "127.0.0.1:8080"
	defaultDebugListen  = "127.0.0.1:7070"
	defaultSOCKSListen  = "127.0.0.1:1080"
	defaultTCPEchoPort  = 18080
	defaultUDPEchoPort  = 18081
	defaultCloseTimeout = 10 * time.Second
)

// ConfigObj controls one diagnostic ratatoskr node.
type ConfigObj struct {
	Name          string   `json:"name"`
	Peers         []string `json:"peers"`
	IfMTU         uint64   `json:"if_mtu"`
	HTTPListen    string   `json:"http_listen"`
	DebugListen   string   `json:"debug_listen"`
	SOCKSListen   string   `json:"socks_listen"`
	SOCKSMaxConns int      `json:"socks_max_connections"`
	TCPEchoPort   uint16   `json:"tcp_echo_port"`
	UDPEchoPort   uint16   `json:"udp_echo_port"`
	ResultsDir    string   `json:"results_dir"`
	CloseTimeout  string   `json:"close_timeout"`
	// DebugEnabled gates the pprof/expvar debug listener; off by default.
	// May also be enabled at startup via the RTS_DIAG_DEBUG env var.
	DebugEnabled bool `json:"debug_enabled"`
}

func loadConfig(path string) (ConfigObj, time.Duration, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ConfigObj{}, 0, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg ConfigObj
	if err = json.Unmarshal(data, &cfg); err != nil {
		return ConfigObj{}, 0, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.Name == "" {
		cfg.Name = "ratatoskr-node"
	}
	if cfg.HTTPListen == "" {
		cfg.HTTPListen = defaultHTTPListen
	}
	if cfg.DebugListen == "" {
		cfg.DebugListen = defaultDebugListen
	}
	if cfg.SOCKSListen == "" {
		cfg.SOCKSListen = defaultSOCKSListen
	}
	if cfg.TCPEchoPort == 0 {
		cfg.TCPEchoPort = defaultTCPEchoPort
	}
	if cfg.UDPEchoPort == 0 {
		cfg.UDPEchoPort = defaultUDPEchoPort
	}
	if cfg.ResultsDir == "" {
		cfg.ResultsDir = "/data/results"
	}
	timeout := defaultCloseTimeout
	if cfg.CloseTimeout != "" {
		timeout, err = time.ParseDuration(cfg.CloseTimeout)
		if err != nil {
			return ConfigObj{}, 0, fmt.Errorf("parse core_stop_timeout: %w", err)
		}
	}
	return cfg, timeout, nil
}
