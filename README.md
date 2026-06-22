# agent-proxy

> Transparent debugging proxy for MCP, A2A, and ACP — with live visibility and human-in-the-loop control.

A lightweight, open-source debugging proxy for agentic AI protocols. Transparently intercepts and visualizes **MCP**, **A2A**, and **ACP** messages with a live web UI — and can pause consequential calls for human approval before they reach your agent.

```
┌──────────────┐     ┌─────────────────┐     ┌──────────────┐
│  MCP Host /  │────►│   agent-proxy   │────►│  Your Agent  │
│  A2A Client  │◄────│  (transparent)  │◄────│   / Server   │
└──────────────┘     └────────┬────────┘     └──────────────┘
                               │
                        http://localhost:7700/ui
```

## Why agent-proxy?

Debugging AI agents is blind work. When a tool call fails, a response is malformed, or latency spikes, you have no visibility into the raw protocol exchange between your agent and its tools or peers — you're left reading logs and guessing.

agent-proxy is a zero-config, transparent debugging proxy for the three protocols behind modern AI agents. Drop it in front of any agent or MCP server and instantly see every message flowing through it, live, in your browser.

**Why not existing tools?** MCP Inspector only does interactive testing — it doesn't observe live sessions. LangSmith/Langfuse operate at the LLM-API layer and miss the MCP/A2A plumbing entirely. agent-proxy fills the gap: protocol-layer visibility, any transport, zero code changes.

## Features

