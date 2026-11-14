# Fork workflow (DataCosmos/ai-hub delta)

This repository is the DataCosmos fork of the CPAMC dashboard for the plus backend. The fork keeps
custom code at the absolute minimum and separates upstream mirrors, upstream contributions and
DataCosmos operations.

## Branch ownership

| Branch | Owner | Contents | Integration |
| --- | --- | --- | --- |
| `main` | official mirror | exact `router-for-me` tree; no fork files | `upstream-sync` PR from `router-for-me/main` |
| `plus` | plus mirror | exact `kaitranntt` tree; no fork files | `upstream-sync` PR from `kaitranntt/main` |
| `datacosmos/ai-hub` | DataCosmos ops | this workflow doc, release/sync jobs, contract guard, `.gc/**`, `schemas/**`, the ai-hub AGENTS delta | PR to `dc-use` |
| `contrib/<topic>` | upstream contribution | one upstream-worthy patch and its tests; no DataCosmos files | PR to `router-for-me` or `kaitranntt`; the same commit is merged into `dc-use` only after local validation |
| `dc-use` | integration + release | `main` + `plus` + `datacosmos/ai-hub` + accepted contributions | integration PRs; stable `-dcN` releases |
| `preserve/*` | recovery | abandoned/divergent history | never merged; retirement only after ancestry and content proof |

Rules:

- No force, no reset and no direct push to `main`, `plus` or `dc-use`.
- `main`/`plus` are mirrors: a fork change that belongs upstream goes to a `contrib/*` branch first.
- `datacosmos/ai-hub` never contains panel features. `contrib/*` never contains `.gc/**`,
  `schemas/**`, release workflows, the ai-hub prelude or the model-pipeline adapter.
- `dc-use` stays at the integration tip. Every change lands through a no-ff PR; every merged lane is
  retired only after ancestry and content proof.

## Sync jobs

Both jobs run from `dc-use`, the default branch, and never merge their own PRs.

- `upstream-sync.yml` (daily and dispatch): fetches `router-for-me/main` and `kaitranntt/main`,
  opens `sync/main-<sha>` and `sync/plus-<sha>` PRs to the mirrors when they are behind. A conflict
  opens a `sync conflict` issue and fails loudly; it never force-resolves.
- `dc-integrate.yml` (daily and dispatch): merges the mirrors into `dc-use` on a `sync/dc-use-*`
  branch, records the official/plus tag and SHA in `.cpamc-fork-upstream.env`, and opens a PR.
  Conflicts abort the merge and open an issue.

## Release flow

Only `dc-use` publishes. A release is stable, never a draft or prerelease.

1. `sync-release-tag.yml` runs on panel-affecting `dc-use` pushes (or a dispatch with `dry_run`).
2. It reads `UPSTREAM_TAG` from `.cpamc-fork-upstream.env`, computes the next
   `v${UPSTREAM_TAG}-dc<N>`, and verifies `MANAGEMENT_API_PREFIX` equals the repository variable
   `CLIPROXY_BACKEND_API_PREFIX`. A mismatch holds the tag; the release job remains fail-closed.
3. It dispatches `release.yml`, which rechecks the contract from the trusted default branch, rejects
   rollbacks against the current latest release, builds `management.html`, and publishes the stable
   release with checksums and provenance.
4. Once the deployed backend is on v8, set `CLIPROXY_BACKEND_API_PREFIX=/v8/management`; the next
   panel-affecting merge publishes `v1.28.0-dc1`, `v1.28.0-dc2`, and so on.
5. ai-hub/ccs activation happens after the release asset is verified against the live backend:
   update the host `cliproxyapi` tool, restart `ccs-cliproxy`, prove `/v1/models` and the management
   contract, and remove `poolside-api-key` from the live config.

## Contributions

- One subject per PR, based on the target upstream, with tests and no DataCosmos artifacts.
- Ordered OAuth alias pools: `router-for-me` PR #575.
- Dependency overrides and the window-isolation test fix: clean `contrib/*` branches rebased on the
  target upstream lock.
- Z.AI/BigModel and OpenCode panel parity only after the v8 backend commits the route paths
  (`cl-eu0`); Poolside stays deleted.
- If upstream rejects a patch, keep it on a `contrib/*` branch and carry it in `dc-use` only as an
  explicitly tracked accepted contribution; do not create a second compatibility path.
