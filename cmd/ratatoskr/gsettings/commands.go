package gsettings

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type GoAskFormatEnum string

const (
	GoAskFormatText GoAskFormatEnum = "text"
	GoAskFormatJson GoAskFormatEnum = "json"
)

type GoForwardProtoEnum string

const (
	GoForwardProtoTcp GoForwardProtoEnum = "tcp"
	GoForwardProtoUdp GoForwardProtoEnum = "udp"
)

type GoConfFormatEnum string
type GoConfGeneratePresetEnum string

const (
	GoConfGeneratePresetBasic  GoConfGeneratePresetEnum = "basic"
	GoConfGeneratePresetMedium GoConfGeneratePresetEnum = "medium"
	GoConfGeneratePresetFull   GoConfGeneratePresetEnum = "full"
)

type GoObj struct {
	Key      GoKeyObj
	Conf     GoConfObj
	Ask      GoAskObj
	PeerInfo GoPeerInfoObj
	Forward  GoForwardObj
	Probe    GoProbeObj
}

type GoKeyObj struct {
	Gen     time.Duration
	Addr    string
	ToPem   string
	FromPem string
}

type GoConfObj struct {
	Generate GoConfGenerateObj
	Import   GoConfImportObj
	Export   GoConfExportObj
}

type GoConfGenerateObj struct {
	Path   string
	Format GoConfFormatEnum
	Preset GoConfGeneratePresetEnum
}

type GoConfImportObj struct {
	From   string
	To     string
	Format GoConfFormatEnum
}

type GoConfExportObj struct {
	From   string
	To     string
	Format GoConfFormatEnum
}

type GoAskObj struct {
	Addr    string
	Peer    []string
	Timeout time.Duration
	Format  GoAskFormatEnum
}

type GoPeerInfoObj struct {
	Peer    []string
	Timeout time.Duration
	Format  GoAskFormatEnum
}

type GoForwardObj struct {
	From  string
	To    string
	Proto GoForwardProtoEnum
	Peer  []string
}

type GoProbeObj struct {
	Scan        bool
	Trace       string
	Ping        string
	Peer        []string
	Timeout     time.Duration
	MaxDepth    uint16
	Concurrency int
	Count       int
	Format      GoAskFormatEnum
}

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
