---
id: 3
slug: package-distribution
title: Package distribution — release binaries (brew / npm deferred)
status: in_progress
priority: medium
type: chore
created: 2026-08-23
updated: 2026-08-25
depends_on: []
related: [2]
discovered_from: []
adrs: []
spec:
plan:
results:
trivial: true
auto_groomable:
branch: chore/0003-package-distribution
pr: 5
blocked_by:
reconciled: true
---

## Artifacts

<!-- docket:artifacts:start (generated — do not hand-edit) -->
<!-- docket:artifacts:end -->

## Why

The distribution half of the review's install/onboarding recommendation: move from
"git clone + go build" to a package-manager install so the tool is trivially
obtainable. Pairs with the `workbench init` onboarding flow (#2).

## What changes

Scoped to **GitHub Releases** — the foundation brew/npm both consume:

- Versioned GitHub releases publishing prebuilt binaries (darwin/linux ×
  amd64/arm64) on every `v*` tag, via a hand-rolled matrix workflow using the
  default `GITHUB_TOKEN` (no external accounts).
- `workbench --version`, stamped from the release tag via ldflags, so the
  binaries self-identify.
- A build/vet/test CI workflow so the release path isn't the first CI run.

## Out of scope

- Homebrew tap/formula — **deferred**; needs a `homebrew-<tap>` repo.
- npm global wrapper — **deferred**; needs an `NPM_TOKEN`.
- The `init` onboarding flow itself (#2).
- Auto-update mechanics.

## Open questions

_(resolved)_ Scope narrowed to Releases; brew vs npm priority is deferred to a
follow-up once accounts/tap exist.

## Reconcile log

- 2026-08-25 — Reconciled at implement time. Original "What changes" spanned
  brew + npm + Releases, but brew/npm each need external accounts an autonomous
  build can't provision (npm publish token; a Homebrew tap repo). Narrowed to
  GitHub Releases (the artifact both later consume), which reaches a mergeable
  PR with only the default `GITHUB_TOKEN`. Brew/npm carried as deferred
  out-of-scope for a follow-up change. Implemented on `chore/0003-package-distribution`, PR #5.
