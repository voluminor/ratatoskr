// Code generated using '_generate/settings'; DO NOT EDIT.
// Generation time: 2026-04-01T16:43:05Z

package settings

// // // // // // // // // //

// FieldOrder maps a dotted prefix to its child keys in settings.yml document order.
// Root-level keys use an empty prefix.
var FieldOrder = map[string][]string{
	"":                        {"yggdrasil", "log"},
	"yggdrasil":               {"key", "listen", "inputs", "peers", "allowed_public_keys", "admin_listen", "if", "node", "log_lookups", "core_stop_timeout", "rst_queue_size", "multicast", "socks"},
	"yggdrasil.key":           {"text", "path"},
	"yggdrasil.peers":         {"url", "interface", "manager"},
	"yggdrasil.peers.manager": {"enable", "probe_timeout", "refresh_interval", "max_per_proto", "batch_size"},
	"yggdrasil.if":            {"name", "mtu"},
	"yggdrasil.node":          {"info", "privacy", "auto"},
	"yggdrasil.multicast":     {"regex", "beacon", "listen", "port", "priority", "password"},
	"yggdrasil.socks":         {"addr", "max_connections"},
	"log":                     {"compress", "file_path", "format", "level", "max_age", "max_backups", "max_size", "output"},
	"log.level":               {"console", "file"},
}
