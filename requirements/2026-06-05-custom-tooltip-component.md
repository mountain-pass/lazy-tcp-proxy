# Custom Tooltip Component (Replace Native title Attributes)

**Date Added**: 2026-06-05
**Priority**: Low
**Status**: In Progress

## Problem Statement

Native `title` attribute tooltips have a browser-imposed delay (~500–1000ms) before they appear.
The status dashboard uses `title` on three emoji spans to explain their meaning — these popups
feel sluggish and unpolished.

## Functional Requirements

- Replace all three `title` attribute usages in `App.svelte` with a custom tooltip component.
- The tooltip must appear with ≤100ms delay on hover.
- Tooltip text must be readable on the dark (`#1C1917`) background.

## User Experience Requirements

- Tooltip positions above the hovered element (with fallback to below if near the top of the viewport).
- Tooltip disappears immediately on mouse-out.

## Technical Requirements

- Implemented as a new Svelte component (`html/src/Tooltip.svelte`).
- Styled to match the existing dark theme (stone palette, same font).
- No new npm dependencies.

## Acceptance Criteria

- [ ] `Tooltip.svelte` component created in `html/src/`.
- [ ] All three `title={...}` attributes in `App.svelte` replaced with `<Tooltip>` wrappers.
- [ ] Tooltip appears within ~50ms of hover (no noticeable delay).
- [ ] Tooltip text is legible on the dark background.
- [ ] No regressions in the rest of the dashboard.

## Dependencies

REQ-103 (Svelte HTML Dashboard)

## Implementation Notes

- The three spans that need wrapping:
  1. `<span title={si.title}>{si.icon}</span>` — status icon
  2. `<span title={snap.has_compose_file ? 'Compose file found' : 'No compose file'} ...>♻️</span>` — compose indicator
  3. `<span title={snap.has_tar_gz ? 'Docker image tar found' : 'No docker image tar'} ...>📦</span>` — tar.gz indicator
