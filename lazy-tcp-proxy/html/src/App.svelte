<script>
  import { onMount, onDestroy } from 'svelte'

  let services = $state([])
  let lastUpdated = $state('Loading…')
  let error = $state('')

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
      const res = await fetch('/metrics')
      if (!res.ok) throw new Error('HTTP ' + res.status)
      services = (await res.json()).services
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

<div class="min-h-screen bg-[#0f1117] text-[#e2e8f0] p-8 overflow-x-auto">
  <h1 class="text-[1.4rem] font-semibold mb-2 text-[#f8fafc]">Lazy TCP Proxy</h1>
  <div class="text-xs text-[#64748b] mb-6">{lastUpdated}</div>
  {#if error}
    <div class="text-[#f87171] mb-4 text-sm">{error}</div>
  {/if}
  <table class="border-collapse w-full min-w-[600px] text-[0.85rem]">
    <thead>
      <tr>
        {#each ['dns', 'proxy', 'target', 'connections'] as col}
          <th class="text-left px-4 py-1.5 text-[0.7rem] font-semibold uppercase tracking-widest text-[#64748b] border-b border-[#2d3748]">{col}</th>
        {/each}
      </tr>
    </thead>
    <tbody>
      {#if services.length === 0}
        <tr>
          <td colspan="4" class="italic text-[#475569] font-sans px-4 py-4">No services registered.</td>
        </tr>
      {:else}
        {#each services as snap}
          {@const udp = snap.is_udp ? '/udp' : ''}
          {@const dns = dnsForPort(snap)}
          {@const si = statusIcon(snap)}
          <tr class="border-b border-[#1e2330] last:border-b-0">
            <td class="px-4 py-2 align-middle font-mono whitespace-nowrap text-[#a78bfa]">
              {#each dns as e}
                <div>
                  {#if e.tcp}
                    <span class="text-[#22d3ee]">{e.url}</span>
                  {:else}
                    <a href={e.url} target="_blank" class="text-[#a78bfa] no-underline hover:text-[#c4b5fd] hover:underline">{e.url}</a>
                  {/if}
                </div>
              {/each}
            </td>
            <td class="px-4 py-2 align-middle font-mono whitespace-nowrap text-[#94a3b8]">
              {snap.has_auth ? '🔒' : '🔓'} :{snap.listen_port}{udp} [{snap.availability}]
            </td>
            <td class="px-4 py-2 align-middle font-mono whitespace-nowrap text-[#94a3b8]">
              <span title={si.title}>{si.icon}</span>
              <span title={snap.has_compose_file ? 'Compose file found' : 'No compose file'} class:opacity-25={!snap.has_compose_file}>♻️</span>
              <span title={snap.has_tar_gz ? 'Docker image tar found' : 'No docker image tar'} class:opacity-25={!snap.has_tar_gz}>📦</span>
              {snap.container_name}:{snap.target_port}{udp}
            </td>
            <td class="px-4 py-2 align-middle font-mono whitespace-nowrap text-[#60a5fa] text-right">{snap.active_conns}</td>
          </tr>
        {/each}
      {/if}
    </tbody>
  </table>
</div>
