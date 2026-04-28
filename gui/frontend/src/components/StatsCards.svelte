<script lang="ts">
  export let data: string = ''

  interface StatItem {
    label: string
    value: string
    progress?: number
  }

  $: stats = parseStats(data)

  function parseNumber(val: string | undefined): number {
    if (!val) return 0
    const n = parseInt(val, 10)
    return isNaN(n) ? 0 : n
  }

  function formatBytes(bytes: number): string {
    if (bytes < 1024) return bytes + ' B'
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
    if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
    return (bytes / (1024 * 1024 * 1024)).toFixed(2) + ' GB'
  }

  function parseStats(raw: string): StatItem[] {
    if (!raw) return []
    try {
      const parsed = JSON.parse(raw)
      const items: Record<string, string> = {}

      for (const serverStats of Object.values(parsed) as Record<string, string>[]) {
        Object.assign(items, serverStats)
      }

      const bytes = parseNumber(items['bytes'])
      const maxBytes = parseNumber(items['limit_maxbytes'])
      const getHits = parseNumber(items['get_hits'])
      const getMisses = parseNumber(items['get_misses'])
      const totalGets = getHits + getMisses
      const hitRate = totalGets > 0 ? Math.round((getHits / totalGets) * 100) : 0
      const currConnections = parseNumber(items['curr_connections'])
      const totalItems = parseNumber(items['curr_items'])

      const memProgress = maxBytes > 0 ? Math.round((bytes / maxBytes) * 100) : 0

      return [
        { label: 'Memory', value: `${formatBytes(bytes)} / ${formatBytes(maxBytes)}`, progress: memProgress },
        { label: 'Hit Rate', value: `${hitRate}% (${getHits}/${totalGets})`, progress: hitRate },
        { label: 'Connections', value: String(currConnections) },
        { label: 'Items', value: String(totalItems) },
      ]
    } catch {
      return []
    }
  }
</script>

{#if stats.length > 0}
  <div class="stats-cards">
    {#each stats as stat}
      <div class="stat-card">
        <div class="stat-label">{stat.label}</div>
        <div class="stat-value">{stat.value}</div>
        {#if stat.progress !== undefined}
          <div class="progress-bar">
            <div class="progress-fill" style="width: {Math.min(stat.progress, 100)}%"></div>
          </div>
        {/if}
      </div>
    {/each}
  </div>
{/if}

<style>
  .stats-cards {
    display: flex;
    gap: 10px;
    padding: 12px 16px;
    overflow-x: auto;
  }
  .stat-card {
    min-width: 130px;
    flex: 1;
    padding: 12px 14px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 10px;
  }
  .stat-label {
    font-size: 11px;
    font-weight: 500;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.3px;
    margin-bottom: 4px;
  }
  .stat-value {
    font-size: 16px;
    font-weight: 600;
    font-family: var(--font-mono);
    color: var(--text-primary);
    margin-bottom: 8px;
  }
  .progress-bar {
    height: 4px;
    background: var(--bg-surface-soft);
    border-radius: 2px;
    overflow: hidden;
  }
  .progress-fill {
    height: 100%;
    background: var(--accent);
    border-radius: 2px;
    transition: width 0.3s ease-out;
  }
</style>
