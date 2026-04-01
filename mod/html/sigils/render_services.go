package sigils

import (
	"bytes"
	"html"
	"slices"
	"strconv"

	"github.com/voluminor/ratatoskr/mod/sigils/services"
)

// // // // // // // // // //

// RenderServices produces an HTML block from a services sigil.
func RenderServices(o *services.Obj) []byte {
	svc := o.Services()
	if len(svc) == 0 {
		return nil
	}

	var buf bytes.Buffer
	buf.WriteString("<div class=\"sg-block\" data-sigil=\"services\">\n")
	buf.WriteString("  <div class=\"sg-header\">services</div>\n")

	names := make([]string, 0, len(svc))
	for n := range svc {
		names = append(names, n)
	}
	slices.Sort(names)

	for _, name := range names {
		buf.WriteString("  <div class=\"sg-svc-row\">")
		buf.WriteString("<span class=\"sg-svc-name\">")
		buf.WriteString(html.EscapeString(name))
		buf.WriteString("</span>")
		buf.WriteString("<span class=\"sg-svc-port\">")
		buf.WriteString(strconv.FormatUint(uint64(svc[name]), 10))
		buf.WriteString("</span>")
		buf.WriteString("</div>\n")
	}

	buf.WriteString("</div>\n")
	return buf.Bytes()
}
