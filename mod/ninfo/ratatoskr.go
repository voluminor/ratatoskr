package ninfo

import (
	"fmt"
	"slices"
	"strings"

	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //

func compileRatatoskrInfo(sg map[string]sigils.Interface) string {
	names := make([]string, 0, len(sg))
	for name := range sg {
		names = append(names, name)
	}
	slices.Sort(names)

	return fmt.Sprintf("[%s] %s",
		strings.Join(names, ","),
		target.GlobalVersion,
	)
}
