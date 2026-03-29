package ninfo

import (
	"fmt"
	"strings"

	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //

func compileRatatoskrInfo(sigils map[string]SigilInterface) string {
	names := make([]string, 0, len(sigils))
	for name, _ := range sigils {
		names = append(names, name)
	}

	return fmt.Sprintf("[%s] %s",
		strings.Join(names, ","),
		target.GlobalVersion,
	)
}

// // // //
