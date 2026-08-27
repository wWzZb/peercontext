# Error and state handling

Never turn infrastructure states into business conclusions.

| `error.code` or state | Meaning | Action |
|---|---|---|
| `unavailable` / Agent offline | Provider device or Agent is offline | Report unavailable; do not retry automatically |
| `lan_connection_failed` | Project host is offline, not on the same LAN, or cannot be rediscovered | Report the connection issue; do not invent an answer |
| `authorization_failed` | Current Project member is not accepted | Ask the user to check membership |
| `request_timeout` | Codex or LAN request exceeded the deadline | Report timeout; retry only on user direction |
| `not_found` | Agent no longer exists | Refresh `agent list` once |
| protocol or signature error | No trustworthy answer exists | Report the failure and stop |
| `recursive_request_blocked` | This is provider-side inbound Codex | Stop; do not attempt another Agent |

PeerContext does not keep an offline queue or persist past answers.
