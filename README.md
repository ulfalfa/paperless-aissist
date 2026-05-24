# paperless-aissist

AI-powered metadata extraction for [paperless-ngx](https://docs.paperless-ngx.com/).
Fetches documents, sends them to an OpenAI-compatible LLM endpoint (e.g. LiteLLM, vLLM, or Ollama via `/v1/`) for analysis, and writes title, tags, correspondent, document type and creation date back — automatically.

## Features

- **Auto-Scanner** — periodically processes documents tagged with a configurable trigger tag
- **RAG Chat** — natural language chat against your entire document archive (vector search)
- **Analyze** — analyze any document on demand via web UI or CLI
- **Tag Cleanup** — review and consolidate tags with AI assistance
- **MCP Server** — integrate with Claude Code and other MCP-compatible tools

## Quick Install (Linux)

```bash
curl -fsSL https://gitlab.com/ulfalfa/paperless-aissist/-/raw/main/install.sh | sudo bash
```

After installation, edit the config and start the service:

```bash
sudo nano /opt/paperless-aissist/.env
sudo systemctl start paperless-aissist
```

The web UI is then available at `http://<your-server>:8080`.

## Requirements

- An OpenAI-compatible LLM endpoint (e.g. [LiteLLM](https://litellm.ai/), [vLLM](https://vllm.ai/), or [Ollama](https://ollama.com/) with `/v1/` API)
- A text generation model accessible at that endpoint
- An embedding model for RAG chat
- A running paperless-ngx instance with API access

## Configuration

| Variable | Default | Description |
|---|---|---|
| `OPENAI_BASE_URL` | `http://localhost:11434` | OpenAI-compatible API base URL |
| `OPENAI_MODEL` | `gemma4:latest` | Model for metadata extraction |
| `OPENAI_API_KEY` | — | Bearer token / API key (optional) |
| `PAPERLESS_BASE_URL` | — | Base URL of your paperless-ngx instance |
| `PAPERLESS_API_TOKEN` | — | API token (paperless-ngx → Settings → API tokens) |
| `PORT` | `8080` | HTTP server port |
| `SCAN_TAG` | — | Tag that triggers auto-processing (removed after) |
| `SCAN_INTERVAL` | `0` | Scanner interval in minutes (`0` = disabled) |
| `AI_TAG` | `ai-gen` | Tag added to every AI-processed document |
| `STRICT_METADATA` | `false` | If `true`, unknown tags/correspondents/types are not created |
| `EMBED_MODEL` | `bge-m3:latest` | Embedding model for RAG chat |
| `INDEX_PATH` | `./data` | Directory for the vector index |

Config and prompt can also be edited at runtime via the web UI.

## CLI Usage

```bash
paperless-aissist --server                        # start web server
paperless-aissist --pdf <file.pdf>                # analyze local PDF (stdout)
paperless-aissist --id <N>                        # fetch and analyze document
paperless-aissist --id <N> --dry-run              # analyze without writing back
```

## Manual Installation

1. Download the latest binary for your platform from the [releases page](https://gitlab.com/ulfalfa/paperless-aissist/-/releases)
2. Copy the [service file](paperless-aissist.service) to `/etc/systemd/system/`
3. Create `/opt/paperless-aissist/.env` from [.env.example](.env.example)
4. Run `systemctl enable --now paperless-aissist`
