# Request patterns

A useful remote request is narrow enough for one provider repository and contains only context needed to answer.

## Fact request

Include:

- the exact contract, rule, configuration, SDK, component, migration, fixture, or failure question;
- the caller-side observation that makes the fact necessary;
- the desired answer shape, such as fields and nullability, enum transitions, supported versions, or a concise root-cause explanation;
- a request to distinguish verified repository facts from uncertainty.

Avoid pasting unrelated local code or asking the provider to inventory its whole repository.

## Focused clarification

Use one clarification only when the first answer names a specific missing input. Supply that input and refer to the original question. If the answer is merely inconclusive, report that to the user instead of starting an open-ended exchange.
