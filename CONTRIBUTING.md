# Contributing to Velane

Thanks for taking the time to improve Velane.

Bug fixes, documentation improvements, and focused enhancements are welcome. For
larger features or changes to public APIs, open an issue first so we can agree on
the problem and approach before either of us spends much time on an implementation.

## Before you start

Check the issue tracker for an existing report or proposal. If you find one,
leave a comment before starting substantial work; this helps avoid two people
solving the same problem in parallel.

When opening a new issue:

- describe the behavior you expected and what happened instead;
- include a small reproduction when reporting a bug;
- explain the use case, not only the proposed implementation;
- leave security vulnerabilities out of public issues.

Small typo fixes and clearly scoped maintenance changes do not need an issue.

## Development setup

You will need Docker with Compose. Working on an individual service may also
require Go 1.26, Node.js, Bun, or Python, depending on the part of the repository
you are changing.

Fork the repository, clone your fork, and start the development stack:

```bash
git clone https://github.com/<your-user>/velane.git
cd velane
make dev
```

The admin portal is available at `http://localhost:8092`, the API at
`http://localhost:8080`, and the MCP server at `http://localhost:8090`.

Use `make up` when you want to run published images. Use `make dev` when testing
local source changes.

The main areas of the repository are:

- `services/control-plane` — Go API, scheduler, auth, and persistence;
- `services/executor-runtime` — Bun and Python execution services;
- `services/mcp-server` — MCP protocol server;
- `services/cli` — command-line client;
- `apps/admin` — React administration portal;
- `apps/embed-dashboard` — embeddable React viewer;
- `platform-libraries` — built-in Bun and Python libraries.

More detailed development notes live in
[the contributor documentation](./docs/contributing/dev-workflow.md).

## Making changes

Keep pull requests narrow. Unrelated cleanup makes a change harder to review and
harder to revert.

Follow the conventions already used in the area you are editing. A few rules are
especially important:

- every authenticated route must declare its required scope;
- slug-based API handlers must enforce tenant isolation;
- embed tokens must never receive the `admin` scope;
- keep the JWT issuer check in session validation;
- do not expose Nango directly to browsers or the host network;
- use Tailwind for the admin UI; do not add inline styles or debug logging;
- platform libraries export classes and include an `integration` in `meta.json`.

Files under `services/control-plane/internal/license/` and
`apps/admin/src/enterprise/` use the Velane Commercial License. Do not move
community features into those directories, and preserve the existing license
header when making an explicitly requested change there.

If you use code-generation tools, review the result as carefully as code written
by hand. You are responsible for the behavior, tests, licensing, and security of
everything in your pull request.

## Testing

Run the checks that cover the code you changed. Common checks are:

```bash
# Control plane
make copy-platform-libs
(cd services/control-plane && go build ./... && go test ./...)

# Admin portal
(cd apps/admin && npx tsc --noEmit)

# OpenAPI contract, when routes or API models change
make openapi-check
```

Other Go services have separate modules, so run `go test ./...` from the service
directory rather than the repository root. For browser-facing changes, test the
affected flow in a browser and include a screenshot when the visual result is
relevant.

Add or update tests when behavior changes. Documentation-only changes generally
do not need automated tests, but links and commands should still be checked.

## Opening a pull request

Before submitting:

- rebase or merge the latest `main`;
- remove temporary files and debug output;
- confirm the relevant checks pass;
- update documentation for user-visible behavior;
- review the complete diff, not only the last commit.

In the pull request description, explain the problem, summarize the approach,
and list the checks you ran. Link the relevant issue with `Fixes #123` when the
pull request fully resolves it.

Maintainers may ask for changes or a smaller scope. Review is a conversation;
clear reasoning and focused follow-up commits make it move faster.

By contributing, you agree that your contribution is licensed under the license
applicable to the files you change.
