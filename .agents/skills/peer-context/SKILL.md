---
name: peer-context
description: Help request-side Codex explicitly ask a registered PeerContext Agent for missing private cross-repository facts or request a separately approved remote change. Use only when the user explicitly invokes $peer-context; do not use for ordinary local work, provider serving, or Relay administration.
---

# PeerContext

Use `peerctx` only as the public infrastructure boundary. You decide whether a remote fact is genuinely missing, which published Agent fits, and how to ask; the CLI does not inspect either repository or interpret the request.

## Request flow

1. Check the current repository and conversation first. Do not send a remote request when the needed fact is already local.
2. Run `peerctx agent list`, then `peerctx agent get AGENT` for the best plausible published Manifest. If no Agent clearly fits, ask the user instead of broadcasting.
3. Send only the minimum context the provider needs. State the concrete question, relevant observed behavior, and desired answer shape. Do not ask for an entire repository, broad secrets, or unrelated files.
4. For facts, call `peerctx ask AGENT --mode read` with the request bytes on stdin. Treat only `data.response.answer` from a successful envelope as the remote answer.
5. At most once, send a focused clarification when the answer explicitly identifies missing information. Do not blindly retry timeouts, denial, offline state, or authorization errors.

Read [references/request-patterns.md](references/request-patterns.md) when shaping a cross-repository question. Read [references/cli-contract.md](references/cli-contract.md) before constructing or parsing commands. Read [references/error-handling.md](references/error-handling.md) whenever a command is nonzero or returns metadata without an answer.

## Write requests

Use write mode only when the user asks for a remote repository change.

1. Call `peerctx task AGENT --mode write` with the exact proposed request on stdin and without `--confirm`.
2. Exit code `10` means nothing was sent. Show the user the returned Agent, mode, byte count, SHA-256, and expiry, then stop and ask for explicit confirmation.
3. Never reuse `confirmation_token`, add `--confirm`, or retry automatically in the same turn. Only after a later user message clearly approves that exact envelope may you rerun the exact same stdin with the returned token.
4. Provider approval is separate. Do not call `request approve` or `request deny`; those are provider-side user actions.
5. A successful write response identifies a detached worktree. Never claim that it was committed, merged, pushed, or applied to the requester's repository.

## Boundaries

- Use only documented public `peerctx` commands; do not import Go packages, contact Relay endpoints directly, or access provider-local paths.
- Do not run `peerctx relay serve` or `peerctx agent serve` as part of a request.
- Do not turn infrastructure errors or request metadata into repository facts.
- Do not scan or alter the request to work around policy. The CLI sends stdin bytes as supplied.
- If `recursive_request_blocked` appears, stop. Provider-side inbound Codex cannot start another PeerContext request.
