# Error and state handling

Never turn these states into business conclusions.

| `error.code` or state | Meaning | Action |
|---|---|---|
| `write_confirmation_required` / exit `10` | Requester confirmation required; nothing sent | Show the exact envelope and ask the user; stop this turn |
| `write_request_denied` / exit `11` | Provider user denied this write | Report denial; do not retry automatically |
| `agent_offline` | Provider is not serving; no offline queue exists | Report unavailable; retry only if the user asks after availability changes |
| `agent_access_denied`, `agent_access_revoked` | ACL does not allow or no longer allows this mode | Ask the Agent owner to change access if appropriate |
| `request_timeout`, `write_approval_expired` | Request or approval window expired | Report timeout; use a new confirmation/request only with user direction |
| `request_canceled` | Requester or infrastructure canceled | Report cancellation |
| `request_replay_mismatch` | Request ID is bound to different bytes or identity | Use a fresh ID; never force replay |
| `response_too_large`, `codex_protocol_error`, `provider_protocol_error` | No valid final answer exists | Report protocol failure, not partial content |
| `recursive_request_blocked` | This is provider-side inbound Codex | Stop; do not attempt another Agent |

`request get` returns audit metadata only. A successful same-ID replay can also return metadata without an answer. In both cases, say that the original answer is not stored rather than inventing or reconstructing it.
