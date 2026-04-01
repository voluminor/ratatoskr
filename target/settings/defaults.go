// Code generated using '_generate/settings'; DO NOT EDIT.
// Generation time: 2026-04-01T16:43:05Z

package settings

import (
	"time"
)

// // // // // // // // // //

// NewDefault creates an Obj pre-filled with default values.
func NewDefault() *Obj {
	o := &Obj{}
	o.Log.Compress = true
	o.Log.Format = GoAskFormatText
	o.Log.Level.Console = LogLevelConsoleDebug
	o.Log.Level.File = LogLevelConsoleInfo
	o.Log.MaxAge = 30
	o.Log.MaxBackups = 3
	o.Log.MaxSize = 32
	o.Log.Output = LogOutputBoth
	o.Yggdrasil.AdminListen = "none"
	o.Yggdrasil.CoreStopTimeout = time.Duration(5000000000)
	o.Yggdrasil.If.Mtu = 65535
	o.Yggdrasil.If.Name = "none"
	o.Yggdrasil.Multicast.Beacon = true
	o.Yggdrasil.Multicast.Listen = true
	o.Yggdrasil.Multicast.Regex = ".*"
	o.Yggdrasil.Node.Auto = true
	o.Yggdrasil.Peers.Manager.Enable = true
	o.Yggdrasil.Peers.Manager.ProbeTimeout = time.Duration(10000000000)
	o.Yggdrasil.RstQueueSize = 100
	return o
}
