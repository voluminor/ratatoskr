package settings

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/voluminor/ratatoskr/target"
	gsettings "github.com/voluminor/ratatoskr/target/settings"
)

// // // // // // // // // //

// New initializes settings and runs the application callback.
// Handles help/info flags internally; on error prints to stderr and exits.
func New(run func(Interface) error) error {
	obj, err := gsettings.Init(buildInfoText(), func(path string, obj *gsettings.Obj) error {
		return ParseFile(path, obj)
	})
	if err != nil {
		if errors.Is(err, flag.ErrHelp) || errors.Is(err, gsettings.ErrInfo) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	return run(obj)
}

// //

func buildInfoText() string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s %s\n", target.GlobalName, target.GlobalVersion)
	b.WriteString("Embeddable Yggdrasil network node library and tools\n\n")
	b.WriteString("Dependencies:\n")

	keys := make([]string, 0, len(target.GlobalDependenciesMap))
	for k := range target.GlobalDependenciesMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		fmt.Fprintf(&b, "  %s %s\n", k, target.GlobalDependenciesMap[k])
	}

	return b.String()
}
