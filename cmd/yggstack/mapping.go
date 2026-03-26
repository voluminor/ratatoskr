package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/voluminor/ratatoskr/mod/forward"
)

// // // // // // // // // //

type tcpLocalMappingsObj []forward.TCPMappingObj

func (m *tcpLocalMappingsObj) String() string { return "" }
func (m *tcpLocalMappingsObj) Set(value string) error {
	listenIP, listenPort, mappedIP, mappedPort, err := buildMapping(value, true)
	if err != nil {
		return err
	}
	*m = append(*m, forward.TCPMappingObj{
		Listen: &net.TCPAddr{IP: listenIP, Port: listenPort},
		Mapped: &net.TCPAddr{IP: mappedIP, Port: mappedPort},
	})
	return nil
}

type tcpRemoteMappingsObj []forward.TCPMappingObj

func (m *tcpRemoteMappingsObj) String() string { return "" }
func (m *tcpRemoteMappingsObj) Set(value string) error {
	listenIP, listenPort, mappedIP, mappedPort, err := buildMapping(value, false)
	if err != nil {
		return err
	}
	*m = append(*m, forward.TCPMappingObj{
		Listen: &net.TCPAddr{IP: listenIP, Port: listenPort},
		Mapped: &net.TCPAddr{IP: mappedIP, Port: mappedPort},
	})
	return nil
}

// //

type udpLocalMappingsObj []forward.UDPMappingObj

func (m *udpLocalMappingsObj) String() string { return "" }
func (m *udpLocalMappingsObj) Set(value string) error {
	listenIP, listenPort, mappedIP, mappedPort, err := buildMapping(value, true)
	if err != nil {
		return err
	}
	*m = append(*m, forward.UDPMappingObj{
		Listen: &net.UDPAddr{IP: listenIP, Port: listenPort},
		Mapped: &net.UDPAddr{IP: mappedIP, Port: mappedPort},
	})
	return nil
}

type udpRemoteMappingsObj []forward.UDPMappingObj

func (m *udpRemoteMappingsObj) String() string { return "" }
func (m *udpRemoteMappingsObj) Set(value string) error {
	listenIP, listenPort, mappedIP, mappedPort, err := buildMapping(value, false)
	if err != nil {
		return err
	}
	*m = append(*m, forward.UDPMappingObj{
		Listen: &net.UDPAddr{IP: listenIP, Port: listenPort},
		Mapped: &net.UDPAddr{IP: mappedIP, Port: mappedPort},
	})
	return nil
}

// //

func buildMapping(value string, isLocal bool) (listenIP net.IP, listenPort int, mappedIP net.IP, mappedPort int, err error) {
	firstAddr, firstPort, secondAddr, secondPort, err := parseMappingString(value)
	if err != nil {
		return nil, 0, nil, 0, err
	}
	if isLocal {
		if secondAddr == "" {
			return nil, 0, nil, 0, fmt.Errorf("local mapping requires a Yggdrasil IPv6 destination address")
		}
		ip := net.ParseIP(secondAddr)
		if ip == nil || ip.To4() != nil {
			return nil, 0, nil, 0, fmt.Errorf("Yggdrasil mapped address must be a valid IPv6 address, got %q", secondAddr)
		}
	} else {
		if firstAddr != "" {
			return nil, 0, nil, 0, fmt.Errorf("Yggdrasil listening must be empty")
		}
	}

	mappedIP = net.IPv6loopback
	if firstAddr != "" {
		listenIP = net.ParseIP(firstAddr)
		if listenIP == nil {
			return nil, 0, nil, 0, fmt.Errorf("invalid listen address %q", firstAddr)
		}
	}
	if secondAddr != "" {
		mappedIP = net.ParseIP(secondAddr)
		if mappedIP == nil {
			return nil, 0, nil, 0, fmt.Errorf("invalid mapped address %q", secondAddr)
		}
	}
	return listenIP, firstPort, mappedIP, secondPort, nil
}

