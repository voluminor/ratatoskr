package sigils

// // // // // // // // // //

// CSS contains minimal styles for sigil and nodeinfo HTML blocks.
// Embed into a <style> tag or append to an existing stylesheet.
var CSS = []byte(`.ni-block, .sg-block {
  margin: 0 0 4px 0;
  padding: 4px 8px;
  font-family: monospace;
  font-size: 14px;
  line-height: 1.4;
}
.ni-key, .sg-key {
  display: inline-block;
  min-width: 120px;
  font-weight: bold;
}
.ni-val, .sg-val {
  display: inline;
}
.sg-header {
  font-weight: bold;
  font-size: 15px;
  margin-bottom: 4px;
  text-transform: uppercase;
  letter-spacing: 1px;
}
.ni-json, .sg-json {
  white-space: pre-wrap;
  word-break: break-all;
}
`)
