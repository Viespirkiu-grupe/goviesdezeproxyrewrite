# Hexagonal Refactor Spec (SDD)

## User story
As a maintainer, I want proxy request handling split into hexagonal layers so that business flow is testable without HTTP/network/process dependencies and adapters can change independently.

## Acceptance criteria
- Core request orchestration lives in application service package and has unit tests with mocked ports.
- HTTP layer only parses request/params and writes response from service output.
- Outbound integrations (upstream HTTP, archive extraction, conversion tools) are hidden behind output ports.
- Existing HTTP route contract and basic response behavior stay compatible.

## Non-goals
- No change to product endpoint surface.
- No migration to event sourcing.
- No external CQRS transport split.

## Constraints
- Idiomatic Go (`gofmt`, explicit error handling, small interfaces).
- Keep compatibility with current `chi` routing and existing environment variables.

## Edge cases
- Invalid `id`, `dokId`, `fileId` return `400`.
- Non-2xx proxy-info and non-2xx upstream file responses are passed through.
- Missing `fileUrl` in proxy info returns `502`.
- Archive target miss returns `404`.
- Image conversion requested for non-image source returns `400`.

## Verification plan
- `go test ./...`
- `go build ./...`
