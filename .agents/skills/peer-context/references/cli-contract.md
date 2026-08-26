# Request-side CLI contract

All commands emit one JSON object. Success goes to stdout with `ok:true`; failures go to stderr with `ok:false`. Match machine fields such as `error.code`, never prose.

## Discovery

```text
peerctx agent list
peerctx agent get AGENT
```

Published Manifests contain a human-written name, summary, tags, capabilities, modes, and online state. They never contain the provider repository path. Select an Agent from those fields; do not ask the CLI to select one.

## Read

```text
peerctx ask AGENT --mode read [--timeout 5m] [--request-id REQUEST_ID]
peerctx request get REQUEST_ID
peerctx request cancel REQUEST_ID
```

The request body is stdin. In v1 JSON, `response.answer` is standard Base64 containing the provider Codex final-message bytes. A replay can return only `metadata` with `replayed:true`, because Relay does not store answers.

## Write

```text
peerctx task AGENT --mode write [--approval-timeout 10m] [--run-timeout 15m]
peerctx task AGENT --mode write --confirm CONFIRMATION_TOKEN
```

The first call exits `10`, writes a `write_confirmation_required` error envelope, and sends no write request. Its `error.details` contains `confirmation` and `confirmation_token`. The second call must receive byte-identical stdin before the confirmation expires.

Provider-side approval commands are intentionally not part of this request-side Skill. A successful write response may include `response.worktree` with a worktree ID and base commit, but never a provider-local path.

## Answer handling

Only this shape is an answer:

```json
{
  "ok": true,
  "data": {
    "response": {
      "status": "succeeded",
      "answer": "Base64 bytes"
    }
  }
}
```

Metadata, approval state, errors, and transport failures are not answers.
