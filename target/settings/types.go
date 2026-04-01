// Code generated using '_generate/settings'; DO NOT EDIT.
// Generation time: 2026-04-01T16:43:05Z

package settings

import "time"

// // // // // // // // // //

type Obj struct {
	// Yggdrasil network node configuration
	Yggdrasil YggdrasilObj `json:"yggdrasil" yaml:"yggdrasil"`
	// logging configuration
	Log    LogObj `json:"log" yaml:"log"`
	Config string `json:"config" yaml:"config"`
	Go     GoObj  `json:"-" yaml:"-"`
}

// Yggdrasil network node configuration
type YggdrasilObj struct {
	// node private key; empty → auto-generated; if both set, path takes priority
	Key    YggdrasilKeyObj `json:"key" yaml:"key"`
	Listen []string        `json:"listen" yaml:"listen"`
	Inputs []string        `json:"inputs" yaml:"inputs"`
	// peer connections
	Peers             YggdrasilPeersObj `json:"peers" yaml:"peers"`
	AllowedPublicKeys []string          `json:"allowed_public_keys" yaml:"allowed_public_keys"`
	AdminListen       string            `json:"admin_listen" yaml:"admin_listen"`
	// TUN adapter
	If YggdrasilIfObj `json:"if" yaml:"if"`
	// node identity and metadata
	Node            YggdrasilNodeObj `json:"node" yaml:"node"`
	LogLookups      bool             `json:"log_lookups" yaml:"log_lookups"`
	CoreStopTimeout time.Duration    `json:"core_stop_timeout" yaml:"core_stop_timeout"`
	RstQueueSize    int              `json:"rst_queue_size" yaml:"rst_queue_size"`
	// multicast peer discovery
	Multicast YggdrasilMulticastObj `json:"multicast" yaml:"multicast"`
	// SOCKS5 proxy configuration
	Socks YggdrasilSocksObj `json:"socks" yaml:"socks"`
}

// node private key; empty → auto-generated; if both set, path takes priority
type YggdrasilKeyObj struct {
	Text string `json:"text" yaml:"text"`
	Path string `json:"path" yaml:"path"`
}

// peer connections
type YggdrasilPeersObj struct {
	Url       []string            `json:"url" yaml:"url"`
	Interface map[string][]string `json:"interface" yaml:"interface"`
	// smart peer manager (replaces standard Yggdrasil peering)
	Manager YggdrasilPeersManagerObj `json:"manager" yaml:"manager"`
}

// smart peer manager (replaces standard Yggdrasil peering)
type YggdrasilPeersManagerObj struct {
	Enable          bool          `json:"enable" yaml:"enable"`
	ProbeTimeout    time.Duration `json:"probe_timeout" yaml:"probe_timeout"`
	RefreshInterval time.Duration `json:"refresh_interval" yaml:"refresh_interval"`
	MaxPerProto     int           `json:"max_per_proto" yaml:"max_per_proto"`
	BatchSize       int           `json:"batch_size" yaml:"batch_size"`
}

// TUN adapter
type YggdrasilIfObj struct {
	Name string `json:"name" yaml:"name"`
	Mtu  uint64 `json:"mtu" yaml:"mtu"`
}

// node identity and metadata
type YggdrasilNodeObj struct {
	Info    map[string]any `json:"info" yaml:"info"`
	Privacy bool           `json:"privacy" yaml:"privacy"`
	Auto    bool           `json:"auto" yaml:"auto"`
}

// multicast peer discovery
type YggdrasilMulticastObj struct {
	Regex    string `json:"regex" yaml:"regex"`
	Beacon   bool   `json:"beacon" yaml:"beacon"`
	Listen   bool   `json:"listen" yaml:"listen"`
	Port     uint16 `json:"port" yaml:"port"`
	Priority uint16 `json:"priority" yaml:"priority"`
	Password string `json:"password" yaml:"password"`
}

// SOCKS5 proxy configuration
type YggdrasilSocksObj struct {
	Addr           string `json:"addr" yaml:"addr"`
	MaxConnections int    `json:"max_connections" yaml:"max_connections"`
}

// logging configuration
type LogObj struct {
	Compress bool            `json:"compress" yaml:"compress"`
	FilePath string          `json:"file_path" yaml:"file_path"`
	Format   GoAskFormatEnum `json:"format" yaml:"format"`
	// log level thresholds
	Level      LogLevelObj   `json:"level" yaml:"level"`
	MaxAge     int           `json:"max_age" yaml:"max_age"`
	MaxBackups int           `json:"max_backups" yaml:"max_backups"`
	MaxSize    int           `json:"max_size" yaml:"max_size"`
	Output     LogOutputEnum `json:"output" yaml:"output"`
}

