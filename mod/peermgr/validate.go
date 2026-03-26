package peermgr

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// // // // // // // // // //

// Допустимые транспортные схемы Yggdrasil
var AllowedSchemes = []string{"tcp", "tls", "quic", "ws", "wss"}

// peerEntryObj — валидированный пир: оригинальный URI + схема транспорта
type peerEntryObj struct {
	URI    string
	Scheme string
}

// ValidatePeers проверяет массив URI-строк:
// пустые строки отсекаются, дубликаты → ошибка, затем парсинг и проверка схемы.
// Порядок валидных записей сохраняется
func ValidatePeers(peers []string) ([]peerEntryObj, []error) {
	var errs []error
	result := make([]peerEntryObj, 0, len(peers))
	seen := make(map[string]bool, len(peers))

	for _, raw := range peers {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}

		if seen[s] {
			errs = append(errs, fmt.Errorf("duplicate peer %q", s))
			continue
		}
		seen[s] = true

		u, err := url.Parse(s)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid URI %q: %w", s, err))
			continue
		}

		if u.Host == "" {
			errs = append(errs, fmt.Errorf("missing host in %q", s))
			continue
		}

		if !slices.Contains(AllowedSchemes, u.Scheme) {
			errs = append(errs, fmt.Errorf("unsupported scheme %q in %q, allowed: %v", u.Scheme, s, AllowedSchemes))
			continue
		}

		result = append(result, peerEntryObj{URI: u.String(), Scheme: u.Scheme})
	}

	return result, errs
}
