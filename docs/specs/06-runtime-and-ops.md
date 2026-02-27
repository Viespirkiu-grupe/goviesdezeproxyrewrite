# Runtime and Operations

## Environment variables

Required:

- `MAIN_SERVER`
- `PROXY_API_KEY`

Optional:

- `PROXY_PORT` (default `4000`)
- `PROXY_UPSTREAM_DIAL_TIMEOUT` (seconds, default `10`)
- `PROXY_UPSTREAM_RESPONSE_HEADER_TIMEOUT` (seconds, default `30`)
- conversion-related envs from deployment profile

## HTTP transport strategy

- Keep-alive enabled.
- Explicit dial timeout.
- Explicit response-header timeout.
- No global client timeout, request context controls cancellation.

This avoids indefinite hangs during VPN/outage scenarios while preserving long-running valid transfers.

## Startup and shutdown

- Service starts chi HTTP server with middleware (request id, real IP, logger, recoverer).
- Graceful shutdown on `SIGINT`/`SIGTERM` with 10-second deadline.

## Temporary file hygiene

- Background cleanup task removes stale `/tmp` entries older than threshold.
- Archive/conversion adapters also clean temporary files on close paths.

## Operational recommendations

- Monitor rates of `4xx/5xx` by route family.
- Track upstream latency and timeout counts.
- Track range fallback frequency as upstream health indicator.
