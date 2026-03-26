package ratatoskr

import yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"

// // // // // // // // // //

// noopLoggerObj — реализация yggcore.Logger, отбрасывающая все сообщения.
// Используется когда пользователь не передал логгер в ConfigObj
type noopLoggerObj struct{}

var _ yggcore.Logger = noopLoggerObj{}

func (noopLoggerObj) Printf(string, ...interface{}) {}
func (noopLoggerObj) Println(...interface{})        {}
func (noopLoggerObj) Infof(string, ...interface{})  {}
func (noopLoggerObj) Infoln(...interface{})         {}
func (noopLoggerObj) Warnf(string, ...interface{})  {}
func (noopLoggerObj) Warnln(...interface{})         {}
func (noopLoggerObj) Errorf(string, ...interface{}) {}
func (noopLoggerObj) Errorln(...interface{})        {}
func (noopLoggerObj) Debugf(string, ...interface{}) {}
func (noopLoggerObj) Debugln(...interface{})        {}
func (noopLoggerObj) Tracef(string, ...interface{}) {}
func (noopLoggerObj) Traceln(...interface{})        {}
