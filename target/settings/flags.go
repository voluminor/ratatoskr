// Code generated using '_generate/settings'; DO NOT EDIT.
// Generation time: 2026-04-01T16:43:05Z

package settings

import (
	"encoding/json"
	"flag"
	"fmt"
	"strconv"
	"strings"
)

// // // // // // // // // //

// CustomFlagTargetsObj holds raw string values for flags that need custom parsing.
type CustomFlagTargetsObj struct {
	GoAskFormat                string
	GoAskPeer                  string
	GoConfExportFormat         string
	GoConfGenerateFormat       string
	GoConfGeneratePreset       string
	GoConfImportFormat         string
	GoForwardPeer              string
	GoForwardProto             string
	GoPeerInfoFormat           string
	GoPeerInfoPeer             string
	GoProbeFormat              string
	GoProbeMaxDepth            string
	GoProbePeer                string
	LogFormat                  string
	LogLevelConsole            string
	LogLevelFile               string
	LogOutput                  string
	YggdrasilAllowedPublicKeys string
	YggdrasilInputs            string
	YggdrasilListen            string
	YggdrasilMulticastPort     string
	YggdrasilMulticastPriority string
	YggdrasilNodeInfo          string
	YggdrasilPeersInterface    string
	YggdrasilPeersUrl          string
}

