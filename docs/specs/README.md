# Project Specs Index

This directory contains the living technical specification for `goviesdezeproxyrewrite`.

## Documents

- `docs/specs/01-product-scope.md` - problem statement, goals, non-goals, constraints.
- `docs/specs/02-system-design.md` - hexagonal architecture, layers, and dependency rules.
- `docs/specs/03-http-contract.md` - routes, request validation, response semantics.
- `docs/specs/04-processing-pipeline.md` - archive extraction and conversion flow.
- `docs/specs/05-range-and-streaming.md` - range behavior, fallback rules, streaming model.
- `docs/specs/06-runtime-and-ops.md` - configuration, timeouts, startup/shutdown, operations.
- `docs/specs/07-testing-and-quality.md` - TDD strategy, coverage map, verification commands.
- `docs/specs/hexagonal-refactor.md` - original SDD refactor spec used during migration.

## Why this exists

- Keep behavior explicit when upstream systems are inconsistent.
- Make architecture intent durable across refactors.
- Provide one source of truth for future contributors and incident debugging.
