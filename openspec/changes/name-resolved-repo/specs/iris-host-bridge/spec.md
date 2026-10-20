## ADDED Requirements

### Requirement: MCP responses name the resolved repo

When an MCP verb resolves a source repo (from `task_id`, `path`, or self-discovery), iris SHALL name that repo in the tool response as `repo: {name, path}`. `path` SHALL be the canonical source-repo path. `name` SHALL be the `owner/name` parsed from the source repo's `origin` remote URL, and SHALL be omitted when the URL can't be parsed as a GitHub-style remote. A JSON-object success response SHALL carry `repo` as its first top-level key, with every existing field unchanged. An error response or a non-JSON response SHALL end with a line `repo: <name> (<path>)`, or `repo: <path>` when there is no name. A verb that resolves no repo SHALL return its response unchanged. If a verb resolves more than once, the first resolution SHALL be the one reported.

#### Scenario: Success response names the repo

- **WHEN** an agent calls `iris_gh_pr_merge` with a `task_id` whose source repo has origin `git@github.com:acme/video-gen.git`
- **THEN** the JSON response contains `"repo": {"name": "acme/video-gen", "path": "<canonical source repo path>"}` as its first key alongside the verb's own fields

#### Scenario: Error response names the repo

- **WHEN** a verb resolves its repo and then fails (for example gh refuses the merge)
- **THEN** the error text ends with `repo: acme/video-gen (<canonical source repo path>)`

#### Scenario: Unparseable origin omits the name

- **WHEN** the source repo's origin is a local filesystem path
- **THEN** the response's `repo` object contains `path` and no `name`

#### Scenario: Verb that resolves nothing is unchanged

- **WHEN** an agent calls `iris_ls`, or a verb fails input validation before resolving
- **THEN** the response carries no `repo` field or line
