<script>
  import { onMount, onDestroy } from 'svelte'
  import MemoryBar from './lib/MemoryBar.svelte'

  let services = $state([])
  let lastUpdated = $state('Loading…')
  let error = $state('')
  let memoryUsed = $state(0)
  let memoryTotal = $state(0)
  let containers = $state([])

  let serviceRows = $derived.by(() => {
    const seen = new Set()
    const byName = new Map(containers.map(c => [c.name, c]))
    return services.map(snap => {
      const name = snap.container_name
      const mem = byName.get(name)
      const showMemory = !!mem && !seen.has(name)
      if (mem) seen.add(name)
      return { snap, mem, showMemory }
    })
  })

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
      const url = import.meta.env.PROD ? '/status' : 'https://dash.cname1.mountainpass.com.au/status'
      const res = await fetch(url)
      if (!res.ok) throw new Error('HTTP ' + res.status)
      const data = await res.json()
      services = data.services
      memoryUsed = data.memory_used ?? 0
      memoryTotal = data.memory_total ?? 0
      containers = data.containers ?? []
      error = ''
    } catch (e) {
      error = 'Failed to fetch status: ' + e.message
    }
    lastUpdated = 'Last updated: ' + new Date().toLocaleTimeString()
  }

  let activeTab = $state('status')
  let metricsData = $state([])
  let metricsError = $state('')
  let metricsFetched = $state(false)

  const DAYS = ['mon','tue','wed','thu','fri','sat','sun']
  const DAY_LABELS = ['Mon','Tue','Wed','Thu','Fri','Sat','Sun']
  const HOURS = Array.from({length: 24}, (_, i) => i)
  const HOUR_TICKS = new Set([0, 6, 12, 18, 23])

  // Shift UTC 7×24 activity matrix into local browser time.
  // getTimezoneOffset() returns minutes-west (negative for east), so negate it.
  const LOCAL_OFFSET_HOURS = Math.round(new Date().getTimezoneOffset() / 60)

  function shiftToLocalTime(svc) {
    if (LOCAL_OFFSET_HOURS === 0) return svc
    // Flatten Mon–Sun into a 168-element array (index 0 = Mon 00:00 UTC)
    const flat = DAYS.flatMap(d => Array.from(svc[d]))
    const len = flat.length
    // Rotate: positive offset shifts data left (UTC+N means local hours are ahead)
    const shifted = flat.map((_, i) => flat[(i + LOCAL_OFFSET_HOURS + len) % len])
    // Re-fold into day arrays
    const result = {}
    DAYS.forEach((d, di) => { result[d] = shifted.slice(di * 24, di * 24 + 24) })
    return { ...svc, ...result }
  }

  async function fetchMetrics() {
    try {
      const url = import.meta.env.PROD ? '/metrics' : 'https://dash.cname1.mountainpass.com.au/metrics'
      const res = await fetch(url)
      if (!res.ok) throw new Error('HTTP ' + res.status)
      metricsData = await res.json()
      metricsFetched = true
      metricsError = ''
    } catch (e) {
      metricsError = 'Failed to fetch metrics: ' + e.message
    }
  }

  function switchTab(tab) {
    activeTab = tab
    if (tab === 'metrics' && !metricsFetched) fetchMetrics()
  }

  let interval
  onMount(() => {
    refresh()
    interval = setInterval(refresh, 3_000)
  })
  onDestroy(() => clearInterval(interval))
</script>

