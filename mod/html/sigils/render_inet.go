package sigils

import (
	"bytes"
	"html"

	"github.com/voluminor/ratatoskr/mod/sigils/inet"
)

// // // // // // // // // //

// RenderInet produces an HTML block from an inet sigil.
func RenderInet(o *inet.Obj) []byte {
	addrs := o.Addrs()
	if len(addrs) == 0 {
		return nil
	}

	var buf bytes.Buffer
	buf.WriteString("<div class=\"sg-block\" data-sigil=\"inet\">\n")
	buf.WriteString("  <div class=\"sg-header\">inet</div>\n")
	buf.WriteString("  <div class=\"sg-list\">\n")

	for _, addr := range addrs {
		buf.WriteString("    <div class=\"sg-list-item\">")
		buf.WriteString(html.EscapeString(addr))
		buf.WriteString("</div>\n")
	}

	buf.WriteString("  </div>\n")
	buf.WriteString("</div>\n")
	return buf.Bytes()
}
