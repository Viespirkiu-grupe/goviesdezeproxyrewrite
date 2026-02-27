# Testing and Quality

## Testing approach

The project uses TDD-oriented coverage around behavior boundaries:

- unit tests for app orchestration (`internal/app/archive/service_test.go`)
- adapter integration tests for HTTP contract (`internal/adapter/in/http/handler_integration_test.go`)
- focused tests for validation helper logic (`internal/adapter/in/http/handler_test.go`)

## Critical scenarios covered

- id parsing and route input validation
- upstream non-2xx passthrough
- archive extraction and best-match behavior
- conversion validation and execution paths
- range behavior for raw passthrough and transformed outputs
- fallback behavior for inconsistent upstream range responses

## Verification commands

- `go test ./...`
- `go build ./...`

## Quality guardrails

- keep small, explicit interfaces for ports
- avoid leaking adapter details into app layer
- preserve deterministic status/header behavior
- prefer additive, regression-first tests for bug fixes

## Documentation maintenance rule

When behavior changes in range handling, extraction rules, or header forwarding, update:

- `docs/specs/03-http-contract.md`
- `docs/specs/05-range-and-streaming.md`
- related tests in app and HTTP adapter packages