- **Multi-protocol, auto-detected** — MCP (stdio + HTTP/SSE), A2A, and ACP. Detection inspects content-type, URL path, and body shape; no configuration needed.
- **Two proxy modes** — `stdio` wraps any MCP subprocess (your client never knows it's there); `http` reverse-proxies any HTTP agent.
- **Live web UI** — protocol filter tabs, direction badges, expandable syntax-highlighted JSON, copy-per-entry, auto-refresh. Pure HTML + vanilla JS, zero build step.
- **Human-in-the-loop** — pause MCP `tools/call` and A2A/ACP submissions for a human to approve, edit, or reject before they reach the agent.
- **Performance stats** — per-protocol P50 / P95 / P99 latency and error counts at `/api/stats`, computed live from the ring buffer.
- **OpenTelemetry export** — every message becomes an OTEL span; ship to Langfuse, Jaeger, Grafana Tempo, Datadog, or Honeycomb with one flag.
- **File logging** — `--log-file` appends every message as NDJSON for `jq`, CI, or a log aggregator.
- **Production-ready** — single static Go binary (no runtime deps), Docker image + Compose, Kubernetes sidecar pattern.
- **REST API** — query and clear captured messages programmatically for test harnesses and dashboards.

## Supported Protocols

| Protocol | Transport | Detection |
|---|---|---|
| MCP | stdio (JSON-RPC newline-delimited) | `"jsonrpc":"2.0"` in body |
| MCP | HTTP / SSE | `text/event-stream` content-type |
| A2A | HTTP/JSON | `/a2a` path or `parts`+`role` body fields |
| ACP | HTTP/REST | `/runs` path or `agent_id` body field |

## Quick Start

### Prerequisites
- Go 1.26+ or Docker

### Run from source

```bash
git clone https://github.com/agentproxy/agent-proxy
cd agent-proxy
go build -o agent-proxy .

# Debug an MCP stdio server
./agent-proxy stdio --cmd "python weather_server.py"

# Debug an HTTP agent (A2A / ACP / MCP HTTP)
./agent-proxy http --listen 7701 --target http://localhost:8080
```

Open **http://localhost:7700/ui** to inspect messages.

### Run with Docker

```bash
docker run -p 7700:7700 -p 7701:7701 \
  ghcr.io/agentproxy/agent-proxy:latest \
  http --listen 7701 --target http://host.docker.internal:8080
```

### Run with Docker Compose

```bash
AGENT_TARGET=http://my-agent:8080 docker compose up
```

### Kubernetes sidecar

See [`k8s/deployment.yaml`](k8s/deployment.yaml) for a ready-to-use sidecar pattern.
Traffic routed to `:7701` is forwarded to your agent on `:8080`; UI is exposed on `:7700`.

## Human-in-the-Loop

Run the HTTP proxy with `--require-approval` to **pause consequential messages** and require a human to approve, edit, or reject them in the web UI before they reach the agent:

```bash
agent-proxy http --target http://localhost:8080 --require-approval
```

By default only the actions that *do* something are gated — MCP `tools/call` and A2A/ACP submissions (POST). Discovery and status-poll traffic (`initialize`, `tools/list`, GET polls) flows through untouched, so the proxy stays transparent.

A paused message appears at the top of `http://localhost:7700/ui` with three choices:

| Action | Effect |
|---|---|
| **Approve** | Forward the message upstream unchanged |
| **Edit** | Modify the JSON payload (e.g. tweak tool arguments), then forward |
| **Reject** | Block the message; the caller receives `403 Forbidden` |

If no decision is made within `--approval-timeout` (default `2m`), the message is auto-rejected (fail-closed).

> Human-in-the-loop currently applies to HTTP mode.

## OpenTelemetry Export

agent-proxy can export every captured message as an OTEL span to any OTLP-compatible backend:

```bash
# Langfuse (via OTEL ingest endpoint)
agent-proxy http --target http://localhost:8080 \
  --otel-endpoint https://cloud.langfuse.com/api/public/otel

# Jaeger (local)
agent-proxy http --target http://localhost:8080 \
  --otel-endpoint http://localhost:4318

# Grafana Tempo / Datadog / Honeycomb — same flag, different URL
```

Each intercepted request/response pair becomes an OTEL span with these attributes:

| Attribute | Example |
|---|---|
| `agent.protocol` | `mcp`, `a2a`, `acp`, `mcp-sse` |
| `agent.direction` | `request`, `response`, `stdio-in`, `stdio-out` |
| `agent.method` | `tools/call`, `initialize` |
| `agent.path` | `/a2a/tasks/send` |
| `agent.latency_ms` | `42` |
| `agent.body` | JSON payload (truncated at 4096 chars) |
| `http.response.status_code` | `200` |
| `mcp.method` | `tools/call` (MCP only) |

> The `--otel-endpoint` flag is optional. Without it, agent-proxy runs in local-only mode with the web UI only.

## File Logging

Pass `--log-file <path>` (either mode) to append every captured message as newline-delimited JSON:

```bash
agent-proxy http --target http://localhost:8080 --log-file capture.ndjson
```

Pipe it into `jq`, tail it in CI, or feed it to a log aggregator.

## CLI Reference

```
agent-proxy http  --target <url> [--listen <port>] [--ui-port <port>] [--otel-endpoint <url>] [--log-file <path>] [--require-approval] [--approval-timeout <dur>]
agent-proxy stdio --cmd "<command>"               [--ui-port <port>] [--otel-endpoint <url>] [--log-file <path>]
```

| Flag | Default | Description |
|---|---|---|
| `--target` | — | Upstream agent URL (HTTP mode, required) |
| `--listen` | 7701 | Port for proxied traffic (HTTP mode) |
| `--cmd` | — | Command to run as the MCP server (stdio mode, required) |
| `--ui-port` | 7700 | Port for the web UI and REST API |
| `--otel-endpoint` | — | OTLP HTTP endpoint for trace export (optional) |
| `--log-file` | — | Append captured messages as NDJSON to this file (optional) |
| `--require-approval` | false | Pause consequential messages for human approval (HTTP mode) |
| `--approval-timeout` | 2m | How long a paused message waits before it is auto-rejected |

## REST API

| Endpoint | Method | Description |
|---|---|---|
| `/api/messages` | GET | Fetch captured messages. Query: `?protocol=mcp&limit=50` |
| `/api/messages` | DELETE | Clear the message log |
| `/api/stats` | GET | Per-protocol latency percentiles (P50/P95/P99) and error counts |
| `/api/pending` | GET | List messages awaiting human approval |
| `/api/decide` | POST | Resolve a paused message: `{ "id": 1, "action": "approve\|reject\|edit", "body": "…" }` |
| `/ui` | GET | Web inspector UI |

## Who It's For

| Persona | Use case |
|---|---|
| MCP server developer | Verify your tool responses match the spec during local dev |
| Agent framework author | Debug A2A / ACP handshakes between agents |
| Platform / infra engineer | Add observability to a deployed agent fleet via a Kubernetes sidecar |
| QA / CI pipeline | Assert on captured traffic in automated tests via the REST API |

## Web UI

- Protocol filter tabs: **All · MCP · MCP SSE · A2A · ACP · Raw**
- Pending-approval panel when `--require-approval` is enabled
- Auto-refreshes every 2 seconds
- Expandable JSON with syntax highlighting
- Direction badges: `→ REQ` / `← RES` / `→ IN` / `← OUT`
- Copy-to-clipboard per message
- No build step — pure HTML + vanilla JS, easy to fork

## License

MIT
