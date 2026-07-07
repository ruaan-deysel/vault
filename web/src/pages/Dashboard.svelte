<script>
  import { onMount } from 'svelte'
  import { navigate } from '../lib/router.svelte.js'
  import { SvelteSet } from 'svelte/reactivity'
  import { api, isReplicaMode } from '../lib/api.js'
  import { onWsMessage } from '../lib/ws.svelte.js'
  import { relTime, relTimeUntil, formatSpeed, formatBytes } from '../lib/utils.js'
  import { getProgress, handleProgressMessage, restoreFromStatus, syncFromStatus } from '../lib/progress.svelte.js'
  import Skeleton from '../components/Skeleton.svelte'
  import Toast from '../components/Toast.svelte'
  import Welcome from '../components/Welcome.svelte'
  import ComplianceBadge from '../components/ComplianceBadge.svelte'
  import ActivityTimeline from '../components/ActivityTimeline.svelte'
  import PullToRefresh from '../components/PullToRefresh.svelte'
  import { getAnomalyEnabled } from '../lib/settings.svelte.js'
  import { getAnomalies, setOpenList } from '../lib/anomalies.svelte.js'
  import { createDashboardLayout } from '../lib/dashboardLayout.svelte.js'

  let loading = $state(true)
  let error = $state('')
  let health = $state(null)
  let jobs = $state([])
  let storage = $state([])
  let recentRuns = $state([])
  let containers = $state([])
  let vms = $state([])
  let folders = $state([])
  let protectedItems = $state(new SvelteSet())
  // Items configured in a job but not yet captured in any restore point
  // (awaiting their first backup). Keyed "type:name", from the health summary.
  let pendingItems = $state(new SvelteSet())
  let runningJob = $state(null)
  let runningAll = $state(false)
  let toast = $state({ message: '', type: 'info', key: 0 })
  let nextRuns = $state({})
  let healthSummary = $state(null)
  let replicationSources = $state([])
  /** @type {Record<string, string>} */
  let settings = $state({})
  let liveSpeed = $state(null)
  let liveCumulativeBytes = $state(0)
  let liveStartTime = $state(null)
  // Shared progress state (persists across page navigations)
  const progress = getProgress()
  const anomalies = getAnomalies()

  function showToast(message, type = 'info') {
    toast = { message, type, key: toast.key + 1 }
  }

  function fmtDur(s) {
    if (s == null || isNaN(s)) return ''
    if (s < 60) return `${Math.round(s)}s`
    const m = Math.floor(s / 60), ss = Math.round(s % 60)
    if (m < 60) return ss ? `${m}m ${ss}s` : `${m}m`
    const h = Math.floor(m / 60), mm = m % 60
    return mm ? `${h}h ${mm}m` : `${h}h`
  }

  // Debounce + in-flight guard for loadDashboard() triggered by burst-y
  // websocket events. Without this, rapid messages (e.g. a job that emits
  // many config_changed/run_started/run_completed in quick succession) can
  // queue up several overlapping fetches that each clobber the UI state.
  /** @type {ReturnType<typeof setTimeout> | null} */
  let dashboardReloadTimer = null
  let dashboardReloadInFlight = false
  let dashboardReloadPending = false

  function scheduleDashboardReload() {
    if (dashboardReloadTimer !== null) {
      clearTimeout(dashboardReloadTimer)
    }
    dashboardReloadTimer = setTimeout(() => {
      dashboardReloadTimer = null
      void runDashboardReload()
    }, 300)
  }

  async function runDashboardReload() {
    if (dashboardReloadInFlight) {
      // Coalesce: remember that another reload was requested while we were
      // mid-flight and run exactly one more after the current call returns.
      dashboardReloadPending = true
      return
    }
    dashboardReloadInFlight = true
    try {
      await loadDashboard()
    } finally {
      dashboardReloadInFlight = false
      if (dashboardReloadPending) {
        dashboardReloadPending = false
        scheduleDashboardReload()
      }
    }
  }

  onMount(() => {
    loadDashboard()
    // Restore progress overlay if a backup/restore is already running.
    api.getRunnerStatus().then(s => restoreFromStatus(s)).catch(() => {})
    const unsub = onWsMessage((msg) => {
      if (msg.type === 'runner_status_snapshot') {
        syncFromStatus(msg.status)
      }
      const jobNameResolver = (id) => jobs.find(j => j.id === id)?.name
      handleProgressMessage(msg, jobNameResolver)
      if (msg.type === 'item_backup_done') {
        liveCumulativeBytes += msg.size_bytes || 0
        if (!liveStartTime) liveStartTime = Date.now()
        const elapsed = (Date.now() - liveStartTime) / 1000
        if (elapsed > 0) liveSpeed = formatSpeed(liveCumulativeBytes, elapsed)
      }
      if (msg.type === 'job_run_completed' || msg.type === 'job_run_started' || msg.type === 'import_completed') {
        liveSpeed = null
        liveCumulativeBytes = 0
        liveStartTime = null
        scheduleDashboardReload()
      }
      if (msg.type === 'config_changed') {
        // Storage / job / replication CRUD changes the inputs to the
        // 3-2-1 compliance widget, the protection-status panel, and the
        // recovery plan. Re-fetch so derived UI stays current without a
        // page reload.
        scheduleDashboardReload()
      }
    })
    return () => {
      unsub()
      if (dashboardReloadTimer !== null) {
        clearTimeout(dashboardReloadTimer)
        dashboardReloadTimer = null
      }
    }
  })

  async function loadDashboard() {
    try {
      const [h, j, s, cRes, vRes, fRes, nextRunsData, hSummary, replSources, sett] = await Promise.all([
        api.health(),
        api.listJobs(),
        api.listStorage(),
        api.listContainers().catch(() => ({ items: [], available: false })),
        api.listVMs().catch(() => ({ items: [], available: false })),
        api.listFolders().catch(() => ({ items: [], available: false })),
        api.getNextRuns().catch(() => ({})),
        api.getHealthSummary().catch(() => null),
        api.listReplicationSources().catch(() => []),
        api.getSettings().catch(() => ({})),
      ])
      health = h
      jobs = j || []
      storage = s || []
      containers = cRes.items || []
      vms = vRes.items || []
      folders = fRes.items || []
      nextRuns = nextRunsData || {}
      healthSummary = hSummary
      replicationSources = replSources || []
      settings = sett || {}

      // Feed the anomaly tile's count from the shared store (also kept live by
      // WS). Only when anomaly detection is enabled — otherwise the tile hides.
      if (getAnomalyEnabled()) {
        api.listAnomalies({ state: 'open', limit: 100 }).then(r => setOpenList(r?.anomalies ?? [])).catch(() => {})
      }

      // Protection is computed server-side from actual restore-point
      // membership (health summary's protected_keys/pending_keys), so an item
      // counts as protected only once a backup has really captured it. Items
      // configured in a job but not yet in any restore point are "pending"
      // (awaiting their first backup) rather than protected. A disabled
      // schedule does not flip already-backed-up items back — the backend
      // keys reflect restore points, not schedule state.
      const pSet = new SvelteSet()
      for (const key of hSummary?.protected_keys || []) pSet.add(key)
      protectedItems = pSet
      const pendSet = new SvelteSet()
      for (const key of hSummary?.pending_keys || []) pendSet.add(key)
      pendingItems = pendSet

      const runPromises = jobs.slice(0, 10).map(async (job) => {
        try {
          const runs = await api.getJobHistory(job.id, 5)
          return (runs || []).map(r => ({ ...r, jobName: job.name }))
        } catch { return [] }
      })
      const allRuns = await Promise.all(runPromises)
      recentRuns = allRuns.flat().sort((a, b) => new Date(b.started_at).getTime() - new Date(a.started_at).getTime()).slice(0, 10)
      error = ''
    } catch (e) {
      error = e.message || 'Failed to load dashboard'
    } finally {
      loading = false
    }
  }

  async function runNow(job) {
    runningJob = job.id
    try {
      await api.runJob(job.id)
      showToast(`"${job.name}" started`, 'success')
    } catch (e) {
      showToast(e.message, 'error')
    } finally {
      runningJob = null
    }
  }

  async function runAll() {
    if (runningAll || enabledJobs.length === 0) return
    runningAll = true
    try {
      for (const j of enabledJobs) {
        await api.runJob(j.id).catch(() => {})
      }
      showToast(`Started ${enabledJobs.length} enabled job${enabledJobs.length === 1 ? '' : 's'}`, 'success')
    } finally {
      runningAll = false
    }
  }

  const enabledJobs = $derived(jobs.filter(j => j.enabled))

  const containerBackupOn = $derived(settings.container_backup_enabled !== 'false')
  const vmBackupOn = $derived(settings.vm_backup_enabled !== 'false')
  const folderBackupOn = $derived(settings.folder_backup_enabled !== 'false')
  const flashBackupOn = $derived(settings.flash_backup_enabled !== 'false')
  const backupRuleOn = $derived(settings.backup_rule_enabled !== 'false')

  // Dismiss (×) on the 3-2-1 panel: hide immediately, persist, and revert on
  // failure. Re-enabled from Settings → Dashboard.
  async function dismissBackupRule() {
    const original = settings.backup_rule_enabled
    settings = { ...settings, backup_rule_enabled: 'false' }
    try {
      await api.updateSettings({ backup_rule_enabled: 'false' })
      showToast('3-2-1 Backup Rule hidden – re-enable in Settings → Dashboard')
    } catch (e) {
      settings = { ...settings, backup_rule_enabled: original }
      showToast(e.message || 'Could not hide the panel', 'error')
    }
  }

  // Persist the chosen 3-2-1 goal (optimistic, revert on failure). When unset,
  // the panel auto-detects the goal from the current setup.
  async function setBackupRuleGoal(goal) {
    const original = settings.backup_rule_goal
    settings = { ...settings, backup_rule_goal: goal }
    try {
      await api.updateSettings({ backup_rule_goal: goal })
    } catch (e) {
      settings = { ...settings, backup_rule_goal: original }
      showToast(e.message || 'Could not save backup goal', 'error')
    }
  }

  const trackedContainers = $derived(containerBackupOn ? containers : [])
  const trackedVMs = $derived(vmBackupOn ? vms : [])
  const trackedFolders = $derived(folderBackupOn ? folders.filter(f => f.settings?.preset !== 'flash') : [])
  const trackedFlash = $derived(flashBackupOn ? folders.filter(f => f.settings?.preset === 'flash') : [])

  // Three-way state per item: protected (in a real restore point), pending
  // (configured in a job but not yet backed up), or unprotected (not in any
  // job). Pending is neither green nor red.
  const isPending = (key) => pendingItems.has(key)
  const protectedContainers = $derived(trackedContainers.filter(c => protectedItems.has(`container:${c.name}`)))
  const unprotectedContainers = $derived(trackedContainers.filter(c => !protectedItems.has(`container:${c.name}`) && !isPending(`container:${c.name}`)))
  const protectedVMs = $derived(trackedVMs.filter(v => protectedItems.has(`vm:${v.name}`)))
  const unprotectedVMs = $derived(trackedVMs.filter(v => !protectedItems.has(`vm:${v.name}`) && !isPending(`vm:${v.name}`)))
  const protectedFolders = $derived(trackedFolders.filter(f => protectedItems.has(`folder:${f.name}`)))
  const protectedFlash = $derived(trackedFlash.filter(f => protectedItems.has(`folder:${f.name}`)))
  const unprotectedFolders = $derived(trackedFolders.filter(f => !protectedItems.has(`folder:${f.name}`) && !isPending(`folder:${f.name}`)))
  const unprotectedFlash = $derived(trackedFlash.filter(f => !protectedItems.has(`folder:${f.name}`) && !isPending(`folder:${f.name}`)))
  const unprotectedCount = $derived(
    unprotectedContainers.length + unprotectedVMs.length + unprotectedFolders.length + unprotectedFlash.length
  )
  // Any unprotected item (not yet in a backup, of any type) → show the CTA.
  const hasUnprotectedItems = $derived(unprotectedCount > 0)

  const totalItems = $derived(trackedContainers.length + trackedVMs.length + trackedFolders.length + trackedFlash.length)
  const totalProtected = $derived(protectedContainers.length + protectedVMs.length + protectedFolders.length + protectedFlash.length)
  const protectionPct = $derived(totalItems > 0 ? Math.round((totalProtected / totalItems) * 100) : 0)

  // Collapse the Protection Status panel when everything is covered (issue #211
  // / E7). Below 100% the panel is always expanded so unprotected/pending items
  // stay visible; at 100% it collapses by default and the user's manual choice
  // persists in localStorage.
  const PROTECTION_EXPANDED_KEY = 'vault:dash:protectionExpanded'
  let protectionExpandedPref = $state(readProtectionPref())
  function readProtectionPref() {
    try {
      const v = localStorage.getItem(PROTECTION_EXPANDED_KEY)
      return v === null ? null : v === 'true'
    } catch { return null }
  }
  const protectionExpanded = $derived(protectionPct < 100 ? true : (protectionExpandedPref ?? false))
  function toggleProtection() {
    const next = !protectionExpanded
    protectionExpandedPref = next
    try { localStorage.setItem(PROTECTION_EXPANDED_KEY, String(next)) } catch { /* ignore */ }
  }

  const soonestNextRun = $derived.by(() => {
    const times = Object.values(nextRuns).map(t => new Date(t)).filter(d => !isNaN(d.getTime()))
    if (times.length === 0) return null
    return new Date(Math.min(...times.map(d => d.getTime()))).toISOString()
  })

  const soonestJob = $derived.by(() => {
    let best = null, bestT = Infinity
    for (const [jid, t] of Object.entries(nextRuns)) {
      const ms = new Date(t).getTime()
      if (!isNaN(ms) && ms < bestT) { bestT = ms; best = jid }
    }
    if (best == null) return null
    return jobs.find(j => String(j.id) === String(best)) || null
  })

  const avgSpeed = $derived.by(() => {
    const completed = recentRuns.filter(r => (r.status === 'completed' || r.status === 'success') && r.size_bytes && r.duration_seconds);
    if (!completed.length) return null;
    const totalBytes = completed.reduce((s, r) => s + r.size_bytes, 0);
    const totalSecs = completed.reduce((s, r) => s + r.duration_seconds, 0);
    return formatSpeed(totalBytes, totalSecs);
  })

  // Most recent completed/failed backup (not restore) for the Last-backup tile.
  const lastBackup = $derived(recentRuns.find(r => (r.run_type || 'backup') === 'backup') || null)

  // Recent backup success rate from the runs we already loaded (not a full 30d
  // window — that would need a history endpoint this page doesn't fetch).
  const successStats = $derived.by(() => {
    const runs = recentRuns.filter(r => (r.run_type || 'backup') === 'backup' && r.status !== 'running')
    if (!runs.length) return null
    const ok = runs.filter(r => r.status === 'completed' || r.status === 'success').length
    return { pct: Math.round((ok / runs.length) * 100), ok, total: runs.length }
  })

  const recentFailures = $derived(recentRuns.filter(r => r.status === 'failed' || r.status === 'error').length)
  const attentionCount = $derived(recentFailures + unprotectedCount)

  // Storage capacity tiles derive from destinations that reported a probe.
  const storageCaps = $derived(storage.filter(s => s.capacity && s.capacity.total_bytes > 0))
  const storageCombined = $derived.by(() => {
    if (!storageCaps.length) return null
    const used = storageCaps.reduce((a, s) => a + (s.capacity.used_bytes || 0), 0)
    const total = storageCaps.reduce((a, s) => a + (s.capacity.total_bytes || 0), 0)
    return { used, total, pct: total > 0 ? Math.round((used / total) * 100) : 0, count: storageCaps.length }
  })

  // Top recent backups by size (for the Largest-backups tile), one row per job.
  const largestBackups = $derived.by(() => {
    const byJob = {}
    for (const r of recentRuns) {
      if ((r.run_type || 'backup') !== 'backup' || !r.size_bytes) continue
      if (byJob[r.jobName] == null || r.size_bytes > byJob[r.jobName]) byJob[r.jobName] = r.size_bytes
    }
    return Object.entries(byJob).map(([name, size]) => ({ name, size })).sort((a, b) => b.size - a.size).slice(0, 5)
  })

  // ── Lazily-loaded data for optional tiles (default-off, so fetched only when
  // the user adds the tile). Each guards against duplicate in-flight loads. ──
  /** @type {{ points: Array<{start: string, total_bytes: number}> } | null} */
  let trendData = $state(null)
  let trendLoading = false
  async function loadTrend() {
    if (trendLoading || trendData) return
    trendLoading = true
    try { trendData = await api.getHistoryTrend('30d') } catch { /* ignore */ } finally { trendLoading = false }
  }

  /** @type {{ ratio: number, logical: number, physical: number } | null} */
  let dedupSummary = $state(null)
  let dedupLoading = false
  async function loadDedup() {
    if (dedupLoading || dedupSummary) return
    dedupLoading = true
    try {
      const dests = storage.filter(s => s.dedup_enabled)
      const stats = await Promise.all(dests.map(d => api.dedupStats(d.id).catch(() => null)))
      let logical = 0, physical = 0
      for (const st of stats) { if (st) { logical += st.logical_bytes || 0; physical += st.physical_bytes || 0 } }
      dedupSummary = physical > 0 ? { ratio: logical / physical, logical, physical } : null
    } catch { /* ignore */ } finally { dedupLoading = false }
  }

  /** @type {{ name: string, days: number, perDay: number } | null} */
  let forecastSummary = $state(null)
  let forecastLoading = false
  async function loadForecast() {
    if (forecastLoading || forecastSummary) return
    forecastLoading = true
    try {
      const dests = storage.filter(s => s.capacity && s.capacity.total_bytes > 0)
      const trajectories = await Promise.all(dests.map(d => api.getCapacityTrajectory(d.id).then(t => ({ d, t })).catch(() => null)))
      let soonest = null
      for (const entry of trajectories) {
        if (!entry) continue
        const samples = (entry.t?.samples || []).filter(sm => sm.free_bytes != null)
        if (samples.length < 2) continue
        const first = samples[0], last = samples[samples.length - 1]
        const days = (new Date(last.sampled_at).getTime() - new Date(first.sampled_at).getTime()) / 86400000
        if (days <= 0) continue
        const perDay = (first.free_bytes - last.free_bytes) / days // bytes consumed/day
        if (perDay <= 0) continue // not filling
        const daysToFull = last.free_bytes / perDay
        if (!soonest || daysToFull < soonest.days) soonest = { name: entry.d.name, days: Math.round(daysToFull), perDay }
      }
      forecastSummary = soonest
    } catch { /* ignore */ } finally { forecastLoading = false }
  }

  // Trigger the lazy loaders whenever a tile that needs their data is present.
  $effect(() => {
    if ((layout.order.includes('sizeTrend') || layout.order.includes('calendar')) && !trendData) loadTrend()
    if (layout.order.includes('savings') && !dedupSummary) loadDedup()
    if (layout.order.includes('forecast') && !forecastSummary) loadForecast()
  })

  const trendChange = $derived.by(() => {
    const pts = trendData?.points?.filter(p => p.total_bytes > 0) || []
    if (pts.length < 2) return null
    const first = pts[0].total_bytes, last = pts[pts.length - 1].total_bytes
    return { first, last, pctChange: first > 0 ? Math.round(((last - first) / first) * 100) : 0 }
  })

  // SVG polyline points for the size-trend sparkline (normalized to 300×64).
  const trendPolyline = $derived.by(() => {
    const pts = (trendData?.points || []).map(p => p.total_bytes)
    if (pts.length < 2) return ''
    const max = Math.max(...pts, 1)
    return pts.map((v, i) => `${(i / (pts.length - 1) * 300).toFixed(1)},${(64 - (v / max) * 58).toFixed(1)}`).join(' ')
  })

  // Recent days as heatmap cells (backup ran that day → coloured by size).
  const calendarCells = $derived.by(() => {
    const pts = trendData?.points || []
    if (!pts.length) return []
    const max = Math.max(...pts.map(p => p.total_bytes), 1)
    return pts.slice(-35).map(p => ({ date: p.start, ran: p.total_bytes > 0, intensity: p.total_bytes / max }))
  })

  const healthScore = $derived(healthSummary?.health_score ?? 0)
  const healthColor = $derived(healthScore >= 80 ? 'var(--color-success)' : healthScore >= 50 ? 'var(--color-warning)' : 'var(--color-danger)')
  const healthSummaryText = $derived.by(() => {
    if (!healthSummary) return ''
    const s = healthSummary
    const score = s.health_score ?? 0
    if (s.recent_failed > 0) {
      return `${s.recent_failed} recent failure${s.recent_failed === 1 ? '' : 's'} – check History`
    }
    if (score >= 80) return 'All backups healthy'
    if (score >= 50) return 'Backups mostly healthy'
    return 'Attention needed – recent backups have not completed'
  })

  function barColor(pct) { return pct === 100 ? 'bg-success' : pct >= 50 ? 'bg-warning' : 'bg-danger' }

  // ── Tile catalog + layout ────────────────────────────────────────────────
  // span = 12-col width. `bare` tiles render their own card (they reuse a
  // self-carding component/panel); the rest get the shared card shell.
  const CATALOG = {
    health:       { name: 'Health score',       span: 3, glyph: '♥' },
    protected:    { name: 'Protected items',    span: 3, glyph: '◈' },
    nextrun:      { name: 'Next run',           span: 3, glyph: '⏱' },
    lastbackup:   { name: 'Last backup',        span: 3, glyph: '✓' },
    threetwoone:  { name: '3-2-1 rule',         span: 12, glyph: '3', bare: true },
    progress:     { name: 'Backup in progress', span: 6, glyph: '◐', bare: true },
    activity:     { name: 'Recent activity',    span: 6, glyph: '≡', bare: true },
    jobs:         { name: 'Backup jobs',        span: 6, glyph: '▤', bare: true },
    protection:   { name: 'Protection status',  span: 6, glyph: '▦', bare: true },
    storageCombined:  { name: 'Storage — combined',  span: 4, glyph: '⛁' },
    storagePerTarget: { name: 'Storage — per target', span: 6, glyph: '⛁' },
    recovery:     { name: 'Recovery readiness', span: 4, glyph: '⛑' },
    attention:    { name: 'Needs attention',    span: 4, glyph: '!' },
    successrate:  { name: 'Success rate',       span: 4, glyph: '%' },
    anomalies:    { name: 'Anomalies',          span: 4, glyph: '⚠' },
    quickactions: { name: 'Quick actions',      span: 4, glyph: '⚡' },
    sizeTrend:    { name: 'Backup size trend',  span: 6, glyph: '📈' },
    calendar:     { name: 'Backup calendar',    span: 6, glyph: '▦' },
    savings:      { name: 'Dedup & compression', span: 4, glyph: '⇊' },
    forecast:     { name: 'Storage forecast',   span: 4, glyph: '◔' },
    largest:      { name: 'Largest backups',    span: 6, glyph: '⬒' },
  }

  const layout = createDashboardLayout(Object.keys(CATALOG))

  // A tile is hidden in normal view (but still listed in edit mode) when it
  // can't be computed from current data — so we never render a broken tile.
  function tileAvailable(id) {
    if (id === 'threetwoone') return jobs.length > 0 && backupRuleOn
    if (id === 'storageCombined' || id === 'storagePerTarget') return storageCaps.length > 0
    if (id === 'anomalies') return getAnomalyEnabled()
    if (id === 'protection') return totalItems > 0
    if (id === 'savings') return storage.some(s => s.dedup_enabled)
    if (id === 'largest') return largestBackups.length > 0
    // sizeTrend / calendar / forecast render their own loading/empty state, so
    // they stay visible while data loads or if the series is thin.
    return true
  }

  const tiles = $derived(layout.order.map((id, idx) => ({ id, idx, ...CATALOG[id] })))
  const visibleTiles = $derived(layout.editMode ? tiles : tiles.filter(t => tileAvailable(t.id)))
  const catalogList = $derived(
    Object.keys(CATALOG).map(id => ({ id, ...CATALOG[id], shown: layout.order.includes(id) }))
  )

  // Drag-and-drop reorder (desktop). Keyboard/touch use the ↑/↓ buttons.
  let dragIdx = $state(-1)
  let overIdx = $state(-1)
  function onDragStart(e, idx) {
    if (!layout.editMode) return
    dragIdx = idx
    if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
  }
  function onDragOver(e, idx) {
    if (!layout.editMode || dragIdx < 0) return
    e.preventDefault()
    if (idx !== overIdx) overIdx = idx
  }
  function onDrop(e, idx) {
    if (!layout.editMode || dragIdx < 0) return
    e.preventDefault()
    if (dragIdx !== idx) layout.move(dragIdx, idx)
    dragIdx = -1
    overIdx = -1
  }
  function onDragEnd() { dragIdx = -1; overIdx = -1 }
