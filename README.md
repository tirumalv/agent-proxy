# agent-proxy

A lightweight, open-source debugging proxy for agentic AI protocols.  
Transparently intercepts and visualizes **MCP**, **A2A**, and **ACP** messages with a live web UI.

```
┌──────────────┐     ┌─────────────────┐     ┌──────────────┐
│  MCP Host /  │────►│   agent-proxy   │────►│  Your Agent  │
│  A2A Client  │◄────│  (transparent)  │◄────│   / Server   │
└──────────────┘     └────────┬────────┘     └──────────────┘
                               │
                        http://localhost:7700/ui
```

## Supported Protocols

| Protocol | Transport | Detection |
|---|---|---|
| MCP | stdio (JSON-RPC newline-delimited) | `"jsonrpc":"2.0"` in body |
| MCP | HTTP / SSE | `text/event-stream` content-type |
| A2A | HTTP/JSON | `/a2a` path or `parts`+`role` body fields |
| ACP | HTTP/REST | `/runs` path or `agent_id` body field |

## Quick Start

### Prerequisites
- Go 1.23+ or Docker

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

## CLI Reference

```
agent-proxy http  --listen <port> --target <url> [--ui-port <port>]
agent-proxy stdio --cmd "<command>"               [--ui-port <port>]
```

| Flag | Default | Description |
|---|---|---|
| `--listen` | 7701 | Port for proxied traffic (HTTP mode) |
| `--target` | — | Upstream agent URL (HTTP mode, required) |
| `--cmd` | — | Command to run as MCP server (stdio mode, required) |
| `--ui-port` | 7700 | Port for web UI and `/api/messages` REST endpoint |

## REST API

| Endpoint | Method | Description |
|---|---|---|
| `/api/messages` | GET | Fetch captured messages. Query: `?protocol=mcp&limit=50` |
| `/api/messages` | DELETE | Clear the message log |
| `/ui` | GET | Web inspector UI |

## Web UI

- Protocol filter tabs: **All · MCP · MCP SSE · A2A · ACP · Raw**
- Auto-refreshes every 2 seconds
- Expandable JSON with syntax highlighting
- Direction badges: `→ REQ` / `← RES` / `→ IN` / `← OUT`
- Copy-to-clipboard per message
- No build step — pure HTML + vanilla JS, easy to fork

## License

MIT
