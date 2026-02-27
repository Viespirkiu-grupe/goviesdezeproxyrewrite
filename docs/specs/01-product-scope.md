# Product Scope

## Purpose

`goviesdezeproxyrewrite` is an HTTP proxy service for file delivery.
It fetches file metadata and file content from upstream services, optionally extracts files from containers (zip/eml/msg/archives), optionally converts content format, and serves deterministic HTTP responses to clients.

## Primary goals

- Keep external URL contract stable for clients.
- Provide robust behavior even when upstream is inconsistent.
- Isolate business flow from transport/integration details.
- Support efficient streaming for non-range passthrough traffic.

## Non-goals

- No event sourcing, aggregate modeling, or replay pipeline.
- No UI/frontend rendering.
- No persistence owned by this service.
- No multi-range (`bytes=0-10,20-30`) support.

## Design constraints

- Go + chi router.
- Hexagonal structure with clear adapter boundaries.
- Output ports hide archive and conversion implementation details.
- Behavior-first TDD for critical request paths.

## Key quality attributes

- Correctness of response semantics (status, headers, body).
- Predictable range behavior.
- Graceful degradation when upstream is down or malformed.
- Operational transparency via logs and configurable timeouts.
