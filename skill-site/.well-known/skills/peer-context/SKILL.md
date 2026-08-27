---
name: peer-context
description: Help Codex explicitly operate the complete public PeerContext LAN CLI in the user's normal workspace, including Project setup, repository sharing, service management, Agent discovery, and read requests. Use only when the user explicitly invokes $peer-context; do not use for ordinary local work or provider-side inbound Codex.
---

# PeerContext

Use only the public `peerctx` CLI. In the user's normal interactive workspace, translate their intent into the documented command, inspect local repository information when needed, parse the JSON envelope, and explain the result. The CLI remains an infrastructure layer and does not inspect repositories or infer semantics.

## Route the task

- For any command, read [references/cli-contract.md](references/cli-contract.md) before constructing or parsing it.
- For Project creation, joining, selection, invitations, or membership, use the Project commands in that contract.
- For sharing a local repository, read [references/repository-sharing.md](references/repository-sharing.md) and follow its preview and confirmation flow.
- For Agent discovery or a remote fact request, read [references/request-patterns.md](references/request-patterns.md).
- For service operations or any failed command, read [references/error-handling.md](references/error-handling.md).

## Action policy

- Run read-only inspection commands such as `project list`, `project member list`, `agent list|get`, `service status`, `skills list|read`, and `version` when they help complete the request.
- Create or join a Project, create an invitation, switch Project, or start/restart the service when the user has clearly requested that outcome. Treat a complete invitation as a sensitive one-time credential and do not place it in logs or unrelated output.
- Before `agent register`, preview the exact local path, proposed public Manifest, Project-wide read access, and LAN content-confidentiality limitation. Obtain explicit confirmation after the preview.
- Before `agent remove`, `project member remove`, or `service stop`, confirm the exact target and user-visible impact unless the current user message already explicitly authorizes that exact action and target.
- Send `ask` only when the user explicitly wants a cross-repository fact. Check the current repository and conversation first, select one fitting Agent, and send the minimum necessary context.

## Boundaries

- This Skill is for the user's normal interactive Codex workspace. Provider-side inbound Codex runs in an isolated environment that does not load this Skill.
- There is no write mode. Never call Relay, credential, `task`, approval, request, worktree, `agent serve`, or `agent access` commands.
- Use public CLI commands only; do not import PeerContext Go packages or contact LAN HTTP/WebSocket endpoints directly.
- Do not treat permission to inspect a repository as permission to share it.
- Do not access provider-local paths from the requesting device.
- Do not turn infrastructure errors into repository facts.
- Do not scan or alter the request to work around policy. The CLI sends stdin bytes as supplied.
- If `recursive_request_blocked` appears, stop. Provider-side inbound Codex cannot start another PeerContext request.
