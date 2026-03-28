package main

import (
	"encoding/json"
	"fmt"
	"os"

	msettings "github.com/voluminor/ratatoskr/mod/settings"
)

// // // // // // // // // //

func main() {
	if err := msettings.New(run); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(cfg msettings.Interface) error {
	obj := msettings.Obj(cfg)

	data, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return err
	}

	fmt.Println(string(data))
	return nil
}
