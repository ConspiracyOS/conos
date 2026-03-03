# conos — ConspiracyOS outer CLI

`conos` is the user-facing CLI for managing ConspiracyOS instances.
It talks to `conctl` inside the container or VM over SSH.

> **Status: work in progress.** The interface below is the target design.

## Commands

```bash
conos start                          # boot the instance
conos stop [--force]                 # stop with confirmation prompt
conos status                         # show agent status

conos config apply                   # apply config from .conos/ to the instance

conos agent list                     # list agents and their state
conos agent kill <name>              # stop a running agent
conos agent logs [-f] [-n N] [name]  # tail audit log (all agents or one)
conos agent task <name> <task>       # send a task to a named agent's inbox
conos agent <task>                   # send a task to concierge (shorthand)
```

Reserved agent names (cannot be used): `list`, `kill`, `logs`, `task`

## Configuration

`conos` looks for instance config in order:

1. `$PWD/.conos/conos.toml`
2. `~/.conos/conos.toml`

```toml
[instance]
host = "conos"   # SSH hostname or alias — see ~/.ssh/config for ProxyJump etc.
```

Complex SSH options (ProxyJump, IdentityFile, port) belong in `~/.ssh/config`,
not here. `conos` just runs `ssh <host> conctl <args>`.

### Container mode

```
Host conos
  HostName 192.168.64.80
  User root
  IdentityFile ~/.ssh/id_ed25519
```

### VM / VPS mode (loopback)

```toml
[instance]
host = "localhost"
```

## How it works

`conos` is a thin SSH wrapper around `conctl`. Every operational command
executes `ssh <host> conctl <equivalent>` on the inner system.
Instance management commands (`start`, `stop`) invoke the container runtime
or VM management API on the local host.

## Build

```bash
go build -o conos ./cmd/conos/
```
