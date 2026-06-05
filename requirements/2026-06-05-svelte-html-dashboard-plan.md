# Svelte + Tailwind HTML Dashboard — Implementation Plan

**Requirement**: [2026-06-05-svelte-html-dashboard.md](2026-06-05-svelte-html-dashboard.md)
**Date**: 2026-06-05
**Status**: Implemented

## Implementation Steps

1. Update requirement status to "In Progress" in the design file and index.
2. Add `lazy-tcp-proxy/html/dist/` to `.gitignore`.
3. Create `lazy-tcp-proxy/html/package.json` — Svelte 5, Vite, Tailwind v4, vite-plugin-singlefile.
4. Create `lazy-tcp-proxy/html/vite.config.js` — configure `@tailwindcss/vite` and `vite-plugin-singlefile`, set `build.target: 'esnext'`.
5. Create `lazy-tcp-proxy/html/index.html` — Vite entry point (loads `src/main.js`).
6. Create `lazy-tcp-proxy/html/src/main.js` — mounts the Svelte app.
7. Create `lazy-tcp-proxy/html/src/App.svelte` — ports existing dashboard HTML/CSS/JS into a Svelte component with Tailwind classes.
8. Modify `lazy-tcp-proxy/main.go` — add `embed` import, replace `const statusDashboardHTML` with `//go:embed html/dist/index.html` + `var statusDashboardHTML string`.
9. Update `.github/workflows/go-ci.yml` — add Node.js setup and `npm ci && npm run build` steps (in `lazy-tcp-proxy/html/`) before Go build/test in both `lint` and `test` jobs; add `lazy-tcp-proxy/html/**` to path triggers.
10. Run `npm ci && npm run build` locally in `lazy-tcp-proxy/html/` to verify the build works.
11. Run `go vet ./...` and `go build ./...` in `lazy-tcp-proxy/` to verify embed compiles.
12. Run `go test -race -count=1 ./...` to confirm no regressions.
13. Update requirement status to "Completed" in design file and index.
14. Commit and push all changes.

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `.gitignore` | Modify | Add `lazy-tcp-proxy/html/dist/` |
| `lazy-tcp-proxy/html/package.json` | Create | Svelte 5, Vite, Tailwind v4, vite-plugin-singlefile deps |
| `lazy-tcp-proxy/html/vite.config.js` | Create | Vite config with singlefile + tailwind plugins |
| `lazy-tcp-proxy/html/index.html` | Create | Vite HTML entry point |
| `lazy-tcp-proxy/html/src/main.js` | Create | Svelte app mount |
| `lazy-tcp-proxy/html/src/App.svelte` | Create | Dashboard component (ported from inline HTML) |
| `lazy-tcp-proxy/main.go` | Modify | Replace const with `//go:embed html/dist/index.html` |
| `.github/workflows/go-ci.yml` | Modify | Add Node.js + npm build steps; widen path trigger |
| `requirements/2026-06-05-svelte-html-dashboard.md` | Modify | Status → Completed |
| `requirements/_index.md` | Modify | Status → Completed |

## Key Code Snippets

### vite.config.js
```js
import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import tailwindcss from '@tailwindcss/vite'
import { viteSingleFile } from 'vite-plugin-singlefile'

export default defineConfig({
  plugins: [tailwindcss(), svelte(), viteSingleFile()],
  build: { target: 'esnext' },
})
```

### main.go embed
```go
import _ "embed"

//go:embed html/dist/index.html
var statusDashboardHTML string
```

### CI step (before go build)
```yaml
- uses: actions/setup-node@v4
  with:
    node-version: '20'
    cache: 'npm'
    cache-dependency-path: lazy-tcp-proxy/html/package-lock.json
- name: Build HTML dashboard
  working-directory: lazy-tcp-proxy/html
  run: npm ci && npm run build
```

## Risks & Open Questions

- `vite-plugin-singlefile` must be confirmed to inline all assets. Using `removeViteModuleLoader: true` option ensures no Vite module loader script tag remains.
- Tailwind v4 uses `@import "tailwindcss"` in CSS rather than `@tailwind` directives.
