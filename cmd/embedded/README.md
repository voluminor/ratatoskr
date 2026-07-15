# Embedding examples

The subdirectories demonstrate four distinct embedding surfaces. They are independent Go modules and do not share a
release lifecycle.

| Directory                        | Purpose                                                   | Current status |
|----------------------------------|-----------------------------------------------------------|----------------|
| [http](http/README.md)           | Static web UI, node state, route trace, and topology scan | Does not build |
| [mobile](mobile/README.md)       | gomobile-compatible node lifecycle and forwarding API     | Does not build |
| [tiny-http](tiny-http/README.md) | Minimal plain and Yggdrasil HTTP listeners                | Builds         |
| [tiny-chat](tiny-chat/README.md) | Two-party HTTP chat over Yggdrasil                        | Builds         |

Build from inside the selected directory so Go uses that component's module metadata:

```bash
cd cmd/embedded/tiny-http
GOWORK=off go build .
```

These are examples, not hardened services. Their READMEs list listener exposure, authentication, fixed limits, and known
build failures.
