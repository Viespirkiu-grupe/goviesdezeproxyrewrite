# HTTP Contract

## Routes

Supported route shapes:

- `/{dokId}/{fileId}`
- `/{dokId}/{fileId}/*`
- `/{id}`
- `/{id}/*`

Where:

- `dokId` and `fileId` must be numeric.
- `id` must be numeric or 32-char MD5-like hex.

## Query parameters

- `convertTo`: optional target format.
- `index`: optional attachment index for eml/msg extraction.

Allowed `convertTo` values:

- `pdf`
- `jpg`, `jpeg`, `png`, `tif`, `tiff`, `bmp`, `prn`, `gif`, `jfif`, `heic`, `webp`

Unsupported `convertTo` returns `400`.

## Status behavior

- Validation errors: `400`.
- Missing upstream `fileUrl`: `502`.
- Upstream non-2xx responses: passed through status/body.
- Archive target miss: `404`.
- Conversion errors: `500`.

## Response headers

Proxy may emit:

- `Content-Type`
- `Content-Disposition`
- `Cache-Control`
- `Content-Length` (raw passthrough only)
- `Accept-Ranges` / `Content-Range` when appropriate

Headers are intentionally filtered by mode (raw passthrough vs transformed output) to avoid semantically incorrect metadata.
