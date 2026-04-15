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