// DefineFlags registers all settings as CLI flags on the given FlagSet.
func DefineFlags(fs *flag.FlagSet, obj *Obj, targets *CustomFlagTargetsObj) {
	fs.StringVar(&obj.Config, "config", obj.Config, "path to config file (json/yml/yaml/hjson/conf)")
	fs.StringVar(&obj.Go.Ask.Addr, "go.ask.addr", obj.Go.Ask.Addr, "target address (64-char hex, <hex>.pk.ygg, [ipv6]:port, or bare IPv6)")
	fs.StringVar(&targets.GoAskFormat, "go.ask.format", obj.Go.Ask.Format.String(), "output format (default: text)")
	fs.StringVar(&targets.GoAskPeer, "go.ask.peer", "", "yggdrasil peer URIs (e.g. tcp://1.2.3.4:5678)")
	fs.DurationVar(&obj.Go.Ask.Timeout, "go.ask.timeout", obj.Go.Ask.Timeout, "response timeout (default: 30s)")
	fs.StringVar(&targets.GoConfExportFormat, "go.conf.export.format", obj.Go.Conf.Export.Format.String(), "output file format (default: json)")
	fs.StringVar(&obj.Go.Conf.Export.From, "go.conf.export.from", obj.Go.Conf.Export.From, "input ratatoskr config file path")
	fs.StringVar(&obj.Go.Conf.Export.To, "go.conf.export.to", obj.Go.Conf.Export.To, "output directory path")
	fs.StringVar(&targets.GoConfGenerateFormat, "go.conf.generate.format", obj.Go.Conf.Generate.Format.String(), "output file format (default: yml)")
	fs.StringVar(&obj.Go.Conf.Generate.Path, "go.conf.generate.path", obj.Go.Conf.Generate.Path, "output directory path (file is always ratatoskr-config.{format})")
	fs.StringVar(&targets.GoConfGeneratePreset, "go.conf.generate.preset", obj.Go.Conf.Generate.Preset.String(), "config preset (default: basic)")
	fs.StringVar(&targets.GoConfImportFormat, "go.conf.import.format", obj.Go.Conf.Import.Format.String(), "output file format (default: yml)")
	fs.StringVar(&obj.Go.Conf.Import.From, "go.conf.import.from", obj.Go.Conf.Import.From, "input Yggdrasil config file path")
	fs.StringVar(&obj.Go.Conf.Import.To, "go.conf.import.to", obj.Go.Conf.Import.To, "output directory path")
	fs.StringVar(&obj.Go.Forward.From, "go.forward.from", obj.Go.Forward.From, "local listen address (e.g. 127.0.0.1:8080)")
	fs.StringVar(&targets.GoForwardPeer, "go.forward.peer", "", "yggdrasil peer URIs (e.g. tcp://1.2.3.4:5678)")
	fs.StringVar(&targets.GoForwardProto, "go.forward.proto", obj.Go.Forward.Proto.String(), "protocol (default: tcp)")
	fs.StringVar(&obj.Go.Forward.To, "go.forward.to", obj.Go.Forward.To, "remote Yggdrasil address:port (e.g. [200:abc::1]:8080)")
	fs.StringVar(&obj.Go.Key.Addr, "go.key.addr", obj.Go.Key.Addr, "show Yggdrasil IPv6 address and subnet for a given key (hex private 128 chars, hex public 64 chars, or PEM file path)")
	fs.StringVar(&obj.Go.Key.FromPem, "go.key.from_pem", obj.Go.Key.FromPem, "convert PEM file to hex private key; value is input file path")
	fs.DurationVar(&obj.Go.Key.Gen, "go.key.gen", obj.Go.Key.Gen, "mine a private key with max leading zeros for the given duration (e.g. 10s, 1m; recommended ≥10s, enforced min 100ms)")
	fs.StringVar(&obj.Go.Key.ToPem, "go.key.to_pem", obj.Go.Key.ToPem, "convert hex private key (128 chars) to PEM file; value is output file path")
	fs.StringVar(&targets.GoPeerInfoFormat, "go.peer_info.format", obj.Go.PeerInfo.Format.String(), "output format (default: text)")
	fs.StringVar(&targets.GoPeerInfoPeer, "go.peer_info.peer", "", "yggdrasil peer URIs (e.g. tcp://1.2.3.4:5678)")
	fs.DurationVar(&obj.Go.PeerInfo.Timeout, "go.peer_info.timeout", obj.Go.PeerInfo.Timeout, "probe timeout per peer (default: 10s)")
	fs.IntVar(&obj.Go.Probe.Concurrency, "go.probe.concurrency", obj.Go.Probe.Concurrency, "parallel workers for scan (default: 64)")
	fs.IntVar(&obj.Go.Probe.Count, "go.probe.count", obj.Go.Probe.Count, "number of pings (default: 4)")
	fs.StringVar(&targets.GoProbeFormat, "go.probe.format", obj.Go.Probe.Format.String(), "output format (default: text)")
	fs.StringVar(&targets.GoProbeMaxDepth, "go.probe.max_depth", fmt.Sprint(obj.Go.Probe.MaxDepth), "BFS max depth for scan (default: 3)")
	fs.StringVar(&targets.GoProbePeer, "go.probe.peer", "", "yggdrasil peer URIs (e.g. tcp://1.2.3.4:5678); when multiple are given, each is tried sequentially until one connects; the total timeout is shared across all attempts")
	fs.StringVar(&obj.Go.Probe.Ping, "go.probe.ping", obj.Go.Probe.Ping, "ping a node by public key and measure RTT (64-char hex)")
	fs.BoolVar(&obj.Go.Probe.Scan, "go.probe.scan", obj.Go.Probe.Scan, "BFS network topology scan")
	fs.DurationVar(&obj.Go.Probe.Timeout, "go.probe.timeout", obj.Go.Probe.Timeout, "context timeout (default: 5m, min: 100ms)")
	fs.StringVar(&obj.Go.Probe.Trace, "go.probe.trace", obj.Go.Probe.Trace, "trace route to target public key (64-char hex)")
	fs.BoolVar(&obj.Log.Compress, "log.compress", obj.Log.Compress, "compress rotated log files")
	fs.StringVar(&obj.Log.FilePath, "log.file_path", obj.Log.FilePath, "log file path (auto-detected if empty)")
	fs.StringVar(&targets.LogFormat, "log.format", obj.Log.Format.String(), "console format")
	fs.StringVar(&targets.LogLevelConsole, "log.level.console", obj.Log.Level.Console.String(), "console log level")
	fs.StringVar(&targets.LogLevelFile, "log.level.file", obj.Log.Level.File.String(), "file log level")
	fs.IntVar(&obj.Log.MaxAge, "log.max_age", obj.Log.MaxAge, "log file max age (days)")
	fs.IntVar(&obj.Log.MaxBackups, "log.max_backups", obj.Log.MaxBackups, "log file max backups")
	fs.IntVar(&obj.Log.MaxSize, "log.max_size", obj.Log.MaxSize, "log file max size (MB)")
	fs.StringVar(&targets.LogOutput, "log.output", obj.Log.Output.String(), "log output mode")
	fs.StringVar(&obj.Yggdrasil.AdminListen, "yggdrasil.admin_listen", obj.Yggdrasil.AdminListen, "admin socket listen address; 'none' to disable")
	fs.StringVar(&targets.YggdrasilAllowedPublicKeys, "yggdrasil.allowed_public_keys", "", "hex-encoded public keys allowed for incoming peering; empty → allow all")
	fs.DurationVar(&obj.Yggdrasil.CoreStopTimeout, "yggdrasil.core_stop_timeout", obj.Yggdrasil.CoreStopTimeout, "core.Stop() timeout (0 → unlimited)")
	fs.Uint64Var(&obj.Yggdrasil.If.Mtu, "yggdrasil.if.mtu", obj.Yggdrasil.If.Mtu, "TUN interface MTU (min 1280)")
	fs.StringVar(&obj.Yggdrasil.If.Name, "yggdrasil.if.name", obj.Yggdrasil.If.Name, "TUN interface name; 'auto', 'none', or specific name")
	fs.StringVar(&targets.YggdrasilInputs, "yggdrasil.inputs", "", "real externally reachable addresses (e.g. public IPs); optional, for internal use")
	fs.StringVar(&obj.Yggdrasil.Key.Path, "yggdrasil.key.path", obj.Yggdrasil.Key.Path, "path to private key file in PEM format (alternative to private_key)")
	fs.StringVar(&obj.Yggdrasil.Key.Text, "yggdrasil.key.text", obj.Yggdrasil.Key.Text, "hex-encoded Ed25519 private key (128 hex chars)")
	fs.StringVar(&targets.YggdrasilListen, "yggdrasil.listen", "", "listener addresses for incoming connections (e.g. tls://0.0.0.0:0)")
	fs.BoolVar(&obj.Yggdrasil.LogLookups, "yggdrasil.log_lookups", obj.Yggdrasil.LogLookups, "log address lookups")
	fs.BoolVar(&obj.Yggdrasil.Multicast.Beacon, "yggdrasil.multicast.beacon", obj.Yggdrasil.Multicast.Beacon, "advertise presence via multicast")
	fs.BoolVar(&obj.Yggdrasil.Multicast.Listen, "yggdrasil.multicast.listen", obj.Yggdrasil.Multicast.Listen, "listen for multicast advertisements")
	fs.StringVar(&obj.Yggdrasil.Multicast.Password, "yggdrasil.multicast.password", obj.Yggdrasil.Multicast.Password, "multicast peering password")
	fs.StringVar(&targets.YggdrasilMulticastPort, "yggdrasil.multicast.port", fmt.Sprint(obj.Yggdrasil.Multicast.Port), "multicast port (0 → default)")
	fs.StringVar(&targets.YggdrasilMulticastPriority, "yggdrasil.multicast.priority", fmt.Sprint(obj.Yggdrasil.Multicast.Priority), "peer priority (lower = preferred)")
	fs.StringVar(&obj.Yggdrasil.Multicast.Regex, "yggdrasil.multicast.regex", obj.Yggdrasil.Multicast.Regex, "interface name regex for multicast discovery")
	fs.BoolVar(&obj.Yggdrasil.Node.Auto, "yggdrasil.node.auto", obj.Yggdrasil.Node.Auto, "auto-populate NodeInfo; merges with info if set; returns error on key conflicts")
	fs.StringVar(&targets.YggdrasilNodeInfo, "yggdrasil.node.info", "", "node metadata visible to the network")
	fs.BoolVar(&obj.Yggdrasil.Node.Privacy, "yggdrasil.node.privacy", obj.Yggdrasil.Node.Privacy, "hide default nodeinfo (platform, architecture, version)")
	fs.StringVar(&targets.YggdrasilPeersInterface, "yggdrasil.peers.interface", "", "outbound peers bound to network interfaces")
	fs.IntVar(&obj.Yggdrasil.Peers.Manager.BatchSize, "yggdrasil.peers.manager.batch_size", obj.Yggdrasil.Peers.Manager.BatchSize, "probing batch size (0/1 → all at once, ≥2 → sliding window)")
	fs.BoolVar(&obj.Yggdrasil.Peers.Manager.Enable, "yggdrasil.peers.manager.enable", obj.Yggdrasil.Peers.Manager.Enable, "when disabled, all peer URLs are passed directly to Yggdrasil Peers")
	fs.IntVar(&obj.Yggdrasil.Peers.Manager.MaxPerProto, "yggdrasil.peers.manager.max_per_proto", obj.Yggdrasil.Peers.Manager.MaxPerProto, "best peers per protocol (0/1 → one, -1 → passive mode)")
	fs.DurationVar(&obj.Yggdrasil.Peers.Manager.ProbeTimeout, "yggdrasil.peers.manager.probe_timeout", obj.Yggdrasil.Peers.Manager.ProbeTimeout, "probe connection timeout")
	fs.DurationVar(&obj.Yggdrasil.Peers.Manager.RefreshInterval, "yggdrasil.peers.manager.refresh_interval", obj.Yggdrasil.Peers.Manager.RefreshInterval, "re-evaluation interval (0 → startup only)")
	fs.StringVar(&targets.YggdrasilPeersUrl, "yggdrasil.peers.url", "", "outbound peer URIs (e.g. tls://a.b.c.d:e, tcp://1.2.3.4:5678)")
	fs.IntVar(&obj.Yggdrasil.RstQueueSize, "yggdrasil.rst_queue_size", obj.Yggdrasil.RstQueueSize, "RST packet deferred queue size")
	fs.StringVar(&obj.Yggdrasil.Socks.Addr, "yggdrasil.socks.addr", obj.Yggdrasil.Socks.Addr, "listen address (TCP '127.0.0.1:1080' or Unix '/tmp/ygg.sock')")
	fs.IntVar(&obj.Yggdrasil.Socks.MaxConnections, "yggdrasil.socks.max_connections", obj.Yggdrasil.Socks.MaxConnections, "max simultaneous connections (0 → unlimited)")
}

