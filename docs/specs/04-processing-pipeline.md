# Processing Pipeline

## Core flow

1. Resolve logical request id from route params.
2. Fetch proxy info from upstream metadata endpoint.
3. Fetch file body from upstream file endpoint.
4. Optionally extract target file from container.
5. Optionally convert output format.
6. Return response with normalized headers/body.

## Extraction behavior

Extraction is triggered when target path is present from:

- `info.extract` from upstream metadata, or
- wildcard path segment in incoming URL.

Special case:

- path value `0` is treated as no extraction.

Extraction modes:

- `eml`: extract attachment by name/index.
- `msg`: convert msg -> eml, then extract attachment.
- default archive: list files, choose best match, extract file.

## Best match strategy

Match order:

1. exact normalized path match
2. exact basename match
3. fuzzy similarity score threshold

This prioritizes deterministic match behavior while still supporting imperfect caller paths.

## Conversion behavior

- Validate requested target format.
- Reject image-target conversion if source extension is not image-like.
- For `pdf`: use image->pdf or document->pdf flow.
- For image targets: use image conversion adapter.
