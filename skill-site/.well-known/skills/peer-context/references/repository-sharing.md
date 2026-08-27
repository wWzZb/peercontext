# Repository sharing

Use this workflow when the user asks to share or register a local repository as a PeerContext Agent.

## Prepare the Manifest

1. Resolve the exact local Git worktree the user intends to share. Do not choose a different repository merely because it is nearby.
2. Read only the repository files needed to describe it, such as its README, package manifest, module file, and concise top-level product documentation. Do not inventory secrets, unrelated files, build output, or user-global configuration.
3. Draft these public fields:
   - `name`: a concise selector. If omitted, PeerContext uses `<member>/<repository-basename>`.
   - `summary`: one sentence describing what repository facts it can answer.
   - `tags`: short discovery categories such as domain, platform, or service type.
   - `capabilities`: concrete questions the repository can answer, such as API contracts, business rules, configuration, or failure diagnosis.
4. Do not invent unsupported capabilities. Leave optional fields empty or mark uncertainty for the user when repository evidence is insufficient.

The local repository path is required by the command but is not part of the published Manifest.

## Confirm sharing

Before registration, show:

- the exact local repository path;
- the proposed public `name`, `summary`, `tags`, and `capabilities`;
- that every current and future Project member can ask an isolated Codex to read this repository;
- that registration is read-only and does not upload the repository, but LAN request and answer content is not encrypted in this MVP.

Obtain explicit confirmation after showing this preview. Do not treat permission to inspect the repository as permission to share it.

## Register and verify

After confirmation, call only the public command:

```text
peerctx agent register REPOSITORY [--name NAME] [--summary TEXT]
                       [--tags CSV] [--capabilities CSV]
```

Parse the JSON envelope, then verify the returned Agent or call `peerctx agent get AGENT`. Report its public name, owner, and online state. Do not expose the local path in output intended for other Project members.

There is no public Agent update command. Changing published metadata currently requires removing and registering the Agent again; explain the interruption and obtain confirmation before removal.
