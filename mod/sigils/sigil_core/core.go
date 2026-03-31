package sigil_core

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //

func CompileInfo(sg map[string]sigils.Interface) string {
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

// ParseInfo parses a ratatoskr info string.
// Accepted formats:
//   - "[sigil1,sigil2] version"
//   - "ratatoskr [sigil1,sigil2] version"
func ParseInfo(raw string) (string, []string, error) {
	begin := strings.IndexByte(raw, '[')
	end := strings.IndexByte(raw, ']')
	if begin < 0 || end < begin+1 {
		return "", nil, errors.New("invalid format: missing sigil brackets")
	}

	body := raw[begin+1 : end]

	rest := strings.TrimSpace(raw[end+1:])
	if rest == "" {
		return "", nil, errors.New("invalid format: missing version")
	}

	var names []string
	if body != "" {
		names = strings.Split(body, ",")
	}

	return rest, names, nil
}
