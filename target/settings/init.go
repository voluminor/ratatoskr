// Code generated using '_generate/settings'; DO NOT EDIT.
// Generation time: 2026-04-01T16:43:05Z

package settings

import (
	"errors"
	"flag"
	"os"
)

// // // // // // // // // //

// Init parses CLI flags and an optional config file, returning a fully resolved Obj.
// Priority: defaults < config file < CLI flags.
func Init(infoText string, parseFile func(string, *Obj) error) (*Obj, error) {
	obj := NewDefault()

	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	fs.Usage = printHelp

	var showInfo bool
	if infoText != "" {
		fs.BoolVar(&showInfo, "i", false, "show application info")
		fs.BoolVar(&showInfo, "info", false, "show application info")
	}
	targets := &CustomFlagTargetsObj{}
	DefineFlags(fs, obj, targets)

	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, err
		}
		return nil, err
	}

	if showInfo && infoText != "" {
		printInfo(infoText)
		return nil, ErrInfo
	}

	// Config file layer: load file, then re-apply CLI flags on top
	if obj.Config != "" && parseFile != nil {
		fileObj := NewDefault()
		if err := parseFile(obj.Config, fileObj); err != nil {
			return nil, err
		}
		configPath := obj.Config
		*obj = *fileObj
		obj.Config = configPath

		fs = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
		fs.Usage = printHelp

		if infoText != "" {
			fs.BoolVar(&showInfo, "i", false, "show application info")
			fs.BoolVar(&showInfo, "info", false, "show application info")
		}
		targets = &CustomFlagTargetsObj{}
		DefineFlags(fs, obj, targets)
		if err := fs.Parse(os.Args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}
			return nil, err
		}
	}
	if err := ApplyCustomFlags(fs, targets, obj); err != nil {
		return nil, err
	}

	return obj, nil
}
