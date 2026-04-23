# cube-mcp

A Model Context Protocol server that exposes the [Cube](https://cube.dev/)
semantic layer as MCP tools over streamable HTTP. Three tools are provided:

- `meta` — returns the Cube semantic model (cubes, views, measures, dimensions,
  joins, and descriptions). Call this first to understand what data is
  available.
- `query` — executes a structured Cube query and returns results. Accepts the
  standard Cube query shape (`measures`, `dimensions`, `filters`,
  `timeDimensions`, `limit`, `offset`, `order`, `timezone`).
- `dimension_search` — searches for matching values of a dimension using a
  case-insensitive contains filter. Useful for resolving ambiguous references
  like store names, product names, or categories before querying.

cube-mcp does not mint tokens. The `Authorization` header from the incoming
MCP request is forwarded verbatim to Cube on each tool call, so the calling
agent or MCP proxy is responsible for providing a valid Cube JWT. If no
`Authorization` header is present, the request is forwarded without one
and Cube returns its usual 401.

Written in Go using the official
[`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk).

## Build

```bash
go mod tidy          # once, to materialize go.sum
go build ./...
```

## Test

```bash
go test -race ./...
```

## Run

```bash
CUBE_API_URL='https://cube.example.com/cubejs-api' ./cube-mcp
```

## Docker

CI publishes a multi-arch manifest to
`ghcr.io/voriteam/cube-mcp` on every push to `main` with three tags:
`<full-sha>` (40-char), `<short-sha>` (7-char), and `latest` (see
`.github/workflows/ci.yml`). To build locally:

```bash
docker buildx build --platform linux/amd64,linux/arm64 \
  --build-arg COMMIT_SHA=$(git rev-parse HEAD) \
  -t ghcr.io/voriteam/cube-mcp:latest \
  --load \
  .
```

Pass `--push` instead of `--load` to publish (requires `docker login ghcr.io`).
The `COMMIT_SHA` build arg is baked into the image as an env var and logged at
startup, so a running container's commit is visible in its logs.

## Environment variables

| Variable       | Required | Description                                                    |
| -------------- | -------- | -------------------------------------------------------------- |
| `CUBE_API_URL` | Yes      | Base URL of the Cube REST API (e.g. `https://.../cubejs-api`). |
| `PORT`         | No       | Listen port. Defaults to `8003`.                               |

## Endpoints

- `POST`/`GET`/`DELETE /mcp` — MCP streamable-HTTP transport.
- `GET /healthz` — liveness probe, always returns `200 ok`.
