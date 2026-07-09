package main

import (
	"fmt"
	"log"
	"os"
)

// // // // // // // // // //

// loggerObj adapts the standard logger to yggdrasil-go's logger contract.
type loggerObj struct {
	base *log.Logger
}

func newLogger(name string) loggerObj {
	return loggerObj{base: log.New(os.Stdout, "["+name+"] ", log.LstdFlags|log.Lmicroseconds)}
}

func (l loggerObj) logf(level string, format string, args ...interface{}) {
	l.base.Printf(level+" "+format, args...)
}

func (l loggerObj) logln(level string, args ...interface{}) {
	l.base.Println(level, fmt.Sprint(args...))
}

func (l loggerObj) Printf(format string, args ...interface{}) { l.logf("print", format, args...) }
func (l loggerObj) Println(args ...interface{})               { l.logln("print", args...) }
func (l loggerObj) Infof(format string, args ...interface{})  { l.logf("info", format, args...) }
func (l loggerObj) Infoln(args ...interface{})                { l.logln("info", args...) }
func (l loggerObj) Warnf(format string, args ...interface{})  { l.logf("warn", format, args...) }
func (l loggerObj) Warnln(args ...interface{})                { l.logln("warn", args...) }
func (l loggerObj) Errorf(format string, args ...interface{}) { l.logf("error", format, args...) }
func (l loggerObj) Errorln(args ...interface{})               { l.logln("error", args...) }
func (l loggerObj) Debugf(format string, args ...interface{}) { l.logf("debug", format, args...) }
func (l loggerObj) Debugln(args ...interface{})               { l.logln("debug", args...) }
func (l loggerObj) Traceln(args ...interface{})               { l.logln("trace", args...) }