<div class="min-h-screen bg-[#1C1917] text-[#FAFAF9] p-8 overflow-x-auto" style="font-family: 'Inter', ui-sans-serif, system-ui, -apple-system, sans-serif;">
  <div class="mb-4">
    <h1 class="text-[1.3rem] font-semibold tracking-tight text-[#FAFAF9] mb-1">Lazy TCP Proxy</h1>
    <div class="text-xs text-[#78716C]">{lastUpdated}</div>
  </div>

  <div class="flex gap-1 mb-6 border-b border-[#3B3837]">
    {#each [['status','Status'],['metrics','Metrics']] as [id, label]}
      <button onclick={() => switchTab(id)}
        class="px-4 py-1.5 text-sm font-medium cursor-pointer transition-colors {activeTab === id ? 'text-[#FAFAF9] border-b-2 border-[#D97757] -mb-px' : 'text-[#78716C] hover:text-[#A8A29E]'}">
        {label}
      </button>
    {/each}
  </div>

  {#if activeTab === 'status'}
    {#if memoryTotal > 0}
      <div class="mb-4">
        <span class="text-xs text-[#78716C]">Memory:</span>
        <div class="mt-2">
          <MemoryBar used={memoryUsed} limit={memoryTotal} barWidth="w-64" />
        </div>
      </div>
    {/if}
    {#if error}
      <div class="text-[#F87171] mb-4 text-sm bg-[#292524] border border-[#3B3837] rounded-lg px-4 py-3">{error}</div>
    {/if}
    <div class="rounded-xl border border-[#3B3837] overflow-hidden inline-block">
      <table class="border-collapse text-[0.84rem]">
        <thead>
          <tr class="bg-[#292524]">
            {#each ['dns', 'proxy', 'target', 'memory', 'connections'] as col}
              <th class="px-4 py-2.5 text-[0.68rem] font-semibold uppercase tracking-widest text-[#78716C] border-b border-[#3B3837] {col === 'connections' ? 'text-right' : 'text-left'}">{col}</th>
            {/each}
          </tr>
        </thead>
        <tbody>
          {#if services.length === 0}
            <tr>
              <td colspan="5" class="italic text-[#57534E] font-sans px-4 py-5">No services registered.</td>
            </tr>
          {:else}
            {#each serviceRows as { snap, mem, showMemory }}
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
                <td class="px-4 py-2.5 align-middle font-mono whitespace-nowrap text-[#A8A29E]"  class:opacity-25={!snap.running}>
                  {snap.has_auth ? '🔒' : '🔓'} :{snap.listen_port}{udp} [{snap.availability}]
                </td>
                <td class="px-4 py-2.5 align-middle font-mono whitespace-nowrap text-[#A8A29E]">
                  <span title={si.title}>{si.icon}</span>
                  <span title={snap.has_compose_file ? 'Compose file found' : 'No compose file'} class:opacity-25={!snap.has_compose_file}>♻️</span>
                  <span title={snap.has_tar_gz ? 'Docker image tar found' : 'No docker image tar'} class:opacity-25={!snap.has_tar_gz}>📦</span>
                  <span class:opacity-25={!snap.running}>{snap.container_name}:{snap.target_port}{udp}</span>
                </td>
                <td class="px-4 py-2.5 align-middle font-mono whitespace-nowrap">
                  {#if showMemory}
                    <MemoryBar used={mem.memory_used} limit={mem.memory_limit} />
                  {/if}
                </td>
                <td class="px-4 py-2.5 align-middle font-mono whitespace-nowrap text-[#D97757] text-right" class:opacity-25={!snap.running}>{snap.active_conns}</td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>
  {:else}
    {#if metricsError}
      <div class="text-[#F87171] mb-4 text-sm bg-[#292524] border border-[#3B3837] rounded-lg px-4 py-3">{metricsError}</div>
    {/if}
    {#if metricsFetched && metricsData.length === 0}
      <p class="italic text-[#57534E] text-sm">No metrics data.</p>
    {/if}
    <div class="mb-4">

      <div class="mb-4">
        <span class="text-xs text-[#78716C]">Times shown in local time ({Intl.DateTimeFormat().resolvedOptions().timeZone})</span>
      </div>
      
    </div>
    <div class="flex flex-col gap-8">
      {#each metricsData as svc}
        {@const udp = svc.is_udp ? '/udp' : ''}
        {@const local = shiftToLocalTime(svc)}
        <div class="{svc.active ? '' : 'opacity-40'}">
          <div class="text-sm font-mono text-[#A8A29E] mb-2">{svc.container_name}:{svc.port}{udp}</div>
          <div class="flex flex-col gap-[3px]">
            {#each DAYS as day, di}
              <div class="flex items-center gap-[3px]">
                <span class="w-8 text-[0.65rem] text-[#57534E] text-right pr-1">{DAY_LABELS[di]}</span>
                {#each HOURS as h}
                  <div class="w-3.5 h-3.5 rounded-sm {local[day][h] ? 'bg-[#D97757]' : 'bg-[#292524]'}" title="{DAY_LABELS[di]} {h}:00"></div>
                {/each}
              </div>
            {/each}
            <div class="flex items-center gap-[3px] mt-1">
              <span class="w-8"></span>
              {#each HOURS as h}
                <div class="w-3.5 text-center text-[0.6rem] text-[#57534E]">{HOUR_TICKS.has(h) ? h : ''}</div>
              {/each}
            </div>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>
