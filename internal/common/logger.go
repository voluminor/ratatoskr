package common

import yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"

// // // // // // // // // //

// DiscardLoggerObj implements the Yggdrasil logger contract without output.
type DiscardLoggerObj struct{}

func (DiscardLoggerObj) Printf(string, ...interface{}) {}
func (DiscardLoggerObj) Println(...interface{})        {}
func (DiscardLoggerObj) Infof(string, ...interface{})  {}
func (DiscardLoggerObj) Infoln(...interface{})         {}
func (DiscardLoggerObj) Warnf(string, ...interface{})  {}
func (DiscardLoggerObj) Warnln(...interface{})         {}
func (DiscardLoggerObj) Errorf(string, ...interface{}) {}
func (DiscardLoggerObj) Errorln(...interface{})        {}
func (DiscardLoggerObj) Debugf(string, ...interface{}) {}
func (DiscardLoggerObj) Debugln(...interface{})        {}
func (DiscardLoggerObj) Traceln(...interface{})        {}

// NormalizeLogger replaces a nil logger with DiscardLoggerObj.
func NormalizeLogger(log yggcore.Logger) yggcore.Logger {
	if log == nil {
		return DiscardLoggerObj{}
	}
	return log
}
