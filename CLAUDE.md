# CLAUDE.md — paperless-aissist

## What this project is

AI-powered metadata extraction and document assistant for [paperless-ngx](https://docs.paperless-ngx.com/). Uses [Ollama](https://ollama.com/) for LLM inference (text generation, embeddings, tool calling). Single Go binary with embedded web UI.

## Build & run

```bash
make build                     # local binary (version=dev)
make run                       # go run . --server
./paperless-aissist --server   # start web UI on :8080
./paperless-aissist --pdf x.pdf  # analyze a local PDF (JSON to stdout)
./paperless-aissist --id 42    # fetch + analyze paperless doc #42
```

Build requires Go 1.25+. No CGO needed. Version is injected via `-ldflags="-X main.version=vX.Y.Z"`.

## Project structure

```
main.go                    Entry point (CLI modes: --server, --pdf, --id)
internal/
  auth/                    Session cookies, bcrypt credentials, setup flow
  config/                  .env loading, prompt template (prompt.md), Save/Load
  logger/                  In-memory ring buffer (200 entries), /api/logs
  mcp/                     Model Context Protocol HTTP handler (/mcp/*)
  ollama/                  Ollama client: /api/generate, /api/chat, /api/embed
  paperless/               paperless-ngx REST client (docs, tags, correspondents, types)
  pdf/                     PDF text extraction
  promptlog/               JSONL logging for LLM calls (LOG_PROMPTS=true)
  rag/                     Vector index (chromem-go) + multi-round chat with tool calling
  scanner/                 Background auto-scan loop (processes docs with SCAN_TAG)
  tools/                   Unified tool dispatcher (used by both RAG chat and MCP)
  types/                   Shared domain types (AppConfig, AnalysisResult, ChatResult, …)
  validate/                Result sanitization, date normalization, AI tag injection
web/
  embed.go                 go:embed for all templates
  templates/               ~15 HTML files (layout, chat, analyze, settings, prompts, …)
```

## Key architecture decisions

- **Single file server**: `web/server.go` contains all HTTP handlers, routes, and template helpers. Router is chi/v5.
- **Embedded templates**: All HTML is embedded in the binary via `web/embed.go`. HTMX 2.0 for dynamic updates, PicoCSS for styling, marked.js for markdown rendering.
- **Tool abstraction**: `tools.Execute()` is the single dispatch point for both MCP and RAG chat tool calls. Adding a new tool means updating `tools/tools.go` and `rag/query.go:buildTools()`.
- **Stateless chat**: Conversation history lives client-side in a hidden `<input>` field. Each request sends the full history to the server, which prepends the system prompt and RAG context before calling Ollama.
- **Ollama keep-alive**: All requests set `keep_alive: -1` to keep models loaded in GPU memory indefinitely.

## Configuration

All config is via environment variables (`.env` file). See `.env.example`. Key vars:

- `OLLAMA_HOST`, `OLLAMA_MODEL`, `EMBED_MODEL` — LLM connection
- `PAPERLESS_BASE_URL`, `PAPERLESS_API_TOKEN` — paperless-ngx API
- `SCAN_TAG`, `SCAN_INTERVAL` — auto-scanner trigger
- `LOG_PROMPTS=true` — enables JSONL logging of all LLM calls to `data/prompts.jsonl`
- `INDEX_PATH=./data` — vector DB + credentials storage

Config and prompt template can also be edited at runtime via the Settings page.

## Conventions

- **Go style**: standard library conventions, exported names are CamelCase, unexported are camelCase
- **Error handling**: `(result, error)` return tuples, errors are returned to caller or logged via `logger`
- **Concurrency**: `sync.Mutex` for shared state (logger, scanner, indexer), `sync.Map` for chat jobs
- **No tests**: project has no unit tests; testing is manual
- **Commits**: conventional commits (`feat:`, `fix:`, `chore:`) — enforced by CI commitlint. semantic-release auto-generates CHANGELOG and GitLab releases
- **Templates**: server-side Go HTML templates with HTMX partials. Template names match filenames. Use `renderPartial()` for HTMX fragment responses, `render()` for full pages

## Common tasks

**Adding a new tool** (for RAG chat and/or MCP):
1. Add the handler case in `internal/tools/tools.go` → `Execute()` switch
2. Add the tool definition in `internal/rag/query.go` → `buildTools()`
3. If also needed for MCP: register in `internal/mcp/server.go`

**Adding a new web page**:
1. Create `web/templates/pagename.html` (define "pagename.html" + "content")
2. Add route in `web/server.go` → `NewRouter()`
3. Add nav link in `web/templates/layout.html`
4. Register template in both `parseGlobal()` and `parsePage()` template lists

**Adding a new API endpoint**:
1. Add handler function in `web/server.go`
2. Register route in `NewRouter()`

## Things to watch out for

- `web/server.go` is large (~1300 lines). All handlers, view models, and helpers live here.
- Template parsing happens in two places: `parseGlobal()` (for layout-only partials) and `parsePage()` (for full pages). New templates must be added to BOTH lists or parsing will fail silently.
- The RAG system prompt instructs the LLM to avoid unnecessary tool calls. If the LLM makes too many `search_documents` calls, tune the system prompt in `rag/query.go`.
- Ollama responses can be slow if models are not loaded. The `keep_alive: -1` flag prevents unloading but only takes effect after the first request.
