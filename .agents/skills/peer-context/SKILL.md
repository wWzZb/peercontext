---
name: peer-context
description: Help request-side Codex explicitly discover a LAN PeerContext Agent and ask it for a missing private cross-repository fact. Use only when the user explicitly invokes $peer-context; do not use for ordinary local work, Project setup, Agent registration, background service management, or provider-side inbound Codex.
---

# PeerContext

Use only the public `peerctx` CLI. You decide whether a remote fact is genuinely missing, which published Agent fits, and how to ask. The CLI transports bytes and does not inspect repositories or interpret the request.

## Read flow

1. Check the current repository and conversation first. Do not send a remote request when the needed fact is already local.
2. Run `peerctx agent list`, then `peerctx agent get AGENT` for the best plausible Manifest. If no Agent clearly fits, ask the user instead of broadcasting.
3. Send only the minimum context the provider needs. State the concrete question, relevant observed behavior, and desired answer shape. Do not ask for an entire repository, broad secrets, or unrelated files.
4. Pipe the exact request bytes to `peerctx ask AGENT`. Treat only `data.response.answer` from a successful envelope as the remote answer.
5. At most once, send a focused clarification when the answer explicitly identifies missing information. Do not blindly retry timeout, offline, authorization, host-unavailable, or mDNS errors.

Read [references/request-patterns.md](references/request-patterns.md) when shaping a question. Read [references/cli-contract.md](references/cli-contract.md) before constructing or parsing commands. Read [references/error-handling.md](references/error-handling.md) whenever a command fails.

## Boundaries

- This Skill only discovers Agents and sends read requests. It never creates or joins a Project, registers or removes an Agent, or manages the service.
- There is no write mode. Never call `task`, approval, request, or worktree commands.
- Do not contact LAN HTTP/WebSocket endpoints directly or access provider-local paths.
- Do not turn infrastructure errors into repository facts.
- Do not scan or alter the request to work around policy. The CLI sends stdin bytes as supplied.
- If `recursive_request_blocked` appears, stop. Provider-side inbound Codex cannot start another PeerContext request.
