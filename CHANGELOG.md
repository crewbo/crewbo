# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `crewbo version` prints the version and commit; `crewbo help` prints usage.
- `crewbo daemon` runs the per-user daemon in the foreground and stops cleanly on Ctrl+C (and SIGTERM on Linux and macOS).
- `crewbo status` reports whether the daemon is running, with its version, process id, uptime and active claims.
