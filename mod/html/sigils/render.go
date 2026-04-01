package sigils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"slices"
	"strconv"

	"github.com/voluminor/ratatoskr/mod/ninfo"
	coresigils "github.com/voluminor/ratatoskr/mod/sigils"
)

// // // // // // // // // //

// Render produces HTML blocks from a parsed NodeInfo.
// Sigils contains one HTML block per recognized sigil.
// Extra contains one <div> per leftover key not claimed by any sigil.
func Render(parsed *ninfo.ParsedObj) (*ResultObj, error) {
	if parsed == nil {
		return nil, fmt.Errorf("nil parsed")
	}

	result := &ResultObj{
		Sigils: make(map[string][]byte, len(parsed.Sigils)),
	}

	for name, sg := range parsed.Sigils {
		buf, err := renderSigil(sg)
		if err != nil {
			return nil, fmt.Errorf("sigil[%s]: %w", name, err)
		}
		result.Sigils[name] = buf
	}

	if len(parsed.Extra) > 0 {
		buf, err := renderExtra(parsed.Extra)
		if err != nil {
			return nil, fmt.Errorf("extra: %w", err)
		}
		result.Extra = buf
	}

	return result, nil
}

// RenderOne produces a single HTML block for one sigil.
func RenderOne(sg coresigils.Interface) ([]byte, error) {
	if sg == nil {
		return nil, fmt.Errorf("nil sigil")
	}
	return renderSigil(sg)
}

// //

func renderSigil(sg coresigils.Interface) ([]byte, error) {
	params := sg.Params()
	if len(params) == 0 {
		return nil, nil
	}

	var buf bytes.Buffer
	buf.WriteString(`<div class="sg-block" data-sigil="`)
	buf.WriteString(html.EscapeString(sg.GetName()))
	buf.WriteString("\">\n")

	buf.WriteString(`  <div class="sg-header">`)
	buf.WriteString(html.EscapeString(sg.GetName()))
	buf.WriteString("</div>\n")

	keys := sortedKeys(params)
	for _, k := range keys {
		writeEntry(&buf, k, params[k], "sg")
	}

	buf.WriteString("</div>\n")
	return buf.Bytes(), nil
}

func renderExtra(m map[string]any) ([]byte, error) {
	if len(m) == 0 {
		return nil, nil
	}

	var buf bytes.Buffer
	keys := sortedKeys(m)

	for _, k := range keys {
		writeEntry(&buf, k, m[k], "ni")
	}

	return buf.Bytes(), nil
}

// //

func writeEntry(buf *bytes.Buffer, key string, val any, prefix string) {
	buf.WriteString(`<div class="`)
	buf.WriteString(prefix)
	buf.WriteString(`-block" data-key="`)
	buf.WriteString(html.EscapeString(key))
	buf.WriteString("\">\n")

	buf.WriteString(`  <div class="`)
	buf.WriteString(prefix)
	buf.WriteString(`-key">`)
	buf.WriteString(html.EscapeString(key))
	buf.WriteString("</div>\n")

	buf.WriteString(`  <div class="`)
	buf.WriteString(prefix)
	buf.WriteString("-val ")
	buf.WriteString(prefix)
	buf.WriteByte('-')
	buf.WriteString(valType(val))
	buf.WriteString(`">`)
	buf.WriteString(formatVal(val))
	buf.WriteString("</div>\n")

	buf.WriteString("</div>\n")
}

func formatVal(v any) string {
	switch t := v.(type) {
	case string:
		return html.EscapeString(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case nil:
		return ""
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return html.EscapeString(fmt.Sprintf("%v", v))
		}
		return html.EscapeString(string(raw))
	}
}

func valType(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "bool"
	case nil:
		return "null"
	default:
		return "json"
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
