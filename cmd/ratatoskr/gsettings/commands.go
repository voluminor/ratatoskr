// Package gsettings parses the CLI utility command group.
package gsettings

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// // // // // // // // // //

// GoAskFormatEnum selects human-readable or JSON command output.
type GoAskFormatEnum string

const (
	// GoAskFormatText selects human-readable output.
	GoAskFormatText GoAskFormatEnum = "text"
	// GoAskFormatJson selects JSON output.
	GoAskFormatJson GoAskFormatEnum = "json"
)

// GoForwardProtoEnum selects the forwarding transport.
type GoForwardProtoEnum string

const (
	// GoForwardProtoTcp selects TCP forwarding.
	GoForwardProtoTcp GoForwardProtoEnum = "tcp"
	// GoForwardProtoUdp selects UDP forwarding.
	GoForwardProtoUdp GoForwardProtoEnum = "udp"
)

// GoConfFormatEnum selects a configuration encoding.
type GoConfFormatEnum string

// GoConfGeneratePresetEnum selects a generated configuration preset.
type GoConfGeneratePresetEnum string

const (
	// GoConfGeneratePresetBasic selects the smallest configuration.
	GoConfGeneratePresetBasic GoConfGeneratePresetEnum = "basic"
	// GoConfGeneratePresetMedium selects common optional settings.
	GoConfGeneratePresetMedium GoConfGeneratePresetEnum = "medium"
	// GoConfGeneratePresetFull selects every generated setting.
	GoConfGeneratePresetFull GoConfGeneratePresetEnum = "full"
)

// GoObj contains every parsed utility command group.
type GoObj struct {
	Key      GoKeyObj      // Key contains key utilities.
	Conf     GoConfObj     // Conf contains configuration utilities.
	Ask      GoAskObj      // Ask contains a NodeInfo query.
	PeerInfo GoPeerInfoObj // PeerInfo contains a peer inspection request.
	Forward  GoForwardObj  // Forward contains a forwarding request.
	Probe    GoProbeObj    // Probe contains topology and latency requests.
}

// GoKeyObj configures key utilities.
type GoKeyObj struct {
	Gen     time.Duration // Gen is the vanity-key search duration.
	Addr    string        // Addr is a key or key-file address input.
	ToPem   string        // ToPem is the PEM output path.
	FromPem string        // FromPem is the PEM input path.
}

// GoConfObj configures configuration utilities.
type GoConfObj struct {
	Generate GoConfGenerateObj // Generate contains generation options.
	Import   GoConfImportObj   // Import contains Yggdrasil import options.
	Export   GoConfExportObj   // Export contains Yggdrasil export options.
}

// GoConfGenerateObj configures configuration generation.
type GoConfGenerateObj struct {
	Path   string                   // Path is the output directory.
	Format GoConfFormatEnum         // Format is the output encoding.
	Preset GoConfGeneratePresetEnum // Preset selects included settings.
}

// GoConfImportObj configures Yggdrasil configuration import.
type GoConfImportObj struct {
	From   string           // From is the Yggdrasil configuration path.
	To     string           // To is the output directory.
	Format GoConfFormatEnum // Format is the output encoding.
}

// GoConfExportObj configures Yggdrasil configuration export.
type GoConfExportObj struct {
	From   string           // From is the Ratatoskr configuration path.
	To     string           // To is the output directory.
	Format GoConfFormatEnum // Format is the output encoding.
}

// GoAskObj configures a NodeInfo query.
type GoAskObj struct {
	Addr    string          // Addr identifies the remote node.
	Peer    []string        // Peer contains bootstrap peer URIs.
	Timeout time.Duration   // Timeout bounds the query.
	Format  GoAskFormatEnum // Format selects command output.
}

// GoPeerInfoObj configures peer inspection.
type GoPeerInfoObj struct {
	Peer    []string        // Peer contains inspected peer URIs.
	Timeout time.Duration   // Timeout bounds connection establishment.
	Format  GoAskFormatEnum // Format selects command output.
}

// GoForwardObj configures one forwarding command.
type GoForwardObj struct {
	From  string             // From is the local listener address.
	To    string             // To is the mapped address.
	Proto GoForwardProtoEnum // Proto selects TCP or UDP.
	Peer  []string           // Peer contains bootstrap peer URIs.
}

// GoProbeObj configures topology, trace, or latency probing.
type GoProbeObj struct {
	Scan        bool            // Scan requests a topology scan.
	Trace       string          // Trace is the destination public key.
	Ping        string          // Ping is the latency target public key.
	Peer        []string        // Peer contains bootstrap peer URIs.
	Timeout     time.Duration   // Timeout bounds probe operations.
	MaxDepth    uint16          // MaxDepth bounds topology traversal.
	Concurrency int             // Concurrency bounds parallel requests.
	Count       int             // Count is the number of latency samples.
	Format      GoAskFormatEnum // Format selects command output.
}

// IsCommandArgs reports whether args select the utility command group.
func IsCommandArgs(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-go.") || strings.HasPrefix(arg, "--go.") {
			return true
		}
	}
	return false
}

func stringSliceFlag(target *[]string) func(string) error {
	return func(value string) error {
		for _, item := range strings.Split(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				*target = append(*target, item)
			}
		}
		return nil
	}
}