func parseMappingString(value string) (string, int, string, int, error) {
	tokens := strings.Split(value, ":")
	tokensLen := len(tokens)

	var firstAddress, secondAddress string
	var firstPort, secondPort int
	var err error

	if tokensLen == 1 {
		firstPort, err = strconv.Atoi(tokens[0])
		if err != nil {
			return "", 0, "", 0, fmt.Errorf("malformed mapping spec '%s'", value)
		}
		secondPort = firstPort
	}

	if tokensLen == 2 {
		firstPort, err = strconv.Atoi(tokens[0])
		if err != nil {
			return "", 0, "", 0, fmt.Errorf("malformed mapping spec '%s'", value)
		}
		secondPort, err = strconv.Atoi(tokens[1])
		if err != nil {
			return "", 0, "", 0, fmt.Errorf("malformed mapping spec '%s'", value)
		}
	}

	if tokensLen == 3 {
		firstPort, err = strconv.Atoi(tokens[0])
		if err != nil {
			return "", 0, "", 0, fmt.Errorf("malformed mapping spec '%s'", value)
		}
		var secondPortStr string
		secondAddress, secondPortStr, err = net.SplitHostPort(tokens[1] + ":" + tokens[2])
		if err != nil {
			return "", 0, "", 0, fmt.Errorf("malformed mapping spec '%s'", value)
		}
		secondPort, err = strconv.Atoi(secondPortStr)
		if err != nil {
			return "", 0, "", 0, fmt.Errorf("malformed mapping spec '%s'", value)
		}
	}

	if tokensLen == 4 {
		var firstPortStr, secondPortStr string
		firstAddress, firstPortStr, err = net.SplitHostPort(tokens[0] + ":" + tokens[1])
		if err != nil {
			return "", 0, "", 0, fmt.Errorf("malformed mapping spec '%s'", value)
		}
		secondAddress, secondPortStr, err = net.SplitHostPort(tokens[2] + ":" + tokens[3])
		if err != nil {
			return "", 0, "", 0, fmt.Errorf("malformed mapping spec '%s'", value)
		}
		firstPort, err = strconv.Atoi(firstPortStr)
		if err != nil {
			return "", 0, "", 0, fmt.Errorf("malformed mapping spec '%s'", value)
		}
		secondPort, err = strconv.Atoi(secondPortStr)
		if err != nil {
			return "", 0, "", 0, fmt.Errorf("malformed mapping spec '%s'", value)
		}
	}

	if tokensLen > 4 {
		secondPort, err = strconv.Atoi(tokens[tokensLen-1])
		if err != nil {
			return "", 0, "", 0, fmt.Errorf("malformed mapping spec '%s'", value)
		}
		tokens = tokens[:tokensLen-1]
		tokensLen = len(tokens)

		if strings.HasSuffix(tokens[tokensLen-1], "]") {
			for i := tokensLen - 1; i >= 0; i-- {
				if strings.HasPrefix(tokens[i], "[") {
					secondAddress = strings.Join(tokens[i:], ":")
					secondAddress, _ = strings.CutPrefix(secondAddress, "[")
					secondAddress, _ = strings.CutSuffix(secondAddress, "]")
					tokens = tokens[:i]
					break
				}
			}
		} else {
			secondAddress = tokens[tokensLen-1]
			tokens = tokens[:tokensLen-1]
		}

		tokensLen = len(tokens)
		if tokensLen < 1 {
			return "", 0, "", 0, fmt.Errorf("malformed mapping spec '%s'", value)
		}
		firstPort, err = strconv.Atoi(tokens[tokensLen-1])
		if err != nil {
			return "", 0, "", 0, fmt.Errorf("malformed mapping spec '%s'", value)
		}
		tokens = tokens[:tokensLen-1]
		tokensLen = len(tokens)

		if tokensLen > 0 {
			if strings.HasSuffix(tokens[tokensLen-1], "]") {
				for i := tokensLen - 1; i >= 0; i-- {
					if strings.HasPrefix(tokens[i], "[") {
						firstAddress = strings.Join(tokens[i:], ":")
						firstAddress, _ = strings.CutPrefix(firstAddress, "[")
						firstAddress, _ = strings.CutSuffix(firstAddress, "]")
						break
					}
				}
			} else {
				firstAddress = tokens[tokensLen-1]
			}
		}
	}

	if firstPort < 1 || firstPort > 65535 || secondPort < 1 || secondPort > 65535 {
		return "", 0, "", 0, fmt.Errorf("ports must be in range 1-65535")
	}
	return firstAddress, firstPort, secondAddress, secondPort, nil
}
