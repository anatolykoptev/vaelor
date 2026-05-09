# resolve_frame MCP tool / POST /resolve HTTP endpoint

Resolve a minified JavaScript stack frame `(url, line, column)` to its original source location using the companion `.map` file.

## Purpose

Browser stack traces report positions in the minified bundle, not the original source. `resolve_frame` fetches the source map for a given bundle URL, parses it, and maps the minified `(line, column)` back to `(file, line, function)` in the pre-built source tree.

Built on [`github.com/go-sourcemap/sourcemap`](https://github.com/go-sourcemap/sourcemap) — the same library used by Sentry-go's source map processing.

## Configuration

```
SOURCEMAP_ALLOWED_HOSTS=oxpulse.chat,piter.now
```

`SOURCEMAP_ALLOWED_HOSTS` is a comma-separated list of hostnames that `resolve_frame` and `POST /resolve` are permitted to fetch source maps from. When the env var is empty (default), both the MCP tool and the HTTP endpoint are **disabled** — tool registration is skipped and the route returns 503. This is intentional: fetching arbitrary URLs is a security risk.

To enable, set the env var in `compose/search.yml`:

```yaml
- SOURCEMAP_ALLOWED_HOSTS=${SOURCEMAP_ALLOWED_HOSTS:-oxpulse.chat,piter.now}
```

> **Note:** A separate krolik-server PR is needed to add this env var to `compose/search.yml`. It is not included in the go-code Phase α PR.

## MCP tool

### Input schema

```json
{
  "url":    "https://oxpulse.chat/_app/immutable/chunks/chunk-abc.js",
  "line":   42,
  "column": 193
}
```

| Field | Type | Description |
|---|---|---|
| `url` | string | Public URL of the minified JS bundle (not the `.map` file). Must match an allowed host. |
| `line` | int | 1-based line number from the browser stack frame. |
| `column` | int | 1-based column number from the browser stack frame. |

### Example call

```
resolve_frame url="https://oxpulse.chat/_app/immutable/chunks/chunk-abc.js" line=1 column=9
```

### Response

On success, returns a JSON object:

```json
{"file":"src/routes/room/+page.svelte","line":87,"column":4,"function":"handleJoin"}
```

On error (host not allowed, map not found, mapping absent), returns a plain-text error message.

## HTTP endpoint

`POST /resolve` exposes the same resolver over HTTP for use outside the MCP context (e.g. server-side symbolication of client error reports).

### Request

```
POST /resolve
Content-Type: application/json

{"url":"https://oxpulse.chat/_app/immutable/chunks/chunk-abc.js","line":42,"column":193}
```

### Response codes

| Code | Meaning |
|---|---|
| 200 | Resolved successfully. Body: `{"file":…,"line":…,"column":…,"function":…}`. |
| 400 | Request body is not valid JSON. |
| 403 | URL host is not in `SOURCEMAP_ALLOWED_HOSTS`. |
| 405 | Method is not POST. |
| 502 | Source map could not be fetched or the mapping was not found. |

### curl example

```bash
curl -sS -X POST http://localhost:8897/resolve \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://oxpulse.chat/_app/immutable/chunks/chunk-abc.js","line":1,"column":9}'
```

Expected (when map is available):

```json
{"file":"src/routes/room/+page.svelte","line":87,"column":4,"function":"handleJoin"}
```

## Cache behavior

Source maps are cached in an in-process LRU:

| Parameter | Value |
|---|---|
| Cache size | 100 entries (parsed `sourcemap.Consumer` objects) |
| TTL | 1 hour per entry |
| Body size cap | 16 MiB per `.map` file (prevents oversized maps from exhausting memory) |
| Eviction | LRU: oldest entry evicted when the cache is full |

The cache is shared between the MCP tool and the HTTP endpoint — a map fetched via one path is immediately available to the other.

## Error handling

- **Host not in allowlist:** returns `403` (HTTP) or an error text (MCP). The URL is not fetched.
- **Map fetch fails (non-200 or network error):** returns `502` (HTTP) or error text (MCP).
- **No mapping at (line, column):** returns `502` with message `"no mapping for <url>:<line>:<col>"`.
- **Parse error:** returns `502` with the parse error message.