// //

// ApplyCustomFlags converts raw string flag values into typed struct fields.
// Only applies values for flags that were explicitly set via CLI.
func ApplyCustomFlags(fs *flag.FlagSet, targets *CustomFlagTargetsObj, obj *Obj) error {
	var err error
	_ = err

	fs.Visit(func(f *flag.Flag) {
		if err != nil {
			return
		}
		switch f.Name {
		case "go.ask.format":
			var v GoAskFormatEnum
			v, err = ParseGoAskFormatEnum(targets.GoAskFormat)
			if err != nil {
				return
			}
			obj.Go.Ask.Format = v
		case "go.ask.peer":
			parts := strings.Split(targets.GoAskPeer, ",")
			obj.Go.Ask.Peer = make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				obj.Go.Ask.Peer = append(obj.Go.Ask.Peer, p)
			}
		case "go.conf.export.format":
			var v GoConfExportFormatEnum
			v, err = ParseGoConfExportFormatEnum(targets.GoConfExportFormat)
			if err != nil {
				return
			}
			obj.Go.Conf.Export.Format = v
		case "go.conf.generate.format":
			var v GoConfExportFormatEnum
			v, err = ParseGoConfExportFormatEnum(targets.GoConfGenerateFormat)
			if err != nil {
				return
			}
			obj.Go.Conf.Generate.Format = v
		case "go.conf.generate.preset":
			var v GoConfGeneratePresetEnum
			v, err = ParseGoConfGeneratePresetEnum(targets.GoConfGeneratePreset)
			if err != nil {
				return
			}
			obj.Go.Conf.Generate.Preset = v
		case "go.conf.import.format":
			var v GoConfExportFormatEnum
			v, err = ParseGoConfExportFormatEnum(targets.GoConfImportFormat)
			if err != nil {
				return
			}
			obj.Go.Conf.Import.Format = v
		case "go.forward.peer":
			parts := strings.Split(targets.GoForwardPeer, ",")
			obj.Go.Forward.Peer = make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				obj.Go.Forward.Peer = append(obj.Go.Forward.Peer, p)
			}
		case "go.forward.proto":
			var v GoForwardProtoEnum
			v, err = ParseGoForwardProtoEnum(targets.GoForwardProto)
			if err != nil {
				return
			}
			obj.Go.Forward.Proto = v
		case "go.peer_info.format":
			var v GoAskFormatEnum
			v, err = ParseGoAskFormatEnum(targets.GoPeerInfoFormat)
			if err != nil {
				return
			}
			obj.Go.PeerInfo.Format = v
		case "go.peer_info.peer":
			parts := strings.Split(targets.GoPeerInfoPeer, ",")
			obj.Go.PeerInfo.Peer = make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				obj.Go.PeerInfo.Peer = append(obj.Go.PeerInfo.Peer, p)
			}
		case "go.probe.format":
			var v GoAskFormatEnum
			v, err = ParseGoAskFormatEnum(targets.GoProbeFormat)
			if err != nil {
				return
			}
			obj.Go.Probe.Format = v
		case "go.probe.max_depth":
			var uv uint64
			uv, err = strconv.ParseUint(targets.GoProbeMaxDepth, 10, 16)
			if err != nil {
				return
			}
			obj.Go.Probe.MaxDepth = uint16(uv)
		case "go.probe.peer":
			parts := strings.Split(targets.GoProbePeer, ",")
			obj.Go.Probe.Peer = make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				obj.Go.Probe.Peer = append(obj.Go.Probe.Peer, p)
			}
		case "log.format":
			var v GoAskFormatEnum
			v, err = ParseGoAskFormatEnum(targets.LogFormat)
			if err != nil {
				return
			}
			obj.Log.Format = v
		case "log.level.console":
			var v LogLevelConsoleEnum
			v, err = ParseLogLevelConsoleEnum(targets.LogLevelConsole)
			if err != nil {
				return
			}
			obj.Log.Level.Console = v
		case "log.level.file":
			var v LogLevelConsoleEnum
			v, err = ParseLogLevelConsoleEnum(targets.LogLevelFile)
			if err != nil {
				return
			}
			obj.Log.Level.File = v
		case "log.output":
			var v LogOutputEnum
			v, err = ParseLogOutputEnum(targets.LogOutput)
			if err != nil {
				return
			}
			obj.Log.Output = v
		case "yggdrasil.allowed_public_keys":
			parts := strings.Split(targets.YggdrasilAllowedPublicKeys, ",")
			obj.Yggdrasil.AllowedPublicKeys = make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				obj.Yggdrasil.AllowedPublicKeys = append(obj.Yggdrasil.AllowedPublicKeys, p)
			}
		case "yggdrasil.inputs":
			parts := strings.Split(targets.YggdrasilInputs, ",")
			obj.Yggdrasil.Inputs = make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				obj.Yggdrasil.Inputs = append(obj.Yggdrasil.Inputs, p)
			}
		case "yggdrasil.listen":
			parts := strings.Split(targets.YggdrasilListen, ",")
			obj.Yggdrasil.Listen = make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				obj.Yggdrasil.Listen = append(obj.Yggdrasil.Listen, p)
			}
		case "yggdrasil.multicast.port":
			var uv uint64
			uv, err = strconv.ParseUint(targets.YggdrasilMulticastPort, 10, 16)
			if err != nil {
				return
			}
			obj.Yggdrasil.Multicast.Port = uint16(uv)
		case "yggdrasil.multicast.priority":
			var uv uint64
			uv, err = strconv.ParseUint(targets.YggdrasilMulticastPriority, 10, 16)
			if err != nil {
				return
			}
			obj.Yggdrasil.Multicast.Priority = uint16(uv)
		case "yggdrasil.node.info":
			if s := targets.YggdrasilNodeInfo; s != "" {
				err = json.Unmarshal([]byte(s), &obj.Yggdrasil.Node.Info)
				if err != nil {
					return
				}
			}
		case "yggdrasil.peers.interface":
			if s := targets.YggdrasilPeersInterface; s != "" {
				err = json.Unmarshal([]byte(s), &obj.Yggdrasil.Peers.Interface)
				if err != nil {
					return
				}
			}
		case "yggdrasil.peers.url":
			parts := strings.Split(targets.YggdrasilPeersUrl, ",")
			obj.Yggdrasil.Peers.Url = make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				obj.Yggdrasil.Peers.Url = append(obj.Yggdrasil.Peers.Url, p)
			}
		}
	})

	return err
}
