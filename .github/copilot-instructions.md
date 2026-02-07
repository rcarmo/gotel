# Copilot instructions

## Mandatory: use the Makefile

Use `make` targets for build/lint/test/format/dev flows whenever available.
If you need a new workflow step, add a Make target rather than running ad-hoc commands.

You MUST manage the workspace via the `Makefile`, reviewing and extending as needed.
You MUST perform all front-end maintenance/linting/testing using `bun`.

## Common workflows (expected Make targets)

- `make help` — list targets
- `make deps` — download and tidy Go dependencies
- `make install` — install project dependencies
- `make lint` / `make format` — static checks / formatting
- `make test` — run tests
- `make coverage` — run tests with coverage
- `make check` — run the project's standard validation pipeline
- `make build` — build the binary
- `make clean` — remove local build/test artifacts

## CI/CD convention

CI should call `make check` (or `make lint` + `make test` when `check` doesn't exist).
Keep CI logic minimal; prefer Make targets for consistency.
