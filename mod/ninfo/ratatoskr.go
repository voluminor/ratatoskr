package ninfo

import (
	"fmt"
	"slices"
	"strings"

	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //

func compileRatatoskrInfo(sigils map[string]SigilInterface) string {
	names := make([]string, 0, len(sigils))
	for name := range sigils {
		names = append(names, name)
	}
	slices.Sort(names)

	return fmt.Sprintf("[%s] %s",
		strings.Join(names, ","),
		target.GlobalVersion,
	)
}
