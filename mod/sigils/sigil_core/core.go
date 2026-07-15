// Package sigil_core assembles sigils into NodeInfo and manages their metadata.
package sigil_core

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //

const (
	maxInfoSigils        = 64
	maxInfoVersionLength = 64
)

// CompileInfo returns sorted sigil metadata using the current Ratatoskr version.
func CompileInfo(sg map[string]sigils.Interface) string {
	return CompileInfoVersion(sg, target.Version)
}

// CompileInfoVersion returns sorted sigil metadata using version.
func CompileInfoVersion(sg map[string]sigils.Interface, version string) string {
	names := make([]string, 0, len(sg))
	for name := range sg {
		names = append(names, name)
	}
	return CompileInfoNames(names, version)
}

// CompileInfoNames returns metadata for sorted, deduplicated names without
// modifying the input slice. An empty version uses the current version.
func CompileInfoNames(names []string, version string) string {
	names = slices.Clone(names)
	slices.Sort(names)
	names = slices.Compact(names)
	if version == "" {
		version = target.Version
	}

	return fmt.Sprintf("[%s] %s",
		strings.Join(names, ","),
		version,
	)
}

// ParseInfo returns the version and unique sigil names encoded in metadata. It
// accepts an optional prefix before the opening bracket.
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
	if utf8.RuneCountInString(rest) > maxInfoVersionLength {
		return "", nil, fmt.Errorf("version is too long: max %d characters", maxInfoVersionLength)
	}
	for _, r := range rest {
		if r < 0x20 || r > 0x7e {
			return "", nil, errors.New("version contains non-printable characters")
		}
	}

	var names []string
	if body != "" {
		seen := make(map[string]struct{})
		for {
			name, rest, found := strings.Cut(body, ",")
			body = rest
			name = strings.TrimSpace(name)
			if name != "" {
				if !sigils.ValidateName(name) {
					return "", nil, fmt.Errorf("invalid sigil name: %s", name)
				}
				if _, ok := seen[name]; !ok {
					if len(names) >= maxInfoSigils {
						return "", nil, fmt.Errorf("too many sigils: %d (max %d)", len(names)+1, maxInfoSigils)
					}
					seen[name] = struct{}{}
					names = append(names, name)
				}
			}
			if !found {
				break
			}
		}
	}

	return rest, names, nil
}