func (o *LogObj) Self() any { return o }

// log level thresholds
type LogLevelObj struct {
	Console LogLevelConsoleEnum `json:"console" yaml:"console"`
	File    LogLevelConsoleEnum `json:"file" yaml:"file"`
}

func (o *LogLevelObj) Self() any { return o }

// executable commands
type GoObj struct {
	Ask      GoAskObj      `json:"-" yaml:"-"`
	Conf     GoConfObj     `json:"-" yaml:"-"`
	Forward  GoForwardObj  `json:"-" yaml:"-"`
	Key      GoKeyObj      `json:"-" yaml:"-"`
	PeerInfo GoPeerInfoObj `json:"-" yaml:"-"`
	Probe    GoProbeObj    `json:"-" yaml:"-"`
}

func (o *GoObj) Self() any { return o }

// query remote node's NodeInfo
type GoAskObj struct {
	Addr    string          `json:"-" yaml:"-"`
	Format  GoAskFormatEnum `json:"-" yaml:"-"`
	Peer    []string        `json:"-" yaml:"-"`
	Timeout time.Duration   `json:"-" yaml:"-"`
}

func (o *GoAskObj) Self() any { return o }

// configuration utilities
type GoConfObj struct {
	Export   GoConfExportObj   `json:"-" yaml:"-"`
	Generate GoConfGenerateObj `json:"-" yaml:"-"`
	Import   GoConfImportObj   `json:"-" yaml:"-"`
}

func (o *GoConfObj) Self() any { return o }

// convert ratatoskr config to Yggdrasil format
type GoConfExportObj struct {
	Format GoConfExportFormatEnum `json:"-" yaml:"-"`
	From   string                 `json:"-" yaml:"-"`
	To     string                 `json:"-" yaml:"-"`
}

func (o *GoConfExportObj) Self() any { return o }

// generate a default ratatoskr config file
type GoConfGenerateObj struct {
	Format GoConfExportFormatEnum   `json:"-" yaml:"-"`
	Path   string                   `json:"-" yaml:"-"`
	Preset GoConfGeneratePresetEnum `json:"-" yaml:"-"`
}

func (o *GoConfGenerateObj) Self() any { return o }

// convert Yggdrasil config to ratatoskr format
type GoConfImportObj struct {
	Format GoConfExportFormatEnum `json:"-" yaml:"-"`
	From   string                 `json:"-" yaml:"-"`
	To     string                 `json:"-" yaml:"-"`
}

func (o *GoConfImportObj) Self() any { return o }

// port forwarding through Yggdrasil
type GoForwardObj struct {
	From  string             `json:"-" yaml:"-"`
	Peer  []string           `json:"-" yaml:"-"`
	Proto GoForwardProtoEnum `json:"-" yaml:"-"`
	To    string             `json:"-" yaml:"-"`
}

func (o *GoForwardObj) Self() any { return o }

// key utilities
type GoKeyObj struct {
	Addr    string        `json:"-" yaml:"-"`
	FromPem string        `json:"-" yaml:"-"`
	Gen     time.Duration `json:"-" yaml:"-"`
	ToPem   string        `json:"-" yaml:"-"`
}

func (o *GoKeyObj) Self() any { return o }

// probe peers and report status
type GoPeerInfoObj struct {
	Format  GoAskFormatEnum `json:"-" yaml:"-"`
	Peer    []string        `json:"-" yaml:"-"`
	Timeout time.Duration   `json:"-" yaml:"-"`
}

func (o *GoPeerInfoObj) Self() any { return o }

type GoProbeObj struct {
	Concurrency int             `json:"-" yaml:"-"`
	Count       int             `json:"-" yaml:"-"`
	Format      GoAskFormatEnum `json:"-" yaml:"-"`
	MaxDepth    uint16          `json:"-" yaml:"-"`
	Peer        []string        `json:"-" yaml:"-"`
	Ping        string          `json:"-" yaml:"-"`
	Scan        bool            `json:"-" yaml:"-"`
	Timeout     time.Duration   `json:"-" yaml:"-"`
	Trace       string          `json:"-" yaml:"-"`
}

func (o *GoProbeObj) Self() any { return o }

// //

func (o *Obj) Self() any { return o }
