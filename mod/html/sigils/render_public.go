package sigils

import (
	"bytes"
	"html"
	"slices"

	"github.com/voluminor/ratatoskr/mod/sigils/public"
)

// // // // // // // // // //

// RenderPublic produces an HTML block from a public sigil.
func RenderPublic(o *public.Obj) []byte {
	peers := o.Peers()
	if len(peers) == 0 {
		return nil
	}

	var buf bytes.Buffer
	buf.WriteString("<div class=\"sg-block\" data-sigil=\"public\">\n")
	buf.WriteString("  <div class=\"sg-header\">public</div>\n")

	groups := make([]string, 0, len(peers))
	for g := range peers {
		groups = append(groups, g)
	}
	slices.Sort(groups)

	for _, g := range groups {
		buf.WriteString("  <div class=\"sg-peer-group\">\n")
		buf.WriteString("    <div class=\"sg-peer-label\">")
		buf.WriteString(html.EscapeString(g))
		buf.WriteString("</div>\n")
		for _, uri := range peers[g] {
			buf.WriteString("    <div class=\"sg-peer-item\">")
			buf.WriteString(html.EscapeString(uri))
			buf.WriteString("</div>\n")
		}
		buf.WriteString("  </div>\n")
	}

	buf.WriteString("</div>\n")
	return buf.Bytes()
}
