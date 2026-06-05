# Custom Tooltip Component — Implementation Plan

**Requirement**: [2026-06-05-custom-tooltip-component.md](2026-06-05-custom-tooltip-component.md)
**Date**: 2026-06-05
**Status**: Approved

## Implementation Steps

1. **Create `html/src/Tooltip.svelte`**
   - Accept a `text` prop (string).
   - Wrap the slot content in a `<span>` with `position: relative`.
   - On `mouseenter`, show a small absolutely-positioned label above the element; hide it on `mouseleave`.
   - Use a `$state` boolean (`visible`) toggled by the mouse events — no delay.
   - Style: dark stone background (`#292524`), light text (`#FAFAF9`), small border (`#3B3837`), `text-xs`, rounded, `z-50`, `whitespace-nowrap`, `pointer-events-none`.
   - Position: `bottom: 100%; left: 50%; transform: translateX(-50%)` with a small `mb-1` gap.

2. **Update `html/src/App.svelte`**
   - Import `Tooltip` at the top of the `<script>` block.
   - Replace `<span title={si.title}>{si.icon}</span>` with `<Tooltip text={si.title}>{si.icon}</Tooltip>`.
   - Replace the ♻️ span (with `title`) with `<Tooltip text={...}>♻️</Tooltip>`, preserving the `class:opacity-25` binding on the outer wrapper.
   - Replace the 📦 span (with `title`) with `<Tooltip text={...}>📦</Tooltip>`, same pattern.

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/html/src/Tooltip.svelte` | Create | Reusable tooltip component |
| `lazy-tcp-proxy/html/src/App.svelte` | Modify | Replace three `title` attributes with `<Tooltip>` |

## API Contracts

`Tooltip.svelte` props:
- `text: string` — tooltip label text

Slot: default slot renders the trigger element inline.

## Key Code Snippets

```svelte
<!-- Tooltip.svelte -->
<script>
  let { text, children } = $props()
  let visible = $state(false)
</script>

<span class="relative inline-block"
  onmouseenter={() => visible = true}
  onmouseleave={() => visible = false}>
  {@render children()}
  {#if visible}
    <span class="absolute bottom-full left-1/2 -translate-x-1/2 mb-1 px-2 py-1 rounded text-xs whitespace-nowrap pointer-events-none z-50 bg-[#292524] border border-[#3B3837] text-[#FAFAF9]">
      {text}
    </span>
  {/if}
</span>
```

```svelte
<!-- App.svelte — usage -->
<Tooltip text={si.title}>{si.icon}</Tooltip>
<Tooltip text={snap.has_compose_file ? 'Compose file found' : 'No compose file'}><span class:opacity-25={!snap.has_compose_file}>♻️</span></Tooltip>
<Tooltip text={snap.has_tar_gz ? 'Docker image tar found' : 'No docker image tar'}><span class:opacity-25={!snap.has_tar_gz}>📦</span></Tooltip>
```

## Risks & Open Questions

- None. The Svelte 5 runes (`$props`, `$state`, `{@render children()}`) are consistent with the existing App.svelte syntax.
