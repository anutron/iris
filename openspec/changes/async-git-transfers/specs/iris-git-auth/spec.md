## ADDED Requirements

### Requirement: Opt-in unattended GitHub authentication

Iris SHALL accept a local per-source-repo `git_auth_token_source` string naming a secret-source descriptor. When unset, push/fetch SHALL retain their existing command and authentication behavior. When set, push/fetch SHALL resolve it through the existing secrets resolver using that repo's `[secrets.op]` bootstrap, derive `https://github.com/<owner>/<repo>.git` from the effective named remote, and use HTTPS for that command only without rewriting any configured remote. Fetch SHALL use origin; push SHALL respect its effective remote override. Non-GitHub or credential-bearing remote URLs SHALL fail closed in opt-in mode. A malformed or unreadable auth config SHALL fail closed rather than silently selecting SSH.

#### Scenario: Configured token resolves

- **WHEN** a GitHub remote and a resolvable token source are configured
- **THEN** push/fetch uses HTTPS for that same repository, while the stored remote and remote-tracking result behavior stay intact

#### Scenario: Missing token

- **WHEN** the configured source cannot resolve to a nonempty token
- **THEN** no transfer starts and the result carries `[auth_failure]` naming only the source descriptor

#### Scenario: No opt-in

- **WHEN** no token source is configured
- **THEN** the original git transfer command and inherited authentication behavior are unchanged

### Requirement: Credential confinement

The resolved token SHALL appear only in the transfer subprocess environment and its credential-helper response, never in argv, a remote URL, any file, logged output or the returned response. Iris SHALL clear inherited credential helpers for opted-in commands and disable terminal and askpass prompts, preventing fallback to interactive credentials. Captured transfer output SHALL redact the token before classification or response encoding. GitHub-rejected credentials SHALL retain `[auth_failure]` classification. Tests SHALL use fake credentials and fake resolvers without reading real secret material.

#### Scenario: Git echoes a rejected credential

- **WHEN** git fails with an authentication error and echoes the token in its output
- **THEN** the classified error and recorded output redact the token, and argv and configured remotes contain no token

#### Scenario: Interactive authentication disabled

- **WHEN** opt-in HTTPS authentication is configured
- **THEN** git receives GIT_TERMINAL_PROMPT=0, disabled askpass and an environment-reading credential helper without token material in argv