</script>

<Toast message={toast.message} type={toast.type} key={toast.key} />

{#snippet pendingBadge()}
  <span class="ml-auto inline-flex items-center gap-1 text-[11px] text-amber-500 shrink-0 whitespace-nowrap" title="In a backup job but not captured in a restore point yet — it will be backed up on the next scheduled run">
    <svg aria-hidden="true" class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>
    pending first backup
  </span>
{/snippet}

<!-- ═══ Tile bodies ═══ -->

{#snippet metricCardEmpty(label)}
  <div class="bg-surface-2 border border-border rounded-xl p-4 h-full min-h-[108px] flex flex-col justify-center">
    <p class="text-xs text-text-muted">{label}</p>
    <p class="text-xs text-text-dim mt-2">Not available yet</p>
  </div>
{/snippet}

{#snippet tHealth()}
  <div class="bg-surface-2 border border-border rounded-xl p-4 h-full min-h-[108px] flex items-center gap-3 cursor-pointer hover:border-vault/40 transition-colors" role="button" tabindex="0" onclick={() => navigate('/history')} onkeydown={(e) => { if (e.key === 'Enter') navigate('/history') }}>
    <div class="relative w-14 h-14 shrink-0">
      <svg aria-hidden="true" viewBox="0 0 36 36" class="w-full h-full -rotate-90">
        <circle cx="18" cy="18" r="15" fill="none" stroke="var(--color-surface-4)" stroke-width="4" />
        <circle cx="18" cy="18" r="15" fill="none" stroke={healthColor} stroke-width="4" stroke-linecap="round"
          stroke-dasharray={2 * Math.PI * 15} stroke-dashoffset={2 * Math.PI * 15 * (1 - healthScore / 100)} class="transition-all duration-700" />
      </svg>
      <div class="absolute inset-0 flex items-center justify-center text-sm font-bold text-text">{healthScore}</div>
    </div>
    <div class="min-w-0">
      <p class="text-xs text-text-muted">Health score</p>
      <p class="text-sm font-semibold text-text truncate">{healthSummaryText || 'Backup health'}</p>
      {#if avgSpeed}<p class="text-[11px] text-text-dim mt-0.5">avg {avgSpeed}</p>{/if}
    </div>
  </div>
{/snippet}

{#snippet tProtected()}
  <div class="bg-surface-2 border border-border rounded-xl p-4 h-full min-h-[108px] flex flex-col justify-center">
    <p class="text-xs text-text-muted">Protected</p>
    <p class="text-2xl font-bold text-text mt-1">{totalProtected}<span class="text-sm text-text-dim font-semibold">/{totalItems}</span></p>
    <div class="h-1.5 bg-surface-4 rounded-full overflow-hidden mt-2">
      <div class="h-full {barColor(protectionPct)} transition-all duration-500" style="width: {protectionPct}%"></div>
    </div>
    {#if hasUnprotectedItems}
      <button onclick={() => navigate('/jobs')} class="text-[11px] text-vault-text hover:text-vault-dark font-medium mt-1.5 text-left">{unprotectedCount} unprotected →</button>
    {:else}
      <p class="text-[11px] text-success mt-1.5 font-medium">All items covered</p>
    {/if}
  </div>
{/snippet}

{#snippet tNextRun()}
  <div class="bg-surface-2 border border-border rounded-xl p-4 h-full min-h-[108px] flex flex-col justify-center cursor-pointer hover:border-vault/40 transition-colors" role="button" tabindex="0" onclick={() => navigate('/jobs')} onkeydown={(e) => { if (e.key === 'Enter') navigate('/jobs') }}>
    <p class="text-xs text-text-muted">Next run</p>
    {#if soonestNextRun}
      <p class="text-lg font-bold text-text mt-1">{relTimeUntil(soonestNextRun)}</p>
      {#if soonestJob}<p class="text-[11px] text-text-dim mt-0.5 truncate">{soonestJob.name}</p>{/if}
    {:else}
      <p class="text-lg font-bold text-text-dim mt-1">No schedule</p>
    {/if}
    <p class="text-[11px] text-vault-text font-medium mt-1.5">{jobs.length} job{jobs.length === 1 ? '' : 's'} · {enabledJobs.length} enabled</p>
  </div>
{/snippet}

{#snippet tLastBackup()}
  <div class="bg-surface-2 border border-border rounded-xl p-4 h-full min-h-[108px] flex flex-col justify-center cursor-pointer hover:border-vault/40 transition-colors" role="button" tabindex="0" onclick={() => navigate('/history')} onkeydown={(e) => { if (e.key === 'Enter') navigate('/history') }}>
    <p class="text-xs text-text-muted">Last backup</p>
    {#if lastBackup}
      {@const ok = lastBackup.status === 'completed' || lastBackup.status === 'success'}
      <p class="text-sm font-bold mt-1 {ok ? 'text-success' : lastBackup.status === 'running' ? 'text-info' : 'text-danger'}">
        {ok ? '✓ Success' : lastBackup.status === 'running' ? '● Running' : '✕ Failed'}
      </p>
      <p class="text-[11px] text-text-dim mt-0.5 truncate">{lastBackup.jobName} · {relTime(lastBackup.started_at)}</p>
      {#if lastBackup.size_bytes || lastBackup.duration_seconds}
        <p class="text-[11px] text-text-muted mt-0.5">{formatBytes(lastBackup.size_bytes || 0)}{lastBackup.duration_seconds ? ` · ${fmtDur(lastBackup.duration_seconds)}` : ''}</p>
      {/if}
    {:else}
      <p class="text-sm font-bold text-text-dim mt-1">No runs yet</p>
    {/if}
  </div>
{/snippet}

{#snippet tThreeTwoOne()}
  {#if jobs.length > 0 && backupRuleOn}
    <ComplianceBadge {storage} {jobs} {replicationSources} ondismiss={dismissBackupRule} goalSetting={settings.backup_rule_goal || ''} onGoalChange={setBackupRuleGoal} />
  {:else}
    <div class="bg-surface-2 border border-border rounded-xl p-4 text-sm text-text-dim">3-2-1 rule unavailable — add a job to compute it.</div>
  {/if}
{/snippet}

{#snippet tProgress()}
  {#if progress.activeRun}
    {@const progressItems = Object.entries(progress.itemProgress)}
    {@const activeItemPct = progressItems.reduce((maxPct, [, info]) => info.status === 'running' ? Math.max(maxPct, info.percent || 0) : maxPct, 0)}
    {@const overallPct = progress.overallTotal > 0 ? Math.min(100, Math.round((((progress.overallDone + progress.overallFailed) + (activeItemPct / 100)) / progress.overallTotal) * 100)) : activeItemPct}
    {@const elapsedStr = progress.elapsedSec >= 3600 ? `${Math.floor(progress.elapsedSec / 3600)}h ${Math.floor((progress.elapsedSec % 3600) / 60)}m` : progress.elapsedSec >= 60 ? `${Math.floor(progress.elapsedSec / 60)}m ${progress.elapsedSec % 60}s` : `${progress.elapsedSec}s`}
    {@const activeRunLabel = progress.activeRun.run_type === 'restore' ? 'Restore in progress' : 'Backup in progress'}
    <div class="bg-surface-2 border border-vault/30 rounded-xl p-5 h-full" role="status" aria-live="polite">
      <div class="flex items-center gap-2.5 mb-3">
        <div class="w-2.5 h-2.5 rounded-full bg-vault animate-pulse"></div>
        <h2 class="text-sm font-semibold text-text">{activeRunLabel}</h2>
        <span class="ml-auto text-[11px] px-2.5 py-0.5 rounded-full bg-vault/15 text-vault font-medium truncate max-w-[45%]">{progress.activeRun.job_name}</span>
      </div>
      <div class="flex items-center justify-between text-xs text-text-muted mb-1.5">
        <span>Overall progress</span><span class="font-mono text-text-dim">{overallPct}%</span>
      </div>
      <div class="w-full h-2 bg-surface-4 rounded-full overflow-hidden">
        <div class="h-full rounded-full transition-all duration-300 {overallPct < 100 ? 'shimmer-bar' : 'bg-vault'}" style="width: {overallPct}%"></div>
      </div>
      <p class="text-[11px] text-text-dim mt-2">
        {progress.overallDone}/{progress.overallTotal} items · {elapsedStr}{#if progress.overallFailed > 0} · <span class="text-danger">{progress.overallFailed} failed</span>{/if}{#if liveSpeed} · <span class="text-info">{liveSpeed}</span>{/if}
      </p>
      {#if progress.phaseMessage}<p class="text-[11px] text-warning animate-pulse mt-1">{progress.phaseMessage}</p>{/if}
    </div>
  {:else}
    <div class="bg-surface-2 border border-border rounded-xl p-5 h-full min-h-[120px] flex flex-col items-center justify-center text-center">
      <svg aria-hidden="true" class="w-7 h-7 text-text-dim mb-2" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z"/></svg>
      <p class="text-sm font-medium text-text">No backup running</p>
      {#if soonestNextRun}<p class="text-xs text-text-dim mt-1">Next: {relTimeUntil(soonestNextRun)}</p>{/if}
    </div>
  {/if}
{/snippet}

{#snippet tActivity()}
  <ActivityTimeline runs={recentRuns} maxItems={6} />
{/snippet}

{#snippet tJobs()}
  <div class="bg-surface-2 border border-border rounded-xl h-full">
    <div class="px-5 py-4 border-b border-border flex items-center">
      <h2 class="text-base font-semibold text-text">Backup Jobs</h2>
      <button onclick={() => navigate('/jobs')} class="ml-auto text-xs text-vault-text hover:text-vault-dark font-medium">View all →</button>
    </div>
    {#if jobs.length === 0}
      <div class="px-5 py-8 text-center text-sm text-text-muted">No backup jobs configured</div>
    {:else}
      <div class="divide-y divide-border">
        {#each jobs.slice(0, 5) as job (job.id)}
          <div class="px-5 py-3 flex items-center justify-between gap-2">
            <div class="min-w-0">
              <p class="text-sm font-medium text-text truncate">{job.name}</p>
              <p class="text-xs text-text-dim">{job.enabled ? 'Enabled' : 'Disabled'} · {job.compression || 'none'}</p>
            </div>
            {#if !isReplicaMode()}
              <div class="flex items-center gap-2 shrink-0">
                <button onclick={() => navigate(`/restore?job=${job.id}`)} class="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-lg font-medium transition-colors bg-surface-3 text-text-muted hover:bg-surface-4 hover:text-text" title="Restore from {job.name}">
                  <svg aria-hidden="true" class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/></svg>
                  Restore
                </button>
                <button onclick={() => runNow(job)} disabled={runningJob === job.id} class="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-lg font-medium transition-colors bg-vault/10 text-vault hover:bg-vault/20 disabled:opacity-50">
                  {#if runningJob === job.id}
                    <svg aria-hidden="true" class="w-3.5 h-3.5 animate-spin" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"/></svg>
                    Running...
                  {:else}
                    <svg aria-hidden="true" class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z"/></svg>
                    Run Now
                  {/if}
                </button>
              </div>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  </div>
{/snippet}

{#snippet protItemRow(type, key, name, restoreType)}
  {@const isProtected = protectedItems.has(key)}
  {@const pending = isPending(key)}
  <div class="flex items-center gap-2.5 px-3 py-2 rounded-lg {isProtected ? 'bg-success/5' : pending ? 'bg-amber-500/5' : 'bg-surface-3'} group">
    <div class="w-2 h-2 rounded-full shrink-0 {isProtected ? 'bg-success' : pending ? 'bg-amber-500' : 'bg-surface-5'}"></div>
    <span class="text-sm text-text truncate">{name}</span>
    {#if type === 'flash'}<span class="text-[11px] px-1.5 py-0.5 rounded bg-amber-500/15 text-amber-400 font-medium shrink-0">USB boot drive</span>{/if}
    {#if isProtected}
      <button onclick={() => navigate(`/restore?type=${restoreType}&name=${encodeURIComponent(name)}`)} class="ml-auto opacity-40 hover:opacity-100 p-1 text-vault hover:bg-vault/10 rounded transition-all" title="Restore {name}">
        <svg aria-hidden="true" class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/></svg>
      </button>
      <svg aria-hidden="true" class="w-3.5 h-3.5 text-success shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z"/></svg>
    {:else if pending}
      {@render pendingBadge()}
    {:else}
      <span class="text-[11px] text-text-dim ml-auto">unprotected</span>
    {/if}
  </div>
{/snippet}

{#snippet tProtection()}
  {#if totalItems > 0}
    <div class="bg-surface-2 border border-border rounded-xl h-full">
      <div class="px-5 py-4 flex items-center justify-between {protectionExpanded ? 'border-b border-border' : ''}">
        <div class="flex items-center gap-3">
          <h2 class="text-base font-semibold text-text">Protection Status</h2>
          <span class="text-xs px-2.5 py-1 rounded-full font-medium {protectionPct === 100 ? 'bg-success/15 text-success' : protectionPct >= 50 ? 'bg-warning/15 text-warning' : 'bg-danger/15 text-danger'}">
            {totalProtected}/{totalItems} · {protectionPct}%
          </span>
        </div>
        {#if hasUnprotectedItems}
          <button onclick={() => navigate('/jobs')} class="text-xs text-vault-text hover:text-vault-dark transition-colors font-medium">+ Add to Backup</button>
        {:else if protectionPct === 100}
          <button onclick={toggleProtection} aria-expanded={protectionExpanded} class="flex items-center gap-1 text-xs font-medium text-text-muted hover:text-text transition-colors">
            {protectionExpanded ? 'Hide items' : 'Show items'}
            <svg aria-hidden="true" class="w-4 h-4 transition-transform {protectionExpanded ? 'rotate-180' : ''}" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"/></svg>
          </button>
        {/if}
      </div>
      {#if protectionExpanded}
        <div class="p-5">
          <div class="w-full h-2 bg-surface-4 rounded-full overflow-hidden mb-5">
            <div class="h-full rounded-full transition-all duration-500 {barColor(protectionPct)}" style="width: {protectionPct}%"></div>
          </div>
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-6">
            {#if trackedContainers.length > 0}
              <div>
                <div class="flex items-center gap-2 mb-3">
                  <svg aria-hidden="true" class="w-4 h-4 text-blue-400" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4"/></svg>
                  <h3 class="text-sm font-medium text-text">Containers</h3>
                  <span class="text-xs text-text-dim ml-auto">{protectedContainers.length}/{trackedContainers.length}</span>
                </div>
                <div class="space-y-1.5">{#each trackedContainers as c (c.name)}{@render protItemRow('container', `container:${c.name}`, c.name, 'container')}{/each}</div>
              </div>
            {/if}
            {#if trackedVMs.length > 0}
              <div>
                <div class="flex items-center gap-2 mb-3">
                  <svg aria-hidden="true" class="w-4 h-4 text-purple-400" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"/></svg>
                  <h3 class="text-sm font-medium text-text">Virtual Machines</h3>
                  <span class="text-xs text-text-dim ml-auto">{protectedVMs.length}/{trackedVMs.length}</span>
                </div>
                <div class="space-y-1.5">{#each trackedVMs as v (v.name)}{@render protItemRow('vm', `vm:${v.name}`, v.name, 'vm')}{/each}</div>
              </div>
            {/if}
            {#if trackedFolders.length > 0}
              <div>
                <div class="flex items-center gap-2 mb-3">
                  <svg aria-hidden="true" class="w-4 h-4 text-amber-400" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z"/></svg>
                  <h3 class="text-sm font-medium text-text">Folders</h3>
                  <span class="text-xs text-text-dim ml-auto">{protectedFolders.length}/{trackedFolders.length}</span>
                </div>
                <div class="space-y-1.5">{#each trackedFolders as f (f.name)}{@render protItemRow('folder', `folder:${f.name}`, f.name, 'folder')}{/each}</div>
              </div>
            {/if}
            {#if trackedFlash.length > 0}
              <div>
                <div class="flex items-center gap-2 mb-3">
                  <svg aria-hidden="true" class="w-4 h-4 text-amber-400" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7v8a2 2 0 002 2h6M8 7V5a2 2 0 012-2h4.586a1 1 0 01.707.293l4.414 4.414a1 1 0 01.293.707V15a2 2 0 01-2 2h-2M8 7H6a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2v-2"/></svg>
                  <h3 class="text-sm font-medium text-text">Flash Drive</h3>
                  <span class="text-xs text-text-dim ml-auto">{protectedFlash.length}/{trackedFlash.length}</span>
                </div>
                <div class="space-y-1.5">{#each trackedFlash as f (f.name)}{@render protItemRow('flash', `folder:${f.name}`, f.name, 'folder')}{/each}</div>
              </div>
            {/if}
          </div>
        </div>
      {/if}
    </div>
  {:else}
    {@render metricCardEmpty('Protection status')}
  {/if}
{/snippet}

{#snippet tStorageCombined()}
  {#if storageCombined}
    <div class="bg-surface-2 border border-border rounded-xl p-4 h-full min-h-[108px] flex flex-col justify-center cursor-pointer hover:border-vault/40 transition-colors" role="button" tabindex="0" onclick={() => navigate('/storage')} onkeydown={(e) => { if (e.key === 'Enter') navigate('/storage') }}>
      <p class="text-xs text-text-muted">Storage — combined</p>
      <p class="text-xl font-bold text-text mt-1">{formatBytes(storageCombined.used)}</p>
      <p class="text-[11px] text-text-dim mt-0.5">of {formatBytes(storageCombined.total)} · {storageCombined.count} target{storageCombined.count === 1 ? '' : 's'}</p>
      <div class="h-1.5 bg-surface-4 rounded-full overflow-hidden mt-2"><div class="h-full bg-vault" style="width: {storageCombined.pct}%"></div></div>
    </div>
  {:else}{@render metricCardEmpty('Storage — combined')}{/if}
{/snippet}

{#snippet tStoragePerTarget()}
  <div class="bg-surface-2 border border-border rounded-xl p-5 h-full cursor-pointer hover:border-vault/40 transition-colors" role="button" tabindex="0" onclick={() => navigate('/storage')} onkeydown={(e) => { if (e.key === 'Enter') navigate('/storage') }}>
    <h2 class="text-sm font-semibold text-text mb-3">Storage — per target</h2>
    {#if storageCaps.length}
      <div class="flex flex-col gap-3">
        {#each storageCaps as s (s.id)}
          {@const pct = s.capacity.total_bytes > 0 ? Math.round((s.capacity.used_bytes || 0) / s.capacity.total_bytes * 100) : 0}
          <div>
            <div class="flex justify-between text-xs mb-1"><span class="text-text truncate">{s.name}</span><span class="text-text-dim shrink-0 ml-2">{formatBytes(s.capacity.used_bytes || 0)}</span></div>
            <div class="h-1.5 bg-surface-4 rounded-full overflow-hidden"><div class="h-full bg-vault" style="width: {pct}%"></div></div>
          </div>
        {/each}
      </div>
    {:else}<p class="text-xs text-text-dim">No capacity data yet — probe a destination on the Storage page.</p>{/if}
  </div>
{/snippet}

{#snippet tRecovery()}
  <div class="bg-surface-2 border border-border rounded-xl p-4 h-full min-h-[108px] flex flex-col justify-center cursor-pointer" onclick={() => navigate('/recovery')} role="button" tabindex="0" onkeydown={(e) => { if (e.key === 'Enter') navigate('/recovery') }}>
    <p class="text-xs text-text-muted">Recovery readiness</p>
    <p class="text-2xl font-bold mt-1 {protectionPct === 100 ? 'text-success' : protectionPct >= 50 ? 'text-warning' : 'text-danger'}">{protectionPct}%</p>
    <p class="text-[11px] text-text-dim mt-1.5">{totalProtected}/{totalItems} items recoverable</p>
  </div>
{/snippet}

{#snippet tAttention()}
  <div class="bg-surface-2 border border-border rounded-xl p-4 h-full min-h-[108px] flex flex-col justify-center cursor-pointer hover:border-vault/40 transition-colors" role="button" tabindex="0" onclick={() => navigate('/history')} onkeydown={(e) => { if (e.key === 'Enter') navigate('/history') }}>
    <p class="text-xs text-text-muted">Needs attention</p>
    <p class="text-2xl font-bold mt-1 {attentionCount === 0 ? 'text-success' : 'text-danger'}">{attentionCount}</p>
    <p class="text-[11px] text-text-dim mt-1.5">{attentionCount === 0 ? 'No failures · all items protected' : `${recentFailures} recent failure${recentFailures === 1 ? '' : 's'} · ${unprotectedCount} unprotected`}</p>
  </div>
{/snippet}

{#snippet tSuccessRate()}
  <div class="bg-surface-2 border border-border rounded-xl p-4 h-full min-h-[108px] flex flex-col justify-center cursor-pointer hover:border-vault/40 transition-colors" role="button" tabindex="0" onclick={() => navigate('/history')} onkeydown={(e) => { if (e.key === 'Enter') navigate('/history') }}>
    <p class="text-xs text-text-muted">Success rate · recent</p>
    {#if successStats}
      <p class="text-2xl font-bold text-text mt-1">{successStats.pct}%</p>
      <p class="text-[11px] text-text-dim mt-1.5">{successStats.ok} of {successStats.total} recent runs succeeded</p>
    {:else}
      <p class="text-2xl font-bold text-text-dim mt-1">—</p>
      <p class="text-[11px] text-text-dim mt-1.5">No completed runs yet</p>
    {/if}
  </div>
{/snippet}

{#snippet tAnomalies()}
  {#if getAnomalyEnabled()}
    <div class="bg-surface-2 border border-border rounded-xl p-4 h-full min-h-[108px] flex flex-col justify-center cursor-pointer" onclick={() => navigate('/anomalies')} role="button" tabindex="0" onkeydown={(e) => { if (e.key === 'Enter') navigate('/anomalies') }}>
      <p class="text-xs text-text-muted">Anomalies</p>
      <p class="text-2xl font-bold mt-1 {anomalies.openList.length === 0 ? 'text-success' : 'text-warning'}">{anomalies.openList.length}</p>
      <p class="text-[11px] text-text-dim mt-1.5">{anomalies.openList.length === 0 ? 'No unusual runs detected' : 'open — review on Anomalies'}</p>
    </div>
  {:else}{@render metricCardEmpty('Anomalies')}{/if}
{/snippet}

{#snippet tQuickActions()}
  <div class="bg-surface-2 border border-border rounded-xl p-4 h-full min-h-[108px] flex flex-col">
    <p class="text-sm font-semibold text-text mb-3">Quick actions</p>
    <div class="flex flex-wrap gap-2">
      <button onclick={runAll} disabled={runningAll || enabledJobs.length === 0} class="text-xs font-medium px-3 py-2 rounded-lg bg-vault text-white hover:bg-vault-dark disabled:opacity-50 transition-colors">
        {runningAll ? 'Starting…' : 'Run all backups'}
      </button>
      <button onclick={() => navigate('/jobs')} class="text-xs font-medium px-3 py-2 rounded-lg bg-vault/10 text-vault hover:bg-vault/20 transition-colors">New job</button>
      <button onclick={() => navigate('/restore')} class="text-xs font-medium px-3 py-2 rounded-lg bg-vault/10 text-vault hover:bg-vault/20 transition-colors">Restore…</button>
    </div>
  </div>
{/snippet}

{#snippet tSizeTrend()}
  <div class="bg-surface-2 border border-border rounded-xl p-5 h-full cursor-pointer hover:border-vault/40 transition-colors" role="button" tabindex="0" onclick={() => navigate('/history')} onkeydown={(e) => { if (e.key === 'Enter') navigate('/history') }}>
    <div class="flex items-center mb-3">
      <h2 class="text-sm font-semibold text-text">Backup size trend</h2>
      {#if trendChange}
        <span class="ml-auto text-[11px] font-medium px-2 py-0.5 rounded-full {trendChange.pctChange >= 0 ? 'bg-warning/15 text-warning' : 'bg-success/15 text-success'}">{trendChange.pctChange >= 0 ? '+' : ''}{trendChange.pctChange}% / 30d</span>
      {/if}
    </div>
    {#if trendPolyline}
      <svg viewBox="0 0 300 70" preserveAspectRatio="none" class="w-full h-16" aria-hidden="true">
        <polyline points={trendPolyline} fill="none" stroke="var(--color-vault)" stroke-width="2.5" vector-effect="non-scaling-stroke" />
      </svg>
      {#if trendChange}<p class="text-[11px] text-text-dim mt-2">{formatBytes(trendChange.last)}/day now · was {formatBytes(trendChange.first)}</p>{/if}
    {:else}
      <p class="text-xs text-text-dim py-6 text-center">{trendLoading ? 'Loading trend…' : 'Not enough history yet'}</p>
    {/if}
  </div>
{/snippet}

{#snippet tCalendar()}
  <div class="bg-surface-2 border border-border rounded-xl p-5 h-full cursor-pointer hover:border-vault/40 transition-colors" role="button" tabindex="0" onclick={() => navigate('/history')} onkeydown={(e) => { if (e.key === 'Enter') navigate('/history') }}>
    <div class="flex items-center mb-3">
      <h2 class="text-sm font-semibold text-text">Backup calendar</h2>
      <span class="ml-auto text-[11px] text-text-dim">Last 30 days</span>
    </div>
    {#if calendarCells.length}
      <div class="grid gap-1" style="grid-template-columns: repeat(15, minmax(0, 1fr));">
        {#each calendarCells as cell (cell.date)}
          <div class="aspect-square rounded-sm {cell.ran ? '' : 'bg-surface-4'}" style={cell.ran ? `background: color-mix(in srgb, var(--color-success) ${Math.round(35 + cell.intensity * 65)}%, transparent)` : ''} title="{new Date(cell.date).toLocaleDateString()} — {cell.ran ? 'backed up' : 'no backup'}"></div>
        {/each}
      </div>
      <p class="text-[11px] text-text-dim mt-2">Shaded = a backup ran that day (darker = larger)</p>
    {:else}
      <p class="text-xs text-text-dim py-6 text-center">{trendLoading ? 'Loading…' : 'No history yet'}</p>
    {/if}
  </div>
{/snippet}

{#snippet tSavings()}
  {#if dedupSummary}
    <div class="bg-surface-2 border border-border rounded-xl p-4 h-full min-h-[108px] flex flex-col justify-center cursor-pointer hover:border-vault/40 transition-colors" role="button" tabindex="0" onclick={() => navigate('/storage')} onkeydown={(e) => { if (e.key === 'Enter') navigate('/storage') }}>
      <p class="text-xs text-text-muted">Dedup &amp; compression</p>
      <p class="text-2xl font-bold text-success mt-1">{dedupSummary.ratio.toFixed(1)}×</p>
      <p class="text-[11px] text-text-dim mt-1.5">{formatBytes(dedupSummary.logical)} logical → {formatBytes(dedupSummary.physical)} stored</p>
    </div>
  {:else}
    <div class="bg-surface-2 border border-border rounded-xl p-4 h-full min-h-[108px] flex flex-col justify-center">
      <p class="text-xs text-text-muted">Dedup &amp; compression</p>
      <p class="text-[11px] text-text-dim mt-2">{dedupLoading ? 'Loading…' : 'No deduplicated destination yet'}</p>
    </div>
  {/if}
{/snippet}

{#snippet tForecast()}
  {#if forecastSummary}
    <div class="bg-surface-2 border border-border rounded-xl p-4 h-full min-h-[108px] flex flex-col justify-center cursor-pointer hover:border-vault/40 transition-colors" role="button" tabindex="0" onclick={() => navigate('/storage')} onkeydown={(e) => { if (e.key === 'Enter') navigate('/storage') }}>
      <p class="text-xs text-text-muted">Storage forecast</p>
      <p class="text-xl font-bold text-text mt-1">~{forecastSummary.days} days</p>
      <p class="text-[11px] text-text-dim mt-1.5">until {forecastSummary.name} is full</p>
      <p class="text-[11px] text-warning mt-0.5 font-medium">+{formatBytes(forecastSummary.perDay)}/day</p>
    </div>
  {:else}
    <div class="bg-surface-2 border border-border rounded-xl p-4 h-full min-h-[108px] flex flex-col justify-center">
      <p class="text-xs text-text-muted">Storage forecast</p>
      <p class="text-[11px] text-text-dim mt-2">{forecastLoading ? 'Loading…' : 'Not filling / not enough samples'}</p>
    </div>
  {/if}
{/snippet}

{#snippet tLargest()}
  <div class="bg-surface-2 border border-border rounded-xl p-5 h-full">
    <h2 class="text-sm font-semibold text-text mb-3">Largest backups</h2>
    {#if largestBackups.length}
      {@const max = largestBackups[0].size}
      <div class="flex flex-col gap-2.5">
        {#each largestBackups as b (b.name)}
          <div>
            <div class="flex justify-between text-xs mb-1"><span class="text-text truncate">{b.name}</span><span class="text-text-dim shrink-0 ml-2">{formatBytes(b.size)}</span></div>
            <div class="h-1.5 bg-surface-4 rounded-full overflow-hidden"><div class="h-full bg-vault" style="width: {Math.round(b.size / max * 100)}%"></div></div>
          </div>
        {/each}
      </div>
    {:else}
      <p class="text-xs text-text-dim py-4 text-center">No sized backups yet</p>
    {/if}
  </div>
{/snippet}

{#snippet tileBody(id)}
  {#if id === 'health'}{@render tHealth()}
  {:else if id === 'protected'}{@render tProtected()}
  {:else if id === 'nextrun'}{@render tNextRun()}
  {:else if id === 'lastbackup'}{@render tLastBackup()}
  {:else if id === 'threetwoone'}{@render tThreeTwoOne()}
  {:else if id === 'progress'}{@render tProgress()}
  {:else if id === 'activity'}{@render tActivity()}
  {:else if id === 'jobs'}{@render tJobs()}
  {:else if id === 'protection'}{@render tProtection()}
  {:else if id === 'storageCombined'}{@render tStorageCombined()}
  {:else if id === 'storagePerTarget'}{@render tStoragePerTarget()}
  {:else if id === 'recovery'}{@render tRecovery()}
  {:else if id === 'attention'}{@render tAttention()}
  {:else if id === 'successrate'}{@render tSuccessRate()}
  {:else if id === 'anomalies'}{@render tAnomalies()}
  {:else if id === 'quickactions'}{@render tQuickActions()}
  {:else if id === 'sizeTrend'}{@render tSizeTrend()}
  {:else if id === 'calendar'}{@render tCalendar()}
  {:else if id === 'savings'}{@render tSavings()}
  {:else if id === 'forecast'}{@render tForecast()}
  {:else if id === 'largest'}{@render tLargest()}
  {/if}
{/snippet}

<PullToRefresh onrefresh={loadDashboard}>
<div>
  <div class="dash-page-head flex items-start justify-between gap-4 mb-6 flex-wrap">
    <div>
      <h1 class="text-2xl font-bold text-text">Dashboard</h1>
      <div class="flex items-center gap-2 mt-1 flex-wrap">
        <p class="text-sm text-text-muted">{layout.editMode ? 'Arrange your dashboard — drag, add, or remove tiles' : 'Overview of your backup system'}</p>
        {#if health}
          <span class="inline-flex items-center gap-1.5 text-xs text-text-dim">
            <span class="w-1.5 h-1.5 rounded-full {health.status === 'ok' ? 'bg-success' : 'bg-danger'}"></span>
            {health.status === 'ok' ? 'Online' : 'Offline'} · v{health.version || '?'}
          </span>
        {/if}
      </div>
    </div>
    {#if !loading && !error && (storage.length > 0 || jobs.length > 0)}
      <div class="dash-head-actions flex gap-2 w-full sm:w-auto">
        {#if layout.editMode}
          <button onclick={layout.reset} class="flex-1 sm:flex-none min-h-[44px] px-4 py-2 text-sm font-medium text-text-muted hover:text-text bg-surface-2 border border-border hover:bg-surface-3 rounded-lg transition-colors">Reset</button>
          <button onclick={layout.toggleEdit} class="flex-1 sm:flex-none min-h-[44px] inline-flex items-center justify-center gap-1.5 px-4 py-2 text-sm font-semibold text-white bg-success hover:bg-success/90 rounded-lg transition-colors">
            <svg aria-hidden="true" class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"/></svg>Done
          </button>
        {:else}
          <button onclick={layout.toggleEdit} class="flex-1 sm:flex-none min-h-[44px] inline-flex items-center justify-center gap-1.5 px-4 py-2 text-sm font-medium text-text-muted hover:text-text bg-surface-2 border border-border hover:bg-surface-3 rounded-lg transition-colors">
            <svg aria-hidden="true" class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>Customize
          </button>
        {/if}
      </div>
    {/if}
  </div>

  {#if loading}
    <Skeleton variant="stats" />
    <Skeleton variant="card" count={3} />
  {:else if error}
    <div class="bg-danger/10 border border-danger/30 text-danger rounded-xl p-4 flex items-center gap-3">
      <svg aria-hidden="true" class="w-5 h-5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>
      <span class="text-sm">{error}</span>
    </div>
  {:else if storage.length === 0 && jobs.length === 0}
    <Welcome onstart={() => navigate('/storage')} />
  {:else}
    <!-- Getting Started Guide (shown when no storage or no jobs) -->
    {#if storage.length === 0 || jobs.length === 0}
      <div class="bg-surface-2 border border-vault/30 rounded-xl p-6 mb-8">
        <div class="flex items-start gap-4">
          <div class="w-10 h-10 rounded-lg bg-vault/10 flex items-center justify-center shrink-0 mt-0.5">
            <svg aria-hidden="true" class="w-5 h-5 text-vault" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z"/></svg>
          </div>
          <div class="flex-1 min-w-0">
            <h2 class="text-base font-semibold text-text">Getting Started</h2>
            <p class="text-sm text-text-muted mt-1">Set up your backup system in a few easy steps.</p>
            <div class="mt-4 flex flex-col sm:flex-row gap-3">
              <button onclick={() => navigate('/storage')} class="flex items-center gap-3 px-4 py-3 rounded-lg border transition-colors text-left {storage.length > 0 ? 'border-success/30 bg-success/5' : 'border-vault/40 bg-vault/5 hover:bg-vault/10'}">
                <div class="w-7 h-7 rounded-full flex items-center justify-center text-xs font-bold shrink-0 {storage.length > 0 ? 'bg-success text-white' : 'bg-vault text-white'}">
                  {#if storage.length > 0}<svg aria-hidden="true" class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7"/></svg>{:else}1{/if}
                </div>
                <div>
                  <p class="text-sm font-medium {storage.length > 0 ? 'text-success' : 'text-text'}">Configure Storage</p>
                  <p class="text-xs {storage.length > 0 ? 'text-success/70' : 'text-text-dim'}">{storage.length > 0 ? `${storage.length} destination${storage.length !== 1 ? 's' : ''} configured` : 'Set up where backups are stored'}</p>
                </div>
              </button>
              <button onclick={() => navigate('/jobs')} disabled={storage.length === 0} class="flex items-center gap-3 px-4 py-3 rounded-lg border transition-colors text-left {jobs.length > 0 ? 'border-success/30 bg-success/5' : storage.length > 0 ? 'border-vault/40 bg-vault/5 hover:bg-vault/10' : 'border-border bg-surface-3 opacity-50 cursor-not-allowed'}">
                <div class="w-7 h-7 rounded-full flex items-center justify-center text-xs font-bold shrink-0 {jobs.length > 0 ? 'bg-success text-white' : storage.length > 0 ? 'bg-vault text-white' : 'bg-surface-4 text-text-dim'}">
                  {#if jobs.length > 0}<svg aria-hidden="true" class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7"/></svg>{:else}2{/if}
                </div>
                <div>
                  <p class="text-sm font-medium {jobs.length > 0 ? 'text-success' : storage.length > 0 ? 'text-text' : 'text-text-dim'}">Create Backup Job</p>
                  <p class="text-xs {jobs.length > 0 ? 'text-success/70' : 'text-text-dim'}">{jobs.length > 0 ? `${jobs.length} job${jobs.length !== 1 ? 's' : ''} configured` : 'Choose what to back up and when'}</p>
                </div>
              </button>
              <div class="flex items-center gap-3 px-4 py-3 rounded-lg border transition-colors text-left {jobs.length > 0 && storage.length > 0 ? 'border-vault/40 bg-vault/5' : 'border-border bg-surface-3 opacity-50'}">
                <div class="w-7 h-7 rounded-full flex items-center justify-center text-xs font-bold shrink-0 {jobs.length > 0 && storage.length > 0 ? 'bg-vault text-white' : 'bg-surface-4 text-text-dim'}">3</div>
                <div>
                  <p class="text-sm font-medium {jobs.length > 0 && storage.length > 0 ? 'text-text' : 'text-text-dim'}">Run Backups</p>
                  <p class="text-xs text-text-dim">Jobs run on schedule or on demand</p>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    {/if}

    <!-- Edit-mode banner -->
    {#if layout.editMode}
      <div class="flex items-center gap-2.5 bg-info/10 border border-info/30 rounded-xl px-4 py-3 mb-4 text-sm text-info">
        <svg aria-hidden="true" class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 8h16M4 8a2 2 0 01-2-2V5a2 2 0 012-2h16a2 2 0 012 2v1a2 2 0 01-2 2M4 8v10a2 2 0 002 2h12a2 2 0 002-2V8M9 12h6"/></svg>
        <span><strong>Customize mode.</strong> Drag a tile by its handle to reorder (or use ↑ ↓) · remove with × · add more from the panel.</span>
      </div>
    {/if}

    <!-- Tile grid + add-tiles panel -->
    <div class="dash-layout-row flex gap-4 items-start">
      <div class="dash-tile-grid flex-1 min-w-0 {layout.editMode ? 'is-edit' : ''}" role="list">
        {#each visibleTiles as t (t.id)}
          <div
            role="listitem"
            class="dash-tile relative min-w-0 {dragIdx === t.idx ? 'is-dragging' : ''} {overIdx === t.idx && dragIdx >= 0 && dragIdx !== t.idx ? 'is-dragover' : ''}"
            style="grid-column: span {t.span};"
            data-span={t.span}
            draggable={layout.editMode}
            ondragstart={(e) => onDragStart(e, t.idx)}
            ondragover={(e) => onDragOver(e, t.idx)}
            ondrop={(e) => onDrop(e, t.idx)}
            ondragend={onDragEnd}
          >
            {#if layout.editMode}
              <div class="flex items-center gap-2 mb-1.5 px-1">
                <svg aria-hidden="true" class="w-4 h-4 text-text-dim cursor-grab shrink-0" fill="currentColor" viewBox="0 0 24 24"><circle cx="8" cy="6" r="1.4"/><circle cx="8" cy="12" r="1.4"/><circle cx="8" cy="18" r="1.4"/><circle cx="16" cy="6" r="1.4"/><circle cx="16" cy="12" r="1.4"/><circle cx="16" cy="18" r="1.4"/></svg>
                <span class="text-[11px] font-medium text-text-muted truncate">{t.name}</span>
                <div class="ml-auto flex items-center gap-1 shrink-0">
                  <button onclick={() => layout.moveBy(t.id, -1)} disabled={t.idx === 0} class="p-1 rounded text-text-muted hover:text-text hover:bg-surface-3 disabled:opacity-30 disabled:cursor-not-allowed" aria-label="Move {t.name} up" title="Move up">
                    <svg aria-hidden="true" class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M5 15l7-7 7 7"/></svg>
                  </button>
                  <button onclick={() => layout.moveBy(t.id, 1)} disabled={t.idx === layout.order.length - 1} class="p-1 rounded text-text-muted hover:text-text hover:bg-surface-3 disabled:opacity-30 disabled:cursor-not-allowed" aria-label="Move {t.name} down" title="Move down">
                    <svg aria-hidden="true" class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7"/></svg>
                  </button>
                  <button onclick={() => layout.remove(t.id)} class="p-1 rounded text-danger hover:bg-danger/10" aria-label="Remove {t.name}" title="Remove tile">
                    <svg aria-hidden="true" class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12"/></svg>
                  </button>
                </div>
              </div>
            {/if}
            {@render tileBody(t.id)}
          </div>
        {/each}
      </div>

      {#if layout.editMode}
        <aside class="dash-addpanel shrink-0 bg-surface-2 border border-border rounded-xl p-4">
          <div class="text-sm font-bold text-text">Add tiles</div>
          <div class="text-xs text-text-muted mb-3">Click to add to your dashboard</div>
          <div class="dash-cat-list flex flex-col gap-2">
            {#each catalogList as c (c.id)}
              <button onclick={() => layout.add(c.id)} disabled={c.shown}
                class="flex items-center justify-between gap-2 w-full px-3 py-2 rounded-lg border text-left transition-colors {c.shown ? 'border-border bg-surface-3 cursor-default' : 'border-border bg-surface hover:border-vault/40'}">
                <div class="flex items-center gap-2.5 min-w-0">
                  <span class="w-7 h-7 rounded-lg bg-vault/10 text-vault-text flex items-center justify-center shrink-0 text-sm">{c.glyph}</span>
                  <span class="text-xs font-medium text-text truncate">{c.name}</span>
                </div>
                <span class="shrink-0 text-[11px] font-semibold px-2 py-0.5 rounded-full {c.shown ? 'bg-success/15 text-success' : 'bg-vault/15 text-vault-text'}">{c.shown ? 'Added' : '+ Add'}</span>
              </button>
            {/each}
          </div>
        </aside>
      {/if}
    </div>
  {/if}
</div>
</PullToRefresh>

<style>
  :global(.dash-tile-grid) {
    display: grid;
    grid-template-columns: repeat(12, minmax(0, 1fr));
    gap: 14px;
    align-content: start;
  }
  :global(.dash-tile.is-dragging) { opacity: 0.4; }
  :global(.dash-tile.is-dragover) { outline: 2px solid var(--color-info); outline-offset: 2px; border-radius: 14px; }
  :global(.dash-tile-grid.is-edit .dash-tile) { cursor: grab; }

  /* Tablet: stack the add-tiles panel under the grid. */
  @media (max-width: 1024px) {
    :global(.dash-layout-row) { flex-direction: column; }
    :global(.dash-addpanel) { width: 100%; }
    :global(.dash-cat-list) { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  }

  /* Phone: 2-up small tiles, panels full width. */
  @media (max-width: 720px) {
    :global(.dash-tile-grid) { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
    :global(.dash-tile-grid > .dash-tile) { grid-column: span 2 !important; }
    :global(.dash-tile-grid > .dash-tile[data-span="3"]),
    :global(.dash-tile-grid > .dash-tile[data-span="4"]) { grid-column: span 1 !important; }
    :global(.dash-cat-list) { grid-template-columns: minmax(0, 1fr); }
  }

  /* Very small: single column. */
  @media (max-width: 400px) {
    :global(.dash-tile-grid) { grid-template-columns: minmax(0, 1fr); }
    :global(.dash-tile-grid > .dash-tile) { grid-column: span 1 !important; }
  }
</style>
