# System Design

## Architecture style

The service uses a hexagonal architecture:

- **Inbound adapter**: HTTP handler (`internal/adapter/in/http`).
- **Application core**: archive service orchestration (`internal/app/archive`).
- **Outbound ports**: interfaces in `internal/port/out`.
- **Outbound adapters**:
  - upstream HTTP client (`internal/adapter/out/httpclient`)
  - archive gateway (`internal/adapter/out/archive`)
  - conversion gateway (`internal/adapter/out/converter`)

## Dependency direction

- Adapters depend on ports and app contracts.
- App depends on ports and domain model.
- Domain has no adapter dependencies.

This allows tests to drive behavior with mocks/fakes at the port boundary.

## Runtime composition

`main.go` wires concrete adapters into app service, then binds chi routes:

- `httpclient.New(...)` for metadata and file fetch.
- `archiveout.NewGateway()` for container operations.
- `converterout.NewGateway()` for format conversion.
- `archiveapp.NewService(...)` as core orchestrator.

## Why this design

- Keeps business flow testable without real HTTP/tooling.
- Enables independent hardening of archive/conversion adapters.
- Makes operational concerns (timeouts, transport tuning) local to composition layer.
