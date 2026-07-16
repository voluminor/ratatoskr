# Contributing

Contributions should keep Ratatoskr small, embeddable, and predictable under concurrency and load.

## Contents

- [Development tree and releases](#development-tree-and-releases)
- [Requirements](#requirements)
- [Bootstrap](#bootstrap)
- [Tests and analysis](#tests-and-analysis)
- [Generated files](#generated-files)
- [Code and documentation](#code-and-documentation)
- [Pull requests](#pull-requests)
- [Security reports](#security-reports)
- [License](#license)

## Development tree and releases

The default branch is the development source tree. Generated `target` files are intentionally absent and must be
created before building or testing a fresh checkout.

Release tags are distribution source trees. The release workflow generates all required files, tests the generated
tree, includes `target`, and removes development-only generators, workspace files, CLI sources, and automation assets.
Consumers of a tagged release can use normal Go module commands without running project generators.

Base contributions on the default branch, not on a stripped release tag.

## Requirements

- Go 1.25 or newer.
- Bash for the root generator directives and repository scripts.
- Docker only for the live multi-node tests and throughput benchmark.
- `/home/su/go/bin/staticcheck` and `/home/su/go/bin/golangci-lint` when using the repository's configured development
  environment; equivalent current installations are acceptable elsewhere.

## Bootstrap

The repository helper performs the required workspace sync, generation, module tidy, and second generation pass:

```bash
bash _run/firststart.sh
```

After the helper has installed the generators and repository hooks, its generation sequence is:

```bash
mkdir -p target tmp
go work sync
go generate .
go list -m -f '{{if .Main}}{{.Dir}}{{end}}' all | while read -r dir; do
  [ -n "$dir" ] || continue
  go -C "$dir" mod tidy
done
go generate .
```

Run exactly `go generate .` at the root. Do not replace it with `go generate ./...`; the root entrypoint controls
generator ordering. Generator tools intentionally use their `@latest` versions.

## Tests and analysis

For root-module changes, run:

```bash
go test ./...
go test -race ./...
go vet ./...
staticcheck ./...
golangci-lint run ./...
```

Nested modules under `cmd` have their own status and commands in [`cmd/README.md`](cmd/README.md). Do not treat an
unrelated incomplete command target as permission to skip checks for the package being changed.

Use the Docker topology for changes that affect real peer connections, listeners, SOCKS, forwarding, NodeInfo,
topology queries, lifecycle, or shutdown:

```bash
bash tests/scripts/up.sh
```

The complete throughput run is intentionally separate because it takes about 35 minutes and saturates CPU and network
paths:

```bash
bash tests/scripts/up.sh --throughput --keep-state
```

Generated live-test data belongs under `tmp/tests`. Remove it when it is no longer needed. See
[`tests/README.md`](tests/README.md) and [THROUGHPUT_BENCHMARK.md](THROUGHPUT_BENCHMARK.md).

Tests must exercise observable contracts and failure modes rather than mirror implementation details. Include
concurrency, cancellation, resource ownership, malformed input, exhaustion, and abuse cases when they are relevant.

## Generated files

- Never edit or format generated files manually.
- Change the source, template, or generator, then run `go generate .`.
- Include generated output when it belongs to the change.
- Keep generators deterministic apart from inputs that are deliberately release metadata.
- Store temporary plans, profiles, traces, and test state only under `tmp`; do not stage them.

The sigil generator discovers the independent packages under `mod/sigils` and creates one registry entrypoint. Adding a
sigil should remain an atomic package change; see [`_generate/sigils/README.md`](_generate/sigils/README.md).

## Code and documentation

- Keep public contracts narrow and avoid dependencies that are not required by the package.
- All struct names end in `Obj`; all interface names end in `Interface`.
- Avoid tautological names that repeat their package.
- Comments, errors, logs, and tracked documentation are written in English.
- Comment exported API where the contract is not already clear from its declaration. Do not add line-by-line narration.
- Preserve caller ownership by copying mutable configuration and returned data where the contract requires it.
- Every component that accepts asynchronous work must have explicit shutdown and resource ownership.
- Review every change for high load, resource exhaustion, malicious input, and unnecessary complexity.
- Update the root README, module README, GoDoc, and examples when their contract changes.

## Pull requests

Keep a pull request focused on one coherent change. Its description should state:

- the behavior or problem being changed;
- why the chosen design fits the package boundary;
- compatibility or migration impact;
- tests, analyzers, and live checks performed;
- remaining risks or deliberately unsupported cases.

Use clear imperative commit subjects. Explain breaking API or behavior changes in the commit body; the project does not
retain a compatibility layer when a smaller, clearer contract is intentionally adopted.

## Security reports

Do not open a public issue or pull request for a suspected vulnerability. Follow [SECURITY.md](SECURITY.md).

## License

By contributing, you agree that your work is licensed under the project's [LGPL-2.1 license](LICENSE).
