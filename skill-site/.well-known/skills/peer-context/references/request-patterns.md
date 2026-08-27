# Request patterns

A useful remote request is narrow enough for one provider repository and contains only context needed to answer.

Before asking, check the current repository and conversation. Run `peerctx agent list`, then `peerctx agent get AGENT` for the best plausible public Manifest. If no Agent clearly fits, ask the user instead of broadcasting.

## Fact request

Include:

- the exact contract, rule, configuration, SDK, component, migration, fixture, or failure question;
- the caller-side observation that makes the fact necessary;
- the desired answer shape, such as fields and nullability, enum transitions, supported versions, or a concise root-cause explanation;
- a request to distinguish verified repository facts from uncertainty.

Avoid pasting unrelated local code or asking the provider to inventory its whole repository.

## Focused clarification

Use one clarification only when the first answer names a specific missing input. Supply that input and refer to the original question. If the answer is merely inconclusive, report that to the user instead of starting an open-ended exchange.

Pipe the exact request bytes to `peerctx ask AGENT`. Treat only `data.response.answer` from a successful envelope as the remote answer. Do not blindly retry timeout, offline, authorization, host-unavailable, or mDNS errors.
