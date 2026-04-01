// Code generated using '_generate/settings'; DO NOT EDIT.
// Generation time: 2026-04-01T16:43:05Z

package settings

import (
	"errors"
	"fmt"
	"os"
)

// // // // // // // // // //

// ErrInfo is returned by Init when -i/-info flag is used.
var ErrInfo = errors.New("info requested")

// //

var helpText = `Usage: <program> [flags]

General:
  -h, -help                                   show this help message
  -i, -info                                   show application info
  -config                                     path to config file (json/yml/yaml/hjson/conf)


Go (executable commands):
  ---
  -go.ask.addr                                target address (64-char hex, <hex>.pk.ygg, [ipv6]:port, or bare IPv6) [trigger]
  -go.ask.format                              output format (default: text) [trigger]
  -go.ask.peer                                yggdrasil peer URIs (e.g. tcp://1.2.3.4:5678) [trigger]
  -go.ask.timeout                             response timeout (default: 30s) [trigger]
  -go.conf.export.format                      output file format (default: json) [trigger]
  -go.conf.export.from                        input ratatoskr config file path [trigger]
  -go.conf.export.to                          output directory path [trigger]
  -go.conf.generate.format                    output file format (default: yml) [trigger]
  -go.conf.generate.path                      output directory path (file is always ratatoskr-config.{format}) [trigger]
  -go.conf.generate.preset                    config preset (default: basic) [trigger]
  -go.conf.import.format                      output file format (default: yml) [trigger]
  -go.conf.import.from                        input Yggdrasil config file path [trigger]
  -go.conf.import.to                          output directory path [trigger]
  -go.forward.from                            local listen address (e.g. 127.0.0.1:8080) [trigger]
  -go.forward.peer                            yggdrasil peer URIs (e.g. tcp://1.2.3.4:5678) [trigger]
  -go.forward.proto                           protocol (default: tcp) [trigger]
  -go.forward.to                              remote Yggdrasil address:port (e.g. [200:abc::1]:8080) [trigger]
  -go.key.addr                                show Yggdrasil IPv6 address and subnet for a given key (hex private 128 chars, hex public 64 chars, or PEM file path) [trigger]
  -go.key.from_pem                            convert PEM file to hex private key; value is input file path [trigger]
  -go.key.gen                                 mine a private key with max leading zeros for the given duration (e.g. 10s, 1m; recommended ≥10s, enforced min 100ms) [trigger]
  -go.key.to_pem                              convert hex private key (128 chars) to PEM file; value is output file path [trigger]
  -go.peer_info.format                        output format (default: text) [trigger]
  -go.peer_info.peer                          yggdrasil peer URIs (e.g. tcp://1.2.3.4:5678) [trigger]
  -go.peer_info.timeout                       probe timeout per peer (default: 10s) [trigger]
  -go.probe.concurrency                       parallel workers for scan (default: 64) [trigger]
  -go.probe.count                             number of pings (default: 4) [trigger]
  -go.probe.format                            output format (default: text) [trigger]
  -go.probe.max_depth                         BFS max depth for scan (default: 3) [trigger]
  -go.probe.peer                              yggdrasil peer URIs (e.g. tcp://1.2.3.4:5678); when multiple are given, each is tried sequentially until one connects; the total timeout is shared across all attempts [trigger]
  -go.probe.ping                              ping a node by public key and measure RTT (64-char hex) [trigger]
  -go.probe.scan                              BFS network topology scan [trigger]
  -go.probe.timeout                           context timeout (default: 5m, min: 100ms) [trigger]
  -go.probe.trace                             trace route to target public key (64-char hex) [trigger]


Log (logging configuration):
  -log.compress                               compress rotated log files (default: true)
  -log.file_path                              log file path (auto-detected if empty)
  -log.format                                 console format [text, json] (default: text)
  -log.level.console                          console log level [debug, info, warn, error, fatal, panic, disabled] (default: debug)
  -log.level.file                             file log level [debug, info, warn, error, fatal, panic, disabled] (default: info)
  -log.max_age                                log file max age (days) (default: 30)
  -log.max_backups                            log file max backups (default: 3)
  -log.max_size                               log file max size (MB) (default: 32)
  -log.output                                 log output mode [console, file, both] (default: both)


Yggdrasil (Yggdrasil network node configuration):
  -yggdrasil.admin_listen                     admin socket listen address; 'none' to disable (default: none)
  -yggdrasil.allowed_public_keys              hex-encoded public keys allowed for incoming peering; empty → allow all
  -yggdrasil.core_stop_timeout                core.Stop() timeout (0 → unlimited) (default: 5s)
  -yggdrasil.if.mtu                           TUN interface MTU (min 1280) (default: 65535)
  -yggdrasil.if.name                          TUN interface name; 'auto', 'none', or specific name (default: none)
  -yggdrasil.inputs                           real externally reachable addresses (e.g. public IPs); optional, for internal use
  -yggdrasil.key.path                         path to private key file in PEM format (alternative to private_key)
  -yggdrasil.key.text                         hex-encoded Ed25519 private key (128 hex chars)
  -yggdrasil.listen                           listener addresses for incoming connections (e.g. tls://0.0.0.0:0)
  -yggdrasil.log_lookups                      log address lookups
  -yggdrasil.multicast.beacon                 advertise presence via multicast (default: true)
  -yggdrasil.multicast.listen                 listen for multicast advertisements (default: true)
  -yggdrasil.multicast.password               multicast peering password
  -yggdrasil.multicast.port                   multicast port (0 → default)
  -yggdrasil.multicast.priority               peer priority (lower = preferred)
  -yggdrasil.multicast.regex                  interface name regex for multicast discovery (default: .*)
  -yggdrasil.node.auto                        auto-populate NodeInfo; merges with info if set; returns error on key conflicts (default: true)
  -yggdrasil.node.info                        node metadata visible to the network
  -yggdrasil.node.privacy                     hide default nodeinfo (platform, architecture, version)
  -yggdrasil.peers.interface                  outbound peers bound to network interfaces
  -yggdrasil.peers.manager.batch_size         probing batch size (0/1 → all at once, ≥2 → sliding window)
  -yggdrasil.peers.manager.enable             when disabled, all peer URLs are passed directly to Yggdrasil Peers (default: true)
  -yggdrasil.peers.manager.max_per_proto      best peers per protocol (0/1 → one, -1 → passive mode)
  -yggdrasil.peers.manager.probe_timeout      probe connection timeout (default: 10s)
  -yggdrasil.peers.manager.refresh_interval   re-evaluation interval (0 → startup only)
  -yggdrasil.peers.url                        outbound peer URIs (e.g. tls://a.b.c.d:e, tcp://1.2.3.4:5678)
  -yggdrasil.rst_queue_size                   RST packet deferred queue size (default: 100)
  -yggdrasil.socks.addr                       listen address (TCP '127.0.0.1:1080' or Unix '/tmp/ygg.sock')
  -yggdrasil.socks.max_connections            max simultaneous connections (0 → unlimited)
`

// //

func printHelp() {
	fmt.Fprint(os.Stderr, helpText)
}

func printInfo(text string) {
	fmt.Fprintln(os.Stderr, text)
}
