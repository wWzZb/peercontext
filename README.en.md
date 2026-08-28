# PeerContext

[简体中文](./README.md) | English

PeerContext is a LAN collaboration tool for developers. Create a Project and send the complete invitation to a colleague on the same local network. Once they join, they can share a local repository by explicitly registering it as an Agent. There is no public server and no need to configure a Relay URL, port, certificate, or static IP.

PeerContext currently supports Apple Silicon Macs, peers on the same directly connected local network, and read-only queries.

## Start collaborating in five minutes

Both Macs need the complete PeerContext installation (`peerctx` CLI and `peer-context` Skill), the Codex CLI installed and signed in, and a connection to the same local network.

Both people should open a new Codex conversation and explicitly invoke `$peer-context`. The Project creator sends:

```text
$peer-context Create a PeerContext Project named backend-team, then give me the complete invitation and the next step.
```

Send the complete invitation to your colleague. They send:

```text
$peer-context Join this PeerContext invitation: peerctx2_...
```

Your colleague opens the repository they intend to share and asks the Skill to preview its public Manifest and sharing scope before registration:

```text
$peer-context Analyze the current repository and propose an Agent Manifest. Wait for my confirmation before registering it.
```

The Agent comes online automatically after registration, so there is no terminal to keep open. The creator sends:

```text
$peer-context List the Agents in the current Project and ask the best match for the required parameters of the order query API.
```

The first time the background service starts, macOS may show an incoming network connection prompt. This is an expected part of activation.

See the [Quickstart](./docs/user/QUICKSTART.md) for the complete flow. To use the CLI directly, see the [command reference](./docs/user/CLI_REFERENCE.md).

## How it works

```text
Creator's Mac (Project host)              Colleague's Mac

peerctx CLI                               peerctx CLI
    │ Unix socket                             │ Unix socket
    ▼                                         ▼
User-level background service ◄── signed LAN connection ──► User-level background service
    │                                                         │
Project SQLite                                        Local Agent + isolated Codex
```

- The creator's computer hosts the Project. The Project is unavailable while that computer is offline or asleep.
- PeerContext first connects to the current address embedded in the invitation. If the address changes, it rediscovers the host through `_peerctx._tcp` mDNS and uses the host public key to reject forged advertisements.
- Network content is currently sent in plaintext, so a passive observer on the same local network may be able to see questions and answers. Every HTTP request, response, and WebSocket message is signed with Ed25519 to prevent tampering, replay, and credential theft.
- Each member's Project private key is stored only in the macOS Keychain. No reusable Bearer token is sent over the network.
- The host database stores only identity, Agent, and request metadata. It does not store questions, answers, private keys, or repository paths.
- The request body is passed byte-for-byte to Codex stdin in an `isolated_runtime`. The CLI does not read repositories, interpret requests, or rewrite prompts.

## Installation

A complete installation includes both the `peerctx` CLI and the `peer-context` Skill. Both are required. Before you begin, make sure you have:

- An Apple Silicon Mac;
- Go 1.26;
- Git;
- Node.js 18 or later, including `npx`;
- The Codex CLI installed and signed in.

### Install it yourself

Install the CLI first:

```shell
git clone https://github.com/wWzZb/peercontext.git
cd peercontext
go install ./cmd/peerctx
```

If your shell cannot find `peerctx`, add `$(go env GOPATH)/bin` to your PATH.

Then install the Skill:

```shell
npx skills add https://wwzzb.github.io/peercontext/ \
  --skill peer-context \
  --agent codex \
  --global
```

`npx` runs the installer temporarily, so you do not need to install `skills` globally first. Finally, verify the CLI:

```shell
peerctx version
peerctx service status
```

Before you have used PeerContext, it is normal for `service status` to return `running:false`. Open a new Codex conversation and explicitly invoke the Skill:

```text
$peer-context Check my current PeerContext status
```

The Skill lets Codex create or join a Project, generate invitations, analyze and register the current repository, manage members and the background service, and select an Agent for a read. It is enabled only when you explicitly type `$peer-context`.

### Let Codex install it

Alternatively, give the [Codex installation task](./INSTALL.md) directly to Codex and send this prompt:

```text
Read the attached PeerContext installation task in full and follow it in order. Ask for my confirmation before running go install, installing the Skill globally, or changing PATH. Only install and verify the peerctx CLI and peer-context Skill; do not create or join a Project, and do not register a repository. When finished, report only the version, Skill installation status, and any remaining manual steps.
```

`INSTALL.md` is a self-contained task for Codex to execute, not a second set of human-facing installation instructions. For contributing, see the [development guide](./docs/developer/DEVELOPMENT.md). The Skill source is in [`.agents/skills/peer-context`](./.agents/skills/peer-context).

## Current scope

Included: Apple Silicon Macs, peers on the same directly connected local network, Project creation and invitations, members, Project-wide read access, Agents that come online automatically, mDNS recovery, signed plaintext transport, and automatic LaunchAgent management.

Not included: public or cross-subnet connections, a standalone Relay, write operations, approvals, worktrees, host migration, offline queues, Linux, Windows, or Intel Macs.

## Documentation

See the [documentation index](./docs/README.md) for the complete entry point.

For users:

- [Installation instructions](#installation) on this page
- [Your first LAN collaboration](./docs/user/QUICKSTART.md)
- [CLI command reference](./docs/user/CLI_REFERENCE.md)

For developers and product maintainers:

- [Development guide](./docs/developer/DEVELOPMENT.md)
- [Development roadmap](./docs/developer/ROADMAP.md)
- [Validation status](./docs/developer/VALIDATION.md)
- [Product requirements document](./docs/product/PRD.md)
- [Runtime Spike results](./spikes/codex-runtime/RESULT.md)

## License

[MIT](./LICENSE)
