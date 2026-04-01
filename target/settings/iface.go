// Code generated using '_generate/settings'; DO NOT EDIT.
// Generation time: 2026-04-01T16:43:05Z

package settings

import "time"

// // // // // // // // // //
func (o *Obj) GetYggdrasil() YggdrasilInterface { return &o.Yggdrasil }
func (o *Obj) GetLog() *LogObj                  { return &o.Log }
func (o *Obj) GetConfig() string                { return o.Config }
func (o *Obj) GetGo() *GoObj                    { return &o.Go }

func (o *YggdrasilObj) GetKey() YggdrasilKeyInterface                   { return &o.Key }
func (o *YggdrasilObj) GetListen() []string                             { return o.Listen }
func (o *YggdrasilObj) GetInputs() []string                             { return o.Inputs }
func (o *YggdrasilObj) GetPeers() YggdrasilPeersInterface               { return &o.Peers }
func (o *YggdrasilObj) GetAllowedPublicKeys() []string                  { return o.AllowedPublicKeys }
func (o *YggdrasilObj) GetAdminListen() string                          { return o.AdminListen }
func (o *YggdrasilObj) GetIf() YggdrasilIfInterface                     { return &o.If }
func (o *YggdrasilObj) GetNode() YggdrasilNodeInterface                 { return &o.Node }
func (o *YggdrasilObj) GetLogLookups() bool                             { return o.LogLookups }
func (o *YggdrasilObj) GetCoreStopTimeout() time.Duration               { return o.CoreStopTimeout }
func (o *YggdrasilObj) GetRstQueueSize() int                            { return o.RstQueueSize }
func (o *YggdrasilObj) GetMulticast() YggdrasilMulticastInterface       { return &o.Multicast }
func (o *YggdrasilObj) GetSocks() YggdrasilSocksInterface               { return &o.Socks }
func (o *YggdrasilKeyObj) GetText() string                              { return o.Text }
func (o *YggdrasilKeyObj) GetPath() string                              { return o.Path }
func (o *YggdrasilPeersObj) GetUrl() []string                           { return o.Url }
func (o *YggdrasilPeersObj) GetInterface() map[string][]string          { return o.Interface }
func (o *YggdrasilPeersObj) GetManager() YggdrasilPeersManagerInterface { return &o.Manager }
func (o *YggdrasilPeersManagerObj) GetEnable() bool                     { return o.Enable }
func (o *YggdrasilPeersManagerObj) GetProbeTimeout() time.Duration      { return o.ProbeTimeout }
func (o *YggdrasilPeersManagerObj) GetRefreshInterval() time.Duration   { return o.RefreshInterval }
func (o *YggdrasilPeersManagerObj) GetMaxPerProto() int                 { return o.MaxPerProto }
func (o *YggdrasilPeersManagerObj) GetBatchSize() int                   { return o.BatchSize }
func (o *YggdrasilIfObj) GetName() string                               { return o.Name }
func (o *YggdrasilIfObj) GetMtu() uint64                                { return o.Mtu }
func (o *YggdrasilNodeObj) GetInfo() map[string]any                     { return o.Info }
func (o *YggdrasilNodeObj) GetPrivacy() bool                            { return o.Privacy }
func (o *YggdrasilNodeObj) GetAuto() bool                               { return o.Auto }
func (o *YggdrasilMulticastObj) GetRegex() string                       { return o.Regex }
func (o *YggdrasilMulticastObj) GetBeacon() bool                        { return o.Beacon }
func (o *YggdrasilMulticastObj) GetListen() bool                        { return o.Listen }
func (o *YggdrasilMulticastObj) GetPort() uint16                        { return o.Port }
func (o *YggdrasilMulticastObj) GetPriority() uint16                    { return o.Priority }
func (o *YggdrasilMulticastObj) GetPassword() string                    { return o.Password }
func (o *YggdrasilSocksObj) GetAddr() string                            { return o.Addr }
func (o *YggdrasilSocksObj) GetMaxConnections() int                     { return o.MaxConnections }
func (o *LogObj) GetCompress() bool                                     { return o.Compress }
func (o *LogObj) GetFilePath() string                                   { return o.FilePath }
func (o *LogObj) GetFormat() GoAskFormatEnum                            { return o.Format }
func (o *LogObj) GetLevel() *LogLevelObj                                { return &o.Level }
func (o *LogObj) GetMaxAge() int                                        { return o.MaxAge }
func (o *LogObj) GetMaxBackups() int                                    { return o.MaxBackups }
func (o *LogObj) GetMaxSize() int                                       { return o.MaxSize }
func (o *LogObj) GetOutput() LogOutputEnum                              { return o.Output }
func (o *LogLevelObj) GetConsole() LogLevelConsoleEnum                  { return o.Console }
func (o *LogLevelObj) GetFile() LogLevelConsoleEnum                     { return o.File }
func (o *GoObj) GetAsk() *GoAskObj                                      { return &o.Ask }
func (o *GoObj) GetConf() *GoConfObj                                    { return &o.Conf }
func (o *GoObj) GetForward() *GoForwardObj                              { return &o.Forward }
func (o *GoObj) GetKey() *GoKeyObj                                      { return &o.Key }
func (o *GoObj) GetPeerInfo() *GoPeerInfoObj                            { return &o.PeerInfo }
func (o *GoObj) GetProbe() *GoProbeObj                                  { return &o.Probe }
func (o *GoAskObj) GetAddr() string                                     { return o.Addr }
func (o *GoAskObj) GetFormat() GoAskFormatEnum                          { return o.Format }
func (o *GoAskObj) GetPeer() []string                                   { return o.Peer }
func (o *GoAskObj) GetTimeout() time.Duration                           { return o.Timeout }
func (o *GoConfObj) GetExport() *GoConfExportObj                        { return &o.Export }
func (o *GoConfObj) GetGenerate() *GoConfGenerateObj                    { return &o.Generate }
func (o *GoConfObj) GetImport() *GoConfImportObj                        { return &o.Import }
func (o *GoConfExportObj) GetFormat() GoConfExportFormatEnum            { return o.Format }
func (o *GoConfExportObj) GetFrom() string                              { return o.From }
func (o *GoConfExportObj) GetTo() string                                { return o.To }
func (o *GoConfGenerateObj) GetFormat() GoConfExportFormatEnum          { return o.Format }
func (o *GoConfGenerateObj) GetPath() string                            { return o.Path }
func (o *GoConfGenerateObj) GetPreset() GoConfGeneratePresetEnum        { return o.Preset }
func (o *GoConfImportObj) GetFormat() GoConfExportFormatEnum            { return o.Format }
func (o *GoConfImportObj) GetFrom() string                              { return o.From }
func (o *GoConfImportObj) GetTo() string                                { return o.To }
func (o *GoForwardObj) GetFrom() string                                 { return o.From }
func (o *GoForwardObj) GetPeer() []string                               { return o.Peer }
func (o *GoForwardObj) GetProto() GoForwardProtoEnum                    { return o.Proto }
func (o *GoForwardObj) GetTo() string                                   { return o.To }
func (o *GoKeyObj) GetAddr() string                                     { return o.Addr }
func (o *GoKeyObj) GetFromPem() string                                  { return o.FromPem }
func (o *GoKeyObj) GetGen() time.Duration                               { return o.Gen }
func (o *GoKeyObj) GetToPem() string                                    { return o.ToPem }
func (o *GoPeerInfoObj) GetFormat() GoAskFormatEnum                     { return o.Format }
func (o *GoPeerInfoObj) GetPeer() []string                              { return o.Peer }
func (o *GoPeerInfoObj) GetTimeout() time.Duration                      { return o.Timeout }
func (o *GoProbeObj) GetConcurrency() int                               { return o.Concurrency }
func (o *GoProbeObj) GetCount() int                                     { return o.Count }
func (o *GoProbeObj) GetFormat() GoAskFormatEnum                        { return o.Format }
func (o *GoProbeObj) GetMaxDepth() uint16                               { return o.MaxDepth }
func (o *GoProbeObj) GetPeer() []string                                 { return o.Peer }
func (o *GoProbeObj) GetPing() string                                   { return o.Ping }
func (o *GoProbeObj) GetScan() bool                                     { return o.Scan }
func (o *GoProbeObj) GetTimeout() time.Duration                         { return o.Timeout }
func (o *GoProbeObj) GetTrace() string                                  { return o.Trace }

