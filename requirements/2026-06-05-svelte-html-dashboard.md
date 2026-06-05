# Svelte + Tailwind HTML Dashboard

**Date Added**: 2026-06-05
**Priority**: Medium
**Status**: Completed

## Problem Statement

The status dashboard at `/` is a large inline HTML/CSS/JS string inside `main.go`. It is difficult to maintain and style effectively. Moving it to a proper Svelte + Tailwind project gives a proper component model, IDE tooling, and hot-reload during development.

## Functional Requirements

- Create `html/` subfolder at the repo root containing a Svelte + Tailwind project.
- Running `npm run build` inside `html/` must produce a single self-contained file: `html/dist/index.html` (all JS and CSS inlined — no external asset files required at runtime).
- `html/dist/index.html` must **not** be committed to git (gitignored).
- `main.go` must embed `html/dist/index.html` via `//go:embed` so the binary is self-contained with no runtime file dependencies.
- The Go build pipeline (CI) must run `npm run build` in `html/` before `go build` so the embedded file is present.
- The existing dashboard behaviour (polling `/metrics`, rendering the proxy table) must be preserved in the new Svelte component.

## User Experience Requirements

- No visible change to the end-user dashboard; this is a developer-experience improvement.
- Local dev workflow: `cd html && npm run dev` for hot-reload, `npm run build` before `go build`.

## Technical Requirements

- Svelte 5 (latest stable).
- Tailwind CSS v4 (via `@tailwindcss/vite` plugin).
- `vite-plugin-singlefile` to inline all assets into `index.html`.
- Vite as the build tool (comes with Svelte template).
- Node.js ≥ 20 required in CI.
- `html/dist/` added to `.gitignore`.
- Go `//go:embed` path: `html/dist/index.html` relative to `main.go` location (`lazy-tcp-proxy/`), so the actual embed path is `../html/dist/index.html` — **Note**: Go embed paths cannot use `..`. The `html/` project must live inside `lazy-tcp-proxy/html/` so the embed directive can reference `html/dist/index.html`.
- The existing inline `statusDashboardHTML` const in `main.go` is replaced with the embed.
- CI workflow (`.github/workflows/go-ci.yml`) updated to install Node.js and run `npm ci && npm run build` in `lazy-tcp-proxy/html/` before the Go build/test steps.

## Acceptance Criteria

- [ ] `lazy-tcp-proxy/html/` exists with a working Svelte + Tailwind project.
- [ ] `npm run build` inside `lazy-tcp-proxy/html/` produces `lazy-tcp-proxy/html/dist/index.html` as a single file (no other files required).
- [ ] `lazy-tcp-proxy/html/dist/` is gitignored.
- [ ] `main.go` uses `//go:embed html/dist/index.html` and serves it at `/`.
- [ ] `go build ./...` succeeds after `npm run build` (file is present).
- [ ] The rendered dashboard is functionally identical to the current one.
- [ ] CI workflow builds the HTML before the Go steps and passes.

## Dependencies

- Supersedes / modifies REQ-056 (Status Dashboard) and REQ-084 (Cards to Table Layout) — same UI, new build pipeline.
- Affects `.github/workflows/go-ci.yml`.

## Implementation Notes

- `vite-plugin-singlefile` replaces all `<script src=...>` and `<link rel=stylesheet>` with inline content, producing a truly portable single HTML file.
- Tailwind v4 uses `@tailwindcss/vite` instead of a PostCSS plugin.
- The Svelte app's `src/App.svelte` will contain the same polling logic and table rendering currently in the inline `<script>` block of `statusDashboardHTML`.
