# Changelog

All notable changes to the `conos` CLI will be documented in this file.

## [Unreleased]

### Added
- Interactive install wizard with TTY detection and non-interactive fallback
- Auto-generate SSH config and `.conos/conos.toml` during install
- `--api-key` flag for non-interactive API key provisioning
- Runtime auto-detection (docker/podman) in wizard
- API key is optional at install time (placeholder env file created)

### Changed
- `CONOS_OPENROUTER_API_KEY` renamed to `CONOS_API_KEY` (generic provider key)
- Install success message shows API key setup instructions when key is empty

### Fixed
- Env var naming mismatch between install script and container runtime

## [v0.2.0] - 2026-03-12

### Added
- One-command install with auto SSH key injection and config setup
- `conos install` command with `--runtime`, `--name`, `--image`, `--ssh-port` flags
- Release workflow for linux/darwin x amd64/arm64 binaries
