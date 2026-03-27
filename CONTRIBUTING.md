# Contributing

Thanks for your interest in improving ratatoskr!

## Quick Start

* **Fork** the repo and create a feature branch: `git checkout -b feat/short-descriptor`.
* **Make changes** in small, focused commits.
* **Format** code with `gofmt` and keep imports tidy (`go fmt ./...`).
* **Test** everything: `go test ./...`. Add/adjust tests for any behavior you change.
* **Cross-build**: the CI verifies compilation on 25 platform targets — run `GOOS=linux GOARCH=arm64 go build ./...`
  locally if you're touching platform-sensitive code.
* **Document** public APIs and update README examples if behavior changes. Both `README.md` (English) and
  `README.RU.md` (Russian) must be kept in sync.

## Style & Scope

* Prefer small PRs that solve one problem.
* All Go struct names must end with `Obj`, all interface names must end with `Interface`.
* Avoid tautological names — don't repeat the package name in identifiers (e.g., `telemetry.Obj` not
  `telemetry.TelemetryObj`).
* Comments, errors, and logs must be in English.
* Keep dependencies minimal; stick to the standard library when possible.
* See `CLAUDE.md` for the full set of code conventions.

## Commit Messages

* Use clear, imperative subjects, e.g., `fix: handle empty peer list`.
* If a change is breaking, include `BREAKING CHANGE:` in the body and explain the migration.

## Pull Requests

* Describe **what** and **why** (link related issues).
* Include usage notes and test coverage.
* CI runs tests on Linux, macOS, and Windows, plus cross-compilation for 25 targets — all checks must pass.
* Be ready to address review feedback; we aim for constructive, concise reviews.

## Reporting Issues

* Provide steps to reproduce, expected vs. actual behavior, environment details (Go version, OS, architecture), and logs
  if relevant.

## Licensing

By submitting a contribution, you agree that it will be licensed under the project's **LGPL-2.1** license.
