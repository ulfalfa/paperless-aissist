# [0.26.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.25.0...v0.26.0) (2026-05-24)


### Features

* add sort_by and increase default max_docs in search_documents_semantic ([6a84e2f](https://gitlab.com/ulfalfa/paperless-aissist/commit/6a84e2ff6724a4c2d0f1fa4b60c54925842aa26e))

# [0.25.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.24.0...v0.25.0) (2026-05-24)


### Features

* improve RAG index freshness, tool descriptions, and index status UI ([c91e26f](https://gitlab.com/ulfalfa/paperless-aissist/commit/c91e26fb150e5e55a350f4a617b20694132b7df2))

# [0.24.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.23.0...v0.24.0) (2026-05-24)


### Features

* add upload_document tool to MCP, RAG chat, and dispatcher ([4229677](https://gitlab.com/ulfalfa/paperless-aissist/commit/422967724532799c4346aaa110f8b266ec7f75b8))

# [0.23.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.22.1...v0.23.0) (2026-05-24)


### Features

* add find_documents, semantic search, tagging and download tools ([7ca24bf](https://gitlab.com/ulfalfa/paperless-aissist/commit/7ca24bf4b043d1215657c5f926ef1d891acae8a4))

## [0.22.1](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.22.0...v0.22.1) (2026-05-16)


### Bug Fixes

* make MCP server stateless to avoid session-id requirement ([6bd0db8](https://gitlab.com/ulfalfa/paperless-aissist/commit/6bd0db8179b1ebc23d27153e3ef7697d3f4eb2ba))

# [0.22.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.21.2...v0.22.0) (2026-05-15)


### Features

* add MCP_API_TOKEN config and rag_search tool ([52be61a](https://gitlab.com/ulfalfa/paperless-aissist/commit/52be61a8ccf82761aa071f49ee2aa908c6f65a87))

## [0.21.2](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.21.1...v0.21.2) (2026-05-14)


### Bug Fixes

* correct chat input proportions and login button colour ([fd2cd2e](https://gitlab.com/ulfalfa/paperless-aissist/commit/fd2cd2ed3d16dda155a85dd8a9d0810cc34641b2))

## [0.21.1](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.21.0...v0.21.1) (2026-05-14)


### Bug Fixes

* improve layout proportions and dark mode polish ([cd6e7b1](https://gitlab.com/ulfalfa/paperless-aissist/commit/cd6e7b1eedb5eff883f479d94ae210f0956cafc2))

# [0.21.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.20.0...v0.21.0) (2026-05-14)


### Features

* model selection dropdowns in settings ([e1b82cb](https://gitlab.com/ulfalfa/paperless-aissist/commit/e1b82cb61d268c09b8b1e102cc44f15d309f4b6c))
* redesign compare page with fixed reference side and save-to-paperless ([94932a9](https://gitlab.com/ulfalfa/paperless-aissist/commit/94932a934f95b6e0ccc4e187915d008d60260e91))
* switch from Ollama to OpenAI-compatible API (LiteLLM) ([e8410e5](https://gitlab.com/ulfalfa/paperless-aissist/commit/e8410e53814c96e36842ff23c80800085353311f))

# [0.20.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.19.0...v0.20.0) (2026-04-20)


### Features

* enrich semantic search queries with inferred document types and allow parallel keyword search ([d5ef917](https://gitlab.com/ulfalfa/paperless-aissist/commit/d5ef917fd556cf313a460c8d6f56f94ba68e00a9))

# [0.19.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.18.0...v0.19.0) (2026-04-19)


### Features

* show context window usage as progress bar (fetched from Ollama /api/show) ([5d7eb65](https://gitlab.com/ulfalfa/paperless-aissist/commit/5d7eb654600eab22c761c7ab0aaa8db2dd8441c9))

# [0.18.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.17.2...v0.18.0) (2026-04-19)


### Features

* show prompt token count (context usage) in chat UI ([5c94918](https://gitlab.com/ulfalfa/paperless-aissist/commit/5c94918566ca7b05746e251e63a83bcb86f3c0f7))

## [0.17.2](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.17.1...v0.17.2) (2026-04-19)


### Bug Fixes

* pass full document content to LLM in get_document and search_documents ([caac884](https://gitlab.com/ulfalfa/paperless-aissist/commit/caac884085a1604c692a51161f8d42a9d0d8c63d))

## [0.17.1](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.17.0...v0.17.1) (2026-04-19)


### Bug Fixes

* guide LLM to use get_document for IDs and search_documents for exact titles ([a345b13](https://gitlab.com/ulfalfa/paperless-aissist/commit/a345b13d9d9ca7495244a300ca25a1dd9d941178))

# [0.17.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.16.1...v0.17.0) (2026-04-19)


### Features

* round-robin pinned doc buffer and allow re-search on new topics ([481e5e4](https://gitlab.com/ulfalfa/paperless-aissist/commit/481e5e4cb57644f577531caec9f2c4701f52656a))

## [0.16.1](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.16.0...v0.16.1) (2026-04-15)


### Bug Fixes

* restore /analyze route and fix nav link href ([dce7a83](https://gitlab.com/ulfalfa/paperless-aissist/commit/dce7a837b010f4d683123ce84b80162a00a8b0ac))

# [0.16.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.15.1...v0.16.0) (2026-04-15)


### Features

* include date, correspondent, document type and tags in RAG index text ([75ff86a](https://gitlab.com/ulfalfa/paperless-aissist/commit/75ff86aa333ce1116178a3cae296f63e16936a3e))

## [0.15.1](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.15.0...v0.15.1) (2026-04-15)


### Bug Fixes

* instruct LLM to mention document titles and include doc IDs in RAG context ([21c5558](https://gitlab.com/ulfalfa/paperless-aissist/commit/21c5558ab094074c6db2f40079a18439a573921e))

# [0.15.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.14.0...v0.15.0) (2026-04-15)


### Features

* linkify document titles in chat answers to paperless-ngx ([1c6db02](https://gitlab.com/ulfalfa/paperless-aissist/commit/1c6db02d46025247edc70d745ded85e800e7b1a7))

# [0.14.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.13.0...v0.14.0) (2026-04-15)


### Features

* responsive mobile UI with hamburger sidebar ([17690f6](https://gitlab.com/ulfalfa/paperless-aissist/commit/17690f6a6ad61471ea58ed006a1b523fe0700da0))

# [0.13.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.12.0...v0.13.0) (2026-04-14)


### Features

* agentic RAG search, think toggle in chat, chat as start page ([afdf10f](https://gitlab.com/ulfalfa/paperless-aissist/commit/afdf10f87650d044eeeb3e2871a4ee08f0fb4dd5))

# [0.12.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.11.0...v0.12.0) (2026-04-13)


### Features

* pin found documents for follow-up chat queries ([e5df5ce](https://gitlab.com/ulfalfa/paperless-aissist/commit/e5df5ce7071f00607aedab1695ef74af0c56bb51))

# [0.11.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.10.1...v0.11.0) (2026-04-13)


### Features

* auto-sync RAG index after scan, move index controls to scan page ([ce3ebe9](https://gitlab.com/ulfalfa/paperless-aissist/commit/ce3ebe9de66b12beaf006ca2a5d7a1c64f0d03d4))

## [0.10.1](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.10.0...v0.10.1) (2026-04-13)


### Bug Fixes

* allow write access to app root dir in systemd unit ([4f9fb53](https://gitlab.com/ulfalfa/paperless-aissist/commit/4f9fb532bf3473f0c9af1fc8df2953c63ba722ea))

# [0.10.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.9.0...v0.10.0) (2026-04-13)


### Features

* model comparison tool, per-call model overrides, nav restructure ([b7176cc](https://gitlab.com/ulfalfa/paperless-aissist/commit/b7176cc1e74da5012d4f8278a2a675004a957c5a))

# [0.9.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.8.0...v0.9.0) (2026-04-11)


### Features

* webhook endpoint, prompt logging UI, chat improvements, remove tag cleanup ([22fc60f](https://gitlab.com/ulfalfa/paperless-aissist/commit/22fc60f5a9937972ec734ff72fa29685548f6080))

# [0.8.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.7.0...v0.8.0) (2026-04-09)


### Features

* show version in sidebar, keep models loaded in memory ([a375deb](https://gitlab.com/ulfalfa/paperless-aissist/commit/a375deb397f8c167ef43993b064810f326effa86))

# [0.7.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.6.0...v0.7.0) (2026-04-09)


### Features

* live chat status, markdown rendering, perf metrics, and unified tools ([695a0f0](https://gitlab.com/ulfalfa/paperless-aissist/commit/695a0f05f6c7ed0ca6d71a1f6fb431616c516ea8))

# [0.6.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.5.1...v0.6.0) (2026-04-09)


### Features

* **chat:** tool calling via /api/chat — LLM can query paperless live data ([0bb1b4f](https://gitlab.com/ulfalfa/paperless-aissist/commit/0bb1b4f8302a692b2feed330c5f14dac20266184))

## [0.5.1](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.5.0...v0.5.1) (2026-04-09)


### Bug Fixes

* **ci:** upgrade Go image to 1.25 (required by golang.org/x/crypto) ([e026a2c](https://gitlab.com/ulfalfa/paperless-aissist/commit/e026a2c221683cf6f9c94341b63c0401404370bb))

# [0.5.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.4.4...v0.5.0) (2026-04-09)


### Features

* add authentication with setup, login, and MCP token guard ([40f7a8c](https://gitlab.com/ulfalfa/paperless-aissist/commit/40f7a8c44848e766e8fa4013c81b83b5d7287e0b))

## [0.4.4](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.4.3...v0.4.4) (2026-04-09)


### Bug Fixes

* unified usage output on stderr for no-args and unknown-flag cases ([5d364c9](https://gitlab.com/ulfalfa/paperless-aissist/commit/5d364c9e84def8bd7dad5af2105a61ce45467845))

## [0.4.3](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.4.2...v0.4.3) (2026-04-09)


### Bug Fixes

* **ci:** use two-rule pattern to skip release job on chore commits ([9ad7714](https://gitlab.com/ulfalfa/paperless-aissist/commit/9ad7714da98554503178ea3e1608ef719ed2f8fc))

## [0.4.2](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.4.1...v0.4.2) (2026-04-09)


### Bug Fixes

* **ci:** remove [skip ci] from release commit so tag pipeline runs build job ([4778d13](https://gitlab.com/ulfalfa/paperless-aissist/commit/4778d13dff36851a0e06d802c04df1218f00274f))
* trigger release build ([15ca2d2](https://gitlab.com/ulfalfa/paperless-aissist/commit/15ca2d2dde6af1be5c5aff6bc95307944600ec9f))

## [0.4.1](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.4.0...v0.4.1) (2026-04-09)


### Bug Fixes

* **install:** track installed version via .version file instead of --version flag ([cd1f2cb](https://gitlab.com/ulfalfa/paperless-aissist/commit/cd1f2cb4c9dfc9af1c3aece8234683854ef20b40))

# [0.4.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.3.0...v0.4.0) (2026-04-09)


### Features

* **install:** skip update if already on latest, restart service on upgrade ([483d41d](https://gitlab.com/ulfalfa/paperless-aissist/commit/483d41dd110c86d6b15f4fef5ca3e21e0776d953))

# [0.3.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.2.0...v0.3.0) (2026-04-09)


### Features

* add --version flag ([302e5fa](https://gitlab.com/ulfalfa/paperless-aissist/commit/302e5fa86c9c90deceabaaf222abcd4cff5bb004))

# [0.2.0](https://gitlab.com/ulfalfa/paperless-aissist/compare/v0.1.1...v0.2.0) (2026-04-09)


### Features

* **ci:** generate and commit CHANGELOG.md on every release ([f044fd1](https://gitlab.com/ulfalfa/paperless-aissist/commit/f044fd14e4c9b7ec075bad647574fb58b6d35968))
