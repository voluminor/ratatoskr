package ninfo

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //

// RatatoskrInfoObj holds parsed ratatoskr metadata from a NodeInfo string.
type RatatoskrInfoObj struct {
	Version string
	Sigils  []string
}

func (ri *RatatoskrInfoObj) String() string {
	return fmt.Sprintf("[%s] %s", strings.Join(ri.Sigils, ","), ri.Version)
}

// //

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

// ParseRatatoskrInfo parses a ratatoskr info string.
// Accepted formats:
//   - "[sigil1,sigil2] version"
//   - "ratatoskr [sigil1,sigil2] version"
func ParseRatatoskrInfo(raw string) (*RatatoskrInfoObj, error) {
	begin := strings.IndexByte(raw, '[')
	end := strings.IndexByte(raw, ']')
	if begin < 0 || end < begin+1 {
		return nil, errors.New("invalid format: missing sigil brackets")
	}

	body := raw[begin+1 : end]

	rest := strings.TrimSpace(raw[end+1:])
	if rest == "" {
		return nil, errors.New("invalid format: missing version")
	}

	var names []string
	if body != "" {
		names = strings.Split(body, ",")
	}

	return &RatatoskrInfoObj{
		Version: rest,
		Sigils:  names,
	}, nil
}