func enumFlag[T ~string](target *T, allowed ...T) func(string) error {
	return func(value string) error {
		value = strings.ToLower(strings.TrimSpace(value))
		for _, candidate := range allowed {
			if value == string(candidate) {
				*target = candidate
				return nil
			}
		}
		return fmt.Errorf("invalid value %q", value)
	}
}

// Parse parses utility command arguments.
func Parse(args []string) (*GoObj, error) {
	obj := &GoObj{}
	obj.Ask.Format = GoAskFormatText
	obj.PeerInfo.Format = GoAskFormatText
	obj.Probe.Format = GoAskFormatText
	obj.Forward.Proto = GoForwardProtoTcp
	obj.Conf.Generate.Format = "yml"
	obj.Conf.Generate.Preset = GoConfGeneratePresetBasic
	obj.Conf.Import.Format = "yml"
	obj.Conf.Export.Format = "json"

	flags := flag.NewFlagSet("ratatoskr", flag.ContinueOnError)
	flags.DurationVar(&obj.Key.Gen, "go.key.gen", 0, "mine a private key for a duration")
	flags.StringVar(&obj.Key.Addr, "go.key.addr", "", "show address for a key")
	flags.StringVar(&obj.Key.ToPem, "go.key.to_pem", "", "write a private key as PEM")
	flags.StringVar(&obj.Key.FromPem, "go.key.from_pem", "", "read a private key from PEM")
	flags.StringVar(&obj.Conf.Generate.Path, "go.conf.generate.path", "", "generate config in directory")
	flags.Func("go.conf.generate.format", "yml, json, or conf", enumFlag(&obj.Conf.Generate.Format, GoConfFormatEnum("yml"), "json", "conf"))
	flags.Func("go.conf.generate.preset", "basic, medium, or full", enumFlag(&obj.Conf.Generate.Preset, GoConfGeneratePresetBasic, GoConfGeneratePresetMedium, GoConfGeneratePresetFull))
	flags.StringVar(&obj.Conf.Import.From, "go.conf.import.from", "", "Yggdrasil config to import")
	flags.StringVar(&obj.Conf.Import.To, "go.conf.import.to", "", "output directory")
	flags.Func("go.conf.import.format", "yml, json, or conf", enumFlag(&obj.Conf.Import.Format, GoConfFormatEnum("yml"), "json", "conf"))
	flags.StringVar(&obj.Conf.Export.From, "go.conf.export.from", "", "Ratatoskr config to export")
	flags.StringVar(&obj.Conf.Export.To, "go.conf.export.to", "", "output directory")
	flags.Func("go.conf.export.format", "yml, json, or conf", enumFlag(&obj.Conf.Export.Format, GoConfFormatEnum("yml"), "json", "conf"))
	flags.StringVar(&obj.Ask.Addr, "go.ask.addr", "", "NodeInfo target")
	flags.Func("go.ask.peer", "peer URI (repeatable or comma-separated)", stringSliceFlag(&obj.Ask.Peer))
	flags.DurationVar(&obj.Ask.Timeout, "go.ask.timeout", 0, "Ask timeout")
	flags.Func("go.ask.format", "text or json", enumFlag(&obj.Ask.Format, GoAskFormatText, GoAskFormatJson))
	flags.Func("go.peer_info.peer", "peer URI (repeatable or comma-separated)", stringSliceFlag(&obj.PeerInfo.Peer))
	flags.DurationVar(&obj.PeerInfo.Timeout, "go.peer_info.timeout", 0, "probe timeout")
	flags.Func("go.peer_info.format", "text or json", enumFlag(&obj.PeerInfo.Format, GoAskFormatText, GoAskFormatJson))
	flags.StringVar(&obj.Forward.From, "go.forward.from", "", "local listen address")
	flags.StringVar(&obj.Forward.To, "go.forward.to", "", "remote mapped address")
	flags.Func("go.forward.proto", "tcp or udp", enumFlag(&obj.Forward.Proto, GoForwardProtoTcp, GoForwardProtoUdp))
	flags.Func("go.forward.peer", "peer URI (repeatable or comma-separated)", stringSliceFlag(&obj.Forward.Peer))
	flags.BoolVar(&obj.Probe.Scan, "go.probe.scan", false, "scan topology")
	flags.StringVar(&obj.Probe.Trace, "go.probe.trace", "", "trace public key")
	flags.StringVar(&obj.Probe.Ping, "go.probe.ping", "", "ping public key")
	flags.Func("go.probe.peer", "peer URI (repeatable or comma-separated)", stringSliceFlag(&obj.Probe.Peer))
	flags.DurationVar(&obj.Probe.Timeout, "go.probe.timeout", 0, "probe timeout")
	flags.Func("go.probe.max_depth", "maximum scan depth", func(value string) error {
		parsed, err := strconv.ParseUint(value, 10, 16)
		obj.Probe.MaxDepth = uint16(parsed)
		return err
	})
	flags.IntVar(&obj.Probe.Concurrency, "go.probe.concurrency", 0, "parallel workers")
	flags.IntVar(&obj.Probe.Count, "go.probe.count", 0, "ping count")
	flags.Func("go.probe.format", "text or json", enumFlag(&obj.Probe.Format, GoAskFormatText, GoAskFormatJson))
	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	if flags.NArg() != 0 {
		return nil, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	return obj, nil
}
