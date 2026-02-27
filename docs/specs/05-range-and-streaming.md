# Range and Streaming Specification

## Objectives

- Respect client range semantics.
- Avoid unnecessary buffering for non-range traffic.
- Defend against upstream range misbehavior.

## Non-range requests

If client does not send `Range`, proxy streams response directly with `io.Copy`.

## Range requests

### Upstream forwarding rule

Range headers are forwarded upstream only for raw passthrough mode:

- no extraction
- no conversion

For transformed outputs, range is applied locally on final output.

### Closed single-range handling

Supported parser target:

- `bytes=<start>-<end>` (single closed range)

When proxy receives range-capable upstream body (`200` or `206`):

1. Read at most `expectedLen + 1` bytes.
2. If bytes read == `expectedLen`, return `206` and that exact slice.
3. If bytes read > `expectedLen`, treat as upstream overread/ignore and buffer remainder from same stream, then apply local range with `ServeContent`.
4. If fewer bytes than expected, return `416`.

This ensures client never gets more bytes than requested.

## Header rules

- `Content-Length` is forwarded only for raw passthrough 2xx.
- `Content-Range` / `Accept-Ranges` are forwarded only when upstream status indicates range semantics (`206`/`416`) or when proxy sets the range response.

## Tradeoffs

- Non-range path is memory-efficient (streaming).
- Range path may buffer local chunks/full body in misbehavior scenarios to preserve correctness.
