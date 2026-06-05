# Dashboard Tab Navigation: Status & Metrics Heatmap — Implementation Plan

**Requirement**: [2026-06-05-dashboard-tabs-status-metrics.md](2026-06-05-dashboard-tabs-status-metrics.md)
**Date**: 2026-06-05
**Status**: Approved

## Implementation Steps

1. **Add state variables** to the `<script>` block in `App.svelte`:
   - `activeTab = $state('status')` — which tab is selected
   - `metricsData = $state([])` — array from `/metrics`
   - `metricsError = $state('')` — error string for the metrics fetch
   - `metricsFetched = $state(false)` — guard so we only fetch once

2. **Add constants** to the `<script>` block:
   - `const DAYS = ['mon','tue','wed','thu','fri','sat','sun']`
   - `const DAY_LABELS = ['Mon','Tue','Wed','Thu','Fri','Sat','Sun']`

3. **Add `fetchMetrics()` function** to `<script>`:
   - Fetches `import.meta.env.PROD ? '/metrics' : 'http://localhost:8080/metrics'`
   - On success: stores result in `metricsData`, sets `metricsFetched = true`, clears `metricsError`
   - On failure: sets `metricsError`

4. **Add `switchTab(tab)` function** to `<script>`:
   - Sets `activeTab = tab`
   - If `tab === 'metrics'` and `!metricsFetched`, calls `fetchMetrics()`

5. **Add tab bar** in the template, between the `<h1>` title block and the error `{#if error}` block:
   ```html
   <div class="flex gap-1 mt-4 mb-2 border-b border-[#3B3837]">
     {#each [['status','Status'],['metrics','Metrics']] as [id, label]}
       <button onclick={() => switchTab(id)}
         class="px-4 py-1.5 text-sm font-medium transition-colors
           {activeTab === id
             ? 'text-[#FAFAF9] border-b-2 border-[#D97757] -mb-px'
             : 'text-[#78716C] hover:text-[#A8A29E]'}">
         {label}
       </button>
     {/each}
   </div>
   ```

6. **Wrap existing status content** in `{#if activeTab === 'status'}…{/if}` — this covers the
   table's `<div class="rounded-xl ...">` wrapper only; the error div and memory bar remain
   outside and always visible.

   Actually: move the error div + memory bar inside the status block too, so the metrics tab has
   its own separate error display.

   **Revised structure**:
   ```
   <header> (h1 + lastUpdated)
   <tab bar>
   {#if activeTab === 'status'}
     {#if error} ... {/if}
     memory bar
     table
   {:else}
     {#if metricsError} ... {/if}
     heatmap blocks
   {/if}
   ```

7. **Render heatmap** in the `{:else}` branch:
   - If `metricsData.length === 0` and `metricsFetched`: show "No metrics data."
   - For each `svc` in `metricsData`:
     - Container: `<div class="mb-6 {svc.active ? '' : 'opacity-40'}">`
     - Label: `<div class="text-xs font-mono text-[#A8A29E] mb-2">svc.container_name:svc.port{svc.is_udp ? '/udp' : ''}</div>`
     - Grid: a `<div class="flex flex-col gap-[3px]">` with one row per day:
       ```
       {#each DAYS as day, di}
         <div class="flex items-center gap-[3px]">
           <span class="w-8 text-[0.65rem] text-[#57534E] text-right pr-1">{DAY_LABELS[di]}</span>
           {#each {length: 24} as _, h}
             <div class="w-3.5 h-3.5 rounded-sm {svc[day][h] ? 'bg-[#D97757]' : 'bg-[#292524]'}"
                  title="{DAY_LABELS[di]} {h}:00"></div>
           {/each}
         </div>
       {/each}
       ```
     - Hour axis: a flex row of 24 cells with labels at 0, 6, 12, 18, 23:
       ```
       <div class="flex items-center gap-[3px] mt-1">
         <span class="w-8"></span>
         {#each {length: 24} as _, h}
           <div class="w-3.5 text-center text-[0.6rem] text-[#57534E]">
             {[0,6,12,18,23].includes(h) ? h : ''}
           </div>
         {/each}
       </div>
       ```

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `lazy-tcp-proxy/html/src/App.svelte` | Modify | Add tab bar, metrics fetch, heatmap render |

## Risks & Open Questions

- The `{#each {length: 24} as _, h}` Svelte 5 syntax iterates over an array-like object. If this
  doesn't work in the project's Svelte version, use `Array.from({length:24}, (_,i) => i)` as a
  helper constant instead.
- Hour labels positioned via per-cell width must match cell width exactly (`w-3.5 = 14px`).
