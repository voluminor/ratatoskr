package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/voluminor/ratatoskr/cmd/ratatoskr/gocmd"
	cmdsettings "github.com/voluminor/ratatoskr/cmd/ratatoskr/gsettings"
	rsettings "github.com/voluminor/ratatoskr/cmd/ratatoskr/target/settings"
)

// // // // // // // // // //

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if cmdsettings.IsCommandArgs(args) {
		cfg, err := cmdsettings.Parse(args)
		if err != nil {
			return err
		}
		handled, err := gocmd.Run(cfg)
		if err != nil {
			return err
		}
		if !handled {
			return fmt.Errorf("no go command selected")
		}
		return nil
	}

	configPath, runtimeArgs, err := extractConfigArg(args)
	if err != nil {
		return err
	}
	cfg, err := rsettings.LoadConfig(configPath, runtimeArgs)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	data, err := cfg.RenderJSON(false)
	if err != nil {
		return err
	}
	fmt.Print(string(data))
	return nil
}

func extractConfigArg(args []string) (string, []string, error) {
	out := make([]string, 0, len(args))
	var configPath string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-config" || arg == "--config":
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("%s requires a path", arg)
			}
			i++
			configPath = args[i]
		case strings.HasPrefix(arg, "-config="):
			configPath = strings.TrimPrefix(arg, "-config=")
		case strings.HasPrefix(arg, "--config="):
			configPath = strings.TrimPrefix(arg, "--config=")
		default:
			out = append(out, arg)
		}
	}
	return configPath, out, nil
}