// //

// Yggdrasil network node configuration
type YggdrasilInterface interface {
	GetKey() YggdrasilKeyInterface
	GetListen() []string
	GetInputs() []string
	GetPeers() YggdrasilPeersInterface
	GetAllowedPublicKeys() []string
	GetAdminListen() string
	GetIf() YggdrasilIfInterface
	GetNode() YggdrasilNodeInterface
	GetLogLookups() bool
	GetCoreStopTimeout() time.Duration
	GetRstQueueSize() int
	GetMulticast() YggdrasilMulticastInterface
	GetSocks() YggdrasilSocksInterface
}

// node private key; empty → auto-generated; if both set, path takes priority
type YggdrasilKeyInterface interface {
	GetText() string
	GetPath() string
}

// peer connections
type YggdrasilPeersInterface interface {
	GetUrl() []string
	GetInterface() map[string][]string
	GetManager() YggdrasilPeersManagerInterface
}

// smart peer manager (replaces standard Yggdrasil peering)
type YggdrasilPeersManagerInterface interface {
	GetEnable() bool
	GetProbeTimeout() time.Duration
	GetRefreshInterval() time.Duration
	GetMaxPerProto() int
	GetBatchSize() int
}

// TUN adapter
type YggdrasilIfInterface interface {
	GetName() string
	GetMtu() uint64
}

// node identity and metadata
type YggdrasilNodeInterface interface {
	GetInfo() map[string]any
	GetPrivacy() bool
	GetAuto() bool
}

// multicast peer discovery
type YggdrasilMulticastInterface interface {
	GetRegex() string
	GetBeacon() bool
	GetListen() bool
	GetPort() uint16
	GetPriority() uint16
	GetPassword() string
}

// SOCKS5 proxy configuration
type YggdrasilSocksInterface interface {
	GetAddr() string
	GetMaxConnections() int
}
