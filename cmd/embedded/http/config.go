package main

import (
	"fmt"
	"os"
)

// // // // // // // // // //

type configObj struct {
	PrivateKey string   `yaml:"private_key"`
	Hostname   string   `yaml:"hostname"`
	Peers      []string `yaml:"peers"`
	HTTPPorts  []int    `yaml:"http_ports"`
	YggPorts   []int    `yaml:"ygg_ports"`
}

// //

func loadConfig(path string) (*configObj, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg configObj
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Hostname == "" {
		cfg.Hostname = "localhost"
	}
	if len(cfg.HTTPPorts) == 0 {
		cfg.HTTPPorts = []int{8080}
	}
	if len(cfg.YggPorts) == 0 {
		cfg.YggPorts = []int{8443}
	}
	return &cfg, nil
}
