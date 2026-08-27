# Request-side CLI contract

All commands emit one JSON object. Success goes to stdout with `ok:true`; failures go to stderr with `ok:false`. Match machine fields such as `error.code`, never prose.

## Discovery

```text
peerctx agent list
peerctx agent get AGENT
```

Published v2 Manifests contain a name, optional summary, tags, capabilities, owner member and online state. They never contain the provider repository path. Select an Agent from these fields; do not ask the CLI to select one.

## Read

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
