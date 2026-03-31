package ninfo

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// SoftwareObj holds build metadata exposed when NodeInfoPrivacy is off.
type SoftwareObj struct {
	Name     string
	Version  string
	Platform string
	Arch     string
}

// //

// AskResultObj is the result of a single getNodeInfo request.
type AskResultObj struct {
	RTT      time.Duration
	Node     *ParsedObj
	Software *SoftwareObj
}

// // // // // // // // // //

const (
	keyBuildName     = "buildname"
	keyBuildVersion  = "buildversion"
	keyBuildPlatform = "buildplatform"
	keyBuildArch     = "buildarch"
)

// //

type adminCaptureObj struct {
	handlers map[string]yggcore.AddHandlerFunc
}

func (a *adminCaptureObj) AddHandler(name, _ string, _ []string, fn yggcore.AddHandlerFunc) error {
	a.handlers[name] = fn
	return nil
}

// //

func (obj *Obj) callNodeInfo(key [32]byte) (json.RawMessage, error) {
	req, _ := json.Marshal(map[string]string{
		"key": hex.EncodeToString(key[:]),
	})
	raw, err := obj.nodeInfo(req)
	if err != nil {
		return nil, err
	}

	resp, ok := raw.(yggcore.GetNodeInfoResponse)
	if !ok {
		return nil, ErrUnexpectedResponse
	}

	for _, msg := range resp {
		return msg, nil
	}
	return nil, ErrEmptyResponse
}

// //

func (obj *Obj) parseAskResponse(raw json.RawMessage, rtt time.Duration) (*AskResultObj, error) {
	var nodeInfo map[string]any
	if err := json.Unmarshal(raw, &nodeInfo); err != nil {
		return nil, fmt.Errorf("ninfo: failed to unmarshal nodeinfo: %w", err)
	}

	result := &AskResultObj{
		RTT:  rtt,
		Node: Parse(nodeInfo, obj.sigilSlice()...),
	}

	result.Software = extractSoftware(result.Node.Extra)

	return result, nil
}

// //

func extractSoftware(extra map[string]any) *SoftwareObj {
	name, _ := extra[keyBuildName].(string)
	version, _ := extra[keyBuildVersion].(string)
	platform, _ := extra[keyBuildPlatform].(string)
	arch, _ := extra[keyBuildArch].(string)

	if name == "" && version == "" && platform == "" && arch == "" {
		return nil
	}

	delete(extra, keyBuildName)
	delete(extra, keyBuildVersion)
	delete(extra, keyBuildPlatform)
	delete(extra, keyBuildArch)

	return &SoftwareObj{
		Name:     name,
		Version:  version,
		Platform: platform,
		Arch:     arch,
	}
}
