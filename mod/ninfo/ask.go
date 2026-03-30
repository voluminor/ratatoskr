package ninfo

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/voluminor/ratatoskr/mod/sigils"
)

// // // // // // // // // //

// BuildInfoObj holds build metadata exposed when NodeInfoPrivacy is off.
type BuildInfoObj struct {
	Name     string
	Version  string
	Platform string
	Arch     string
}

// //

// AskResultObj is the result of a single getNodeInfo request.
type AskResultObj struct {
	RTT    time.Duration
	Parsed *ParsedObj
	Build  *BuildInfoObj
}

// // // // // // // // // //

const (
	keyBuildName     = "buildname"
	keyBuildVersion  = "buildversion"
	keyBuildPlatform = "buildplatform"
	keyBuildArch     = "buildarch"
)

// //

// Ask queries a remote node's NodeInfo by its public key.
// Returns parsed ratatoskr metadata, build info (nil if NodeInfoPrivacy),
// and measured RTT. Accepts optional sigil parsers forwarded to Parse.
func (obj *Obj) Ask(ctx context.Context, key ed25519.PublicKey, sg ...sigils.Interface) (*AskResultObj, error) {
	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: got %d, expected %d", ErrInvalidKeyLength, len(key), ed25519.PublicKeySize)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var ka [ed25519.PublicKeySize]byte
	copy(ka[:], key)

	type callResultObj struct {
		raw json.RawMessage
		err error
	}

	ch := make(chan callResultObj, 1)
	start := time.Now()

	go func() {
		raw, err := obj.callNodeInfo(ka)
		ch <- callResultObj{raw: raw, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		rtt := time.Since(start)
		if r.err != nil {
			return nil, r.err
		}
		return parseAskResponse(r.raw, rtt, sg...)
	}
}

// // // // // // // // // //

func parseAskResponse(raw json.RawMessage, rtt time.Duration, sg ...sigils.Interface) (*AskResultObj, error) {
	var nodeInfo map[string]any
	if err := json.Unmarshal(raw, &nodeInfo); err != nil {
		return nil, fmt.Errorf("ninfo: failed to unmarshal nodeinfo: %w", err)
	}

	result := &AskResultObj{
		RTT:    rtt,
		Parsed: Parse(nodeInfo, sg...),
	}

	result.Build = extractBuildInfo(result.Parsed.Extra)

	return result, nil
}

// //

func extractBuildInfo(extra map[string]any) *BuildInfoObj {
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

	return &BuildInfoObj{
		Name:     name,
		Version:  version,
		Platform: platform,
		Arch:     arch,
	}
}

// //

func encodeHexKey(key []byte) string {
	return hex.EncodeToString(key)
}
