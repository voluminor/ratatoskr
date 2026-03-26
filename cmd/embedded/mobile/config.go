package mobile

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"

	"github.com/yggdrasil-network/ratatoskr/mod/forward"
)

// // // // // // // // // //

// GenerateConfig генерирует новую конфигурацию ноды Yggdrasil со случайной парой ключей.
// Возвращает JSON-строку для сохранения и передачи в LoadConfigJSON.
func GenerateConfig() (string, error) {
	nodeCfg := config.GenerateConfig()
	nodeCfg.AdminListen = "none"
	b, err := json.MarshalIndent(nodeCfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal config: %w", err)
	}
	return string(b), nil
}

// //

func parseTCPMapping(listenStr, mappedStr string) (forward.TCPMappingObj, error) {
	listenAddr, err := net.ResolveTCPAddr("tcp", listenStr)
	if err != nil {
		return forward.TCPMappingObj{}, fmt.Errorf("invalid listen address %q: %w", listenStr, err)
	}
	mappedAddr, err := net.ResolveTCPAddr("tcp", mappedStr)
	if err != nil {
		return forward.TCPMappingObj{}, fmt.Errorf("invalid mapped address %q: %w", mappedStr, err)
	}
	return forward.TCPMappingObj{Listen: listenAddr, Mapped: mappedAddr}, nil
}

func parseUDPMapping(listenStr, mappedStr string) (forward.UDPMappingObj, error) {
	listenAddr, err := net.ResolveUDPAddr("udp", listenStr)
	if err != nil {
		return forward.UDPMappingObj{}, fmt.Errorf("invalid listen address %q: %w", listenStr, err)
	}
	mappedAddr, err := net.ResolveUDPAddr("udp", mappedStr)
	if err != nil {
		return forward.UDPMappingObj{}, fmt.Errorf("invalid mapped address %q: %w", mappedStr, err)
	}
	return forward.UDPMappingObj{Listen: listenAddr, Mapped: mappedAddr}, nil
}

func parseRemoteTCPMapping(port int, localStr string) (forward.TCPMappingObj, error) {
	if port < 1 || port > 65535 {
		return forward.TCPMappingObj{}, fmt.Errorf("port %d out of range 1-65535", port)
	}
	mappedAddr, err := net.ResolveTCPAddr("tcp", localStr)
	if err != nil {
		return forward.TCPMappingObj{}, fmt.Errorf("invalid local address %q: %w", localStr, err)
	}
	return forward.TCPMappingObj{
		Listen: &net.TCPAddr{Port: port},
		Mapped: mappedAddr,
	}, nil
}

func parseRemoteUDPMapping(port int, localStr string) (forward.UDPMappingObj, error) {
	if port < 1 || port > 65535 {
		return forward.UDPMappingObj{}, fmt.Errorf("port %d out of range 1-65535", port)
	}
	mappedAddr, err := net.ResolveUDPAddr("udp", localStr)
	if err != nil {
		return forward.UDPMappingObj{}, fmt.Errorf("invalid local address %q: %w", localStr, err)
	}
	return forward.UDPMappingObj{
		Listen: &net.UDPAddr{Port: port},
		Mapped: mappedAddr,
	}, nil
}
