<script>
  let { used = 0, limit = 0, barWidth = 'w-20', active = true } = $props()

  function formatBytes(bytes) {
    if (bytes >= 1073741824) return (bytes / 1073741824).toFixed(1) + ' GB'
    if (bytes >= 1048576) return (bytes / 1048576).toFixed(1) + ' MB'
    if (bytes >= 1024) return (bytes / 1024).toFixed(1) + ' KB'
    return bytes + ' B'
  }

  let pct = $derived(limit > 0 ? Math.round((used / limit) * 100) : 0)
</script>

<div class="flex items-center gap-2" class:opacity-25={!active}>
  <div class="{barWidth} h-2 rounded-full bg-[#3B3837] overflow-hidden shrink-0">
    <div class="h-full rounded-full bg-[#D97757] transition-all" style="width: {pct}%"></div>
  </div>
  <span class="text-[0.7rem] text-[#78716C] whitespace-nowrap">{formatBytes(used)} / {formatBytes(limit)} ({pct}%)</span>
</div>
