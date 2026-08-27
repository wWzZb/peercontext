# Public CLI contract

All commands emit one JSON object. Success goes to stdout with `ok:true`; failures go to stderr with `ok:false`. Match machine fields such as `error.code`, never prose.

Use only the commands below. Normal commands automatically ensure the macOS user service is running.

## Projects

```text
peerctx project create --name NAME [--member NAME]
peerctx project join INVITATION [--member NAME]
peerctx project list
peerctx project use PROJECT_ID
peerctx project invite create
peerctx project member list
peerctx project member remove MEMBER_ID
```

`project create` returns the Project, owner Member, and a complete single-use invitation. `project join` takes that complete `peerctx2_...` invitation as a positional argument. When `--member` is omitted, the CLI prefers Git `user.name`, then the macOS username. Display names may repeat; Member IDs are authoritative.

Creating another invitation and removing a member require Owner authority. Removing a member also removes that member's Agents from the Project.

## Agents

```text
peerctx agent register REPOSITORY [--name NAME] [--summary TEXT]
                       [--tags CSV] [--capabilities CSV]
peerctx agent list
peerctx agent get AGENT
peerctx agent remove AGENT
```

`REPOSITORY` must be an existing local Git worktree. Registration stores the absolute path only on the provider device and automatically brings the Agent online. Published v2 Manifests contain a name, optional summary, tags, capabilities, owner member, and online state; they never contain the repository path. If `--name` is omitted, the default is `<member>/<repository-basename>`.

Read [repository-sharing.md](repository-sharing.md) before registering. Removing an Agent immediately stops sharing it.

## Read requests

```text
peerctx ask AGENT [--timeout 5m] [--request-id REQUEST_ID]
```

The request body is stdin. There is no `--mode` flag. In JSON, `data.response.answer` is standard Base64 containing the provider Codex final-message bytes.

Only a successful response object is an answer:

```json
{
  "ok": true,
  "data": {
    "schema_version": 2,
    "response": {
      "schema_version": 2,
      "request_id": "req_...",
      "status": "succeeded",
      "answer": "Base64 bytes"
    },
    "replayed": false
  },
  "meta": {
    "request_id": "req_...",
    "version": "0.2.0"
  }
}
```

Errors, connection state and Agent metadata are not answers.

## Service

```text
peerctx service start
peerctx service stop
peerctx service restart
peerctx service status
```

The service is normally automatic. Use `status` for diagnosis, `start` or `restart` when recovery is needed, and `stop` only when the user intends to make hosted Projects and local Agents unavailable.

## Skill and version inspection

```text
peerctx skills list
peerctx skills read peer-context [--file PATH]
peerctx version
```

These commands inspect the version-matched embedded Skill and CLI version. They do not install a Skill.

## Removed interfaces

LAN v2 has no public Relay, credential, `agent serve/access`, `task`, write approval, request-management, worktree, `--relay`, `--credential-file`, `--invite-token`, or `--mode` interface. Do not reconstruct v1 commands.
