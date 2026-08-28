# Error and state handling

Never turn infrastructure states into business conclusions.

| `error.code` or state | Meaning | Action |
|---|---|---|
| `agent_offline` | The selected Agent or its provider device is offline | Report the Agent issue; do not retry automatically |
| `project_host_offline` | Project Host is offline or unreachable | Ask the user to confirm the creator Mac is awake and on the same LAN |
| `lan_discovery_unavailable` | The saved address failed and mDNS rediscovery is unavailable | Ask the user to check same-LAN multicast availability |
| `invite_expired` | The invitation expired | Ask a Project Owner for a new invitation |
| `invite_consumed` | The invitation was already used | Ask a Project Owner for a new invitation |
| `host_identity_mismatch` / `signature_invalid` | The remote identity or signed message is not trustworthy | Stop and verify the invitation or Project with its Owner |
| `request_replayed` | A signed nonce was reused | Stop; a fresh command creates a fresh nonce |
| `clock_skew` | The two Macs' clocks differ too much | Ask the user to enable automatic date and time |
| `invalid_invitation` | The invitation is incomplete, malformed, or changed | Copy the complete invitation again or request a new one |
| `authorization_failed` | Current Project member is not accepted | Ask the user to check membership |
| `request_timeout` | Codex or LAN request exceeded the deadline | Report timeout; retry only on user direction |
| `not_found` | Agent no longer exists | Refresh `agent list` once |
| protocol or signature error | No trustworthy answer exists | Report the failure and stop |
| `recursive_request_blocked` | This is provider-side inbound Codex | Stop; do not attempt another Agent |

PeerContext does not keep an offline queue or persist past answers.

For local setup or service failures, run `peerctx --json service status` once and report its structured state. Use `peerctx --json service start` or `peerctx --json service restart` only when the user requested recovery or the status shows the background service should be running. Do not automatically call `service stop`, remove a member, or remove an Agent as a troubleshooting shortcut.

Invitation contents and local repository paths are not diagnostic details for third parties. Do not include them in logs, issue text, or unrelated summaries.
