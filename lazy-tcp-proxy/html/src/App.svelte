<script>
  import { onMount, onDestroy } from 'svelte'

  let services = $state([])
  let lastUpdated = $state('Loading…')
  let error = $state('')
  let memoryUsed = $state(0)
  let memoryTotal = $state(0)

  function formatBytes(bytes) {
    if (bytes >= 1073741824) return (bytes / 1073741824).toFixed(1) + ' GB'
    if (bytes >= 1048576) return (bytes / 1048576).toFixed(1) + ' MB'
    if (bytes >= 1024) return (bytes / 1024).toFixed(1) + ' KB'
    return bytes + ' B'
  }

  function statusIcon(snap) {
    if (snap.container_missing) return { icon: '⚠️', title: 'Container missing' }
    if (!snap.running) return { icon: '🔴', title: 'Container stopped' }
    return snap.active_conns > 0
      ? { icon: '🟢', title: 'Container running' }
      : { icon: '🟠', title: 'Container idle' }
  }

  function dnsForPort(snap) {
    const entries = []
    const port = snap.listen_port
    for (const h of (snap.traefik_hosts || [])) {
      const idx = h.lastIndexOf(':')
      if (idx < 1 || parseInt(h.substring(idx + 1)) !== port) continue
      entries.push({ url: 'https://' + h.substring(0, idx), tcp: false })
    }
    for (const h of (snap.traefik_tcp_hosts || [])) {
      const idx = h.lastIndexOf(':')
      if (idx < 1 || parseInt(h.substring(idx + 1)) !== port) continue
      entries.push({ url: 'tcp://' + h.substring(0, idx), tcp: true })
    }
    return entries
  }

  async function refresh() {
    try {
      const url = import.meta.env.PROD ? '/metrics' : 'http://localhost:8080/metrics'
      const res = await fetch(url)
      if (!res.ok) throw new Error('HTTP ' + res.status)
      const data = await res.json()
      services = data.services
      memoryUsed = data.memory_used ?? 0
      memoryTotal = data.memory_total ?? 0
      error = ''
    } catch (e) {
      error = 'Failed to fetch status: ' + e.message
    }
    lastUpdated = 'Last updated: ' + new Date().toLocaleTimeString()
  }

  let interval
  onMount(() => {
    refresh()
    interval = setInterval(refresh, 2000)
  })
  onDestroy(() => clearInterval(interval))
</script>

<div class="min-h-screen bg-[#1C1917] text-[#FAFAF9] p-8 overflow-x-auto" style="font-family: 'Inter', ui-sans-serif, system-ui, -apple-system, sans-serif;">
  <div class="mb-6">
    <h1 class="text-[1.3rem] font-semibold tracking-tight text-[#FAFAF9] mb-1">Lazy TCP Proxy</h1>
    <div class="text-xs text-[#78716C]">{lastUpdated}</div>
    {#if memoryTotal > 0}
    {@const pct = Math.round((memoryUsed / memoryTotal) * 100)}
    <span class="text-xs text-[#78716C]">Memory: {formatBytes(memoryUsed)} / {formatBytes(memoryTotal)}</span>
      <div class="mt-2 flex items-center gap-2">
        <div class="w-64 h-1.5 rounded-full bg-[#3B3837] overflow-hidden">
          <div class="h-full rounded-full bg-[#D97757] transition-all" style="width: {pct}%"></div>
        </div>
      </div>
    {/if}
  </div>
  {#if error}
    <div class="text-[#F87171] mb-4 text-sm bg-[#292524] border border-[#3B3837] rounded-lg px-4 py-3">{error}</div>
  {/if}
  <div class="rounded-xl border border-[#3B3837] overflow-hidden inline-block">
    <table class="border-collapse text-[0.84rem]">
      <thead>
        <tr class="bg-[#292524]">
          {#each ['dns', 'proxy', 'target', 'connections'] as col}
            <th class="px-4 py-2.5 text-[0.68rem] font-semibold uppercase tracking-widest text-[#78716C] border-b border-[#3B3837] {col === 'connections' ? 'text-right' : 'text-left'}">{col}</th>
          {/each}
        </tr>
      </thead>
      <tbody>
        {#if services.length === 0}
          <tr>
            <td colspan="4" class="italic text-[#57534E] font-sans px-4 py-5">No services registered.</td>
          </tr>
        {:else}
          {#each services as snap}
            {@const udp = snap.is_udp ? '/udp' : ''}
            {@const dns = dnsForPort(snap)}
            {@const si = statusIcon(snap)}
            <tr class="border-b border-[#292524] last:border-b-0 hover:bg-[#292524]/50 transition-colors">
              <td class="px-4 py-2.5 align-middle font-mono whitespace-nowrap">
                {#each dns as e}
                  <div>
                    {#if e.tcp}
                      <span class="text-[#A8A29E]">{e.url}</span>
                    {:else}
                      <a href={e.url} target="_blank" class="text-[#D97757] no-underline hover:text-[#E8956B] hover:underline transition-colors">{e.url}</a>
                    {/if}
                  </div>
                {/each}
              </td>
              <td class="px-4 py-2.5 align-middle font-mono whitespace-nowrap text-[#A8A29E]">
                {snap.has_auth ? '🔒' : '🔓'} :{snap.listen_port}{udp} [{snap.availability}]
              </td>
              <td class="px-4 py-2.5 align-middle font-mono whitespace-nowrap text-[#A8A29E]">
                <span title={si.title}>{si.icon}</span>
                <span title={snap.has_compose_file ? 'Compose file found' : 'No compose file'} class:opacity-25={!snap.has_compose_file}>♻️</span>
                <span title={snap.has_tar_gz ? 'Docker image tar found' : 'No docker image tar'} class:opacity-25={!snap.has_tar_gz}>📦</span>
                {snap.container_name}:{snap.target_port}{udp}
              </td>
              <td class="px-4 py-2.5 align-middle font-mono whitespace-nowrap text-[#D97757] text-right">{snap.active_conns}</td>
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  </div>
</div>
