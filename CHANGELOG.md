# Changelog

All notable changes to `github.com/Verexa-dev/verexa-go` are recorded here. The format follows [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/) and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-10-03

First public release.

### Changed

- The default API base URL is now `https://api.verexa.dev`. It was `http://localhost:8080`, so an app that set only `VEREXA_API_KEY` called a server that was not there and failed open.
- An `action` value this SDK does not recognise is now treated as `block`, on the verdict and on each detector, stage and judge outcome. Previously it was passed through, and integrations let the request continue. Unknown response fields and detector ids are still accepted without error. (#8)

### Added

- Every check sends `User-Agent: verexa-go/<version> (go/1.22.5)`, so Verexa can see which SDK versions are in use before deprecating anything. (#8)
- Released under the Apache License 2.0.
