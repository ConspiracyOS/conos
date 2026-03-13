# conos — ConspiracyOS outer CLI

`conos` is the user-facing CLI for managing ConspiracyOS instances.
It talks to `conctl` inside the container or VM over SSH.

## Commands

```bash
conos install                        # pull image + create env file + start local container
conos start                          # boot the instance
conos stop [--force]                 # stop the instance (--force skips confirmation)
conos status                         # show agent status

conos config apply                   # push .conos/conos.toml to the instance

conos agent list                     # list agents and their state
conos agent kill <name>              # stop a running agent
conos agent logs [-f] [-n N] [name]  # tail audit log (all agents or one)
conos agent task <name> <task>       # send a task to a named agent's inbox
conos agent <task>                   # send a task to concierge (shorthand)
```

Reserved agent names (cannot be used as `conos agent <task>`): `list`, `kill`, `logs`, `task`

## Zero-to-one local install

The fastest path is a single command with one required env var:

```bash
CONOS_API_KEY=sk-or-your-key conos install
```

What `conos install` does:

- Pulls `ghcr.io/conspiracyos/conos:latest`
- Creates `~/.conos/container.env` if missing
- Starts container `conos` with required systemd runtime flags (Docker/Podman)

## Configuration

`conos` looks for config in order:

1. `.conos/conos.toml` (project-local)
2. `~/.conos/conos.toml` (user-global)

Minimal config:

```toml
[instance]
host = "conos"   # SSH hostname or alias from ~/.ssh/config
```

Full config with container management:

```toml
[instance]
host = "conos"         # SSH host alias

[container]
runtime = "docker"     # docker | podman | container (default: docker)
name    = "conos"      # container name (default: conos)
image   = "ghcr.io/conspiracyos/conos:latest"  # image to start
env_file = "container.env"  # optional: env file passed to runtime on start
```

Complex SSH options (ProxyJump, IdentityFile, port) belong in `~/.ssh/config`,
not here. `conos` just runs `ssh <host> conctl <args>`.

### Apple Container / local container

```
# ~/.ssh/config
Host conos
  HostName 192.168.64.80   # container IP (find with: container list)
  User root
  IdentityFile ~/.ssh/id_ed25519
```

### VPS / remote server

```toml
# .conos/conos.toml
[instance]
host = "my-vps"   # alias defined in ~/.ssh/config
```

## How it works

`conos` is a thin SSH wrapper around `conctl`. Every operational command
executes `ssh <host> conctl <equivalent>` on the inner system.
`start` and `stop` invoke the local container runtime directly.

## Build

```bash
go build -o conos ./cmd/conos/
```
