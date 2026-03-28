package settings

import (
	"errors"
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/voluminor/ratatoskr/target"
	gsettings "github.com/voluminor/ratatoskr/target/settings"
)

// // // // // // // // // //

const description = "Embeddable Yggdrasil network node library and tools"

// //

// New initializes settings and runs the application callback.
// Returns nil on help/info, error on init failure.
func New(run func(Interface) error) error {
	obj, err := gsettings.Init(buildInfoText(), func(path string, obj *gsettings.Obj) error {
		return ParseFile(path, obj)
	})
	if err != nil {
		if errors.Is(err, flag.ErrHelp) || errors.Is(err, gsettings.ErrInfo) {
			return nil
		}
		return err
	}

	return run(obj)
}

// //

func Obj(i Interface) *gsettings.Obj {
	return i.Self().(*gsettings.Obj)
}

// //

func buildInfoText() string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s %s\n", target.GlobalName, target.GlobalVersion)
	b.WriteString(description)
	b.WriteString("\n\n")
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
