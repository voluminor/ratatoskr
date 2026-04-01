package sigils

import (
	"bytes"
	"html"
	"slices"

	"github.com/voluminor/ratatoskr/mod/sigils/info"
)

// // // // // // // // // //

// RenderInfo produces an HTML block from an info sigil.
func RenderInfo(o *info.Obj) []byte {
	c := o.Info()
	if c == nil {
		return nil
	}

	var buf bytes.Buffer
	buf.WriteString("<div class=\"sg-block\" data-sigil=\"info\">\n")
	buf.WriteString("  <div class=\"sg-header\">info</div>\n")

	writeRow(&buf, "name", c.Name)
	writeRow(&buf, "type", c.Type)

	if c.Location != "" {
		writeRow(&buf, "location", c.Location)
	}

	if len(c.Contacts) > 0 {
		buf.WriteString("  <div class=\"sg-group\" data-key=\"contacts\">\n")
		buf.WriteString("    <div class=\"sg-key\">contacts</div>\n")

		groups := make([]string, 0, len(c.Contacts))
		for g := range c.Contacts {
			groups = append(groups, g)
		}
		slices.Sort(groups)

		for _, g := range groups {
			buf.WriteString("    <div class=\"sg-contacts-group\">\n")
			buf.WriteString("      <div class=\"sg-contacts-label\">")
			buf.WriteString(html.EscapeString(g))
			buf.WriteString("</div>\n")
			for _, addr := range c.Contacts[g] {
				buf.WriteString("      <div class=\"sg-contacts-item\">")
				buf.WriteString(html.EscapeString(addr))
				buf.WriteString("</div>\n")
			}
			buf.WriteString("    </div>\n")
		}
		buf.WriteString("  </div>\n")
	}

	if c.Description != "" {
		writeRow(&buf, "description", c.Description)
	}

	buf.WriteString("</div>\n")
	return buf.Bytes()
}
