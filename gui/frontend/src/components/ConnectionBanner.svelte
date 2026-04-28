<script lang="ts">
  import { connectionStatus, connectionError, activeContextName, connected } from '../stores/app'
  import { Stats } from '../../wailsjs/go/service/OperationService.js'
  import { onDestroy } from 'svelte'

  let statsText = ''
  let statsInterval: ReturnType<typeof setInterval> | null = null

  async function refreshStats() {
    try {
      const result = await Stats()
      if (result && result.success && result.data) {
        const parsed = JSON.parse(result.data)
        const items: Record<string, string> = {}
        for (const serverStats of Object.values(parsed) as Record<string, string>[]) {
          Object.assign(items, serverStats)
        }
        const totalItems = items['curr_items'] || '0'
        const getHits = parseInt(items['get_hits'] || '0', 10)
        const getMisses = parseInt(items['get_misses'] || '0', 10)
        const total = getHits + getMisses
        const hitRate = total > 0 ? Math.round((getHits / total) * 100) : 0
        const bytes = parseInt(items['bytes'] || '0', 10)
        const maxBytes = parseInt(items['limit_maxbytes'] || '0', 10)
        function fmt(b: number): string {
          if (b < 1024) return b + 'B'
          if (b < 1024 * 1024) return (b / 1024).toFixed(0) + 'K'
          return (b / (1024 * 1024)).toFixed(0) + 'M'
        }
        statsText = `items: ${totalItems} | hits: ${hitRate}% | mem: ${fmt(bytes)}/${fmt(maxBytes)}`
      }
    } catch {
      // Stats refresh failed silently
    }
  }

  $: {
    if ($connected && $connectionStatus === 'connected') {
      refreshStats()
      if (statsInterval) clearInterval(statsInterval)
      statsInterval = setInterval(refreshStats, 30000)
    } else {
      statsText = ''
      if (statsInterval) {
        clearInterval(statsInterval)
        statsInterval = null
      }
    }
  }

  onDestroy(() => {
    if (statsInterval) clearInterval(statsInterval)
  })
</script>

{#if $connectionStatus === 'connected'}
  <div class="banner banner-ok">
    <span class="dot green"></span>
    <span class="banner-text">Connected to {$activeContextName}</span>
    {#if statsText}
      <span class="banner-stats">{statsText}</span>
    {/if}
  </div>
{:else if $connectionStatus === 'error'}
  <div class="banner banner-error">
    <span class="dot red"></span>
    <span>Connection failed</span>
    <pre class="error-text">{$connectionError}</pre>
  </div>
{:else}
  <div class="banner banner-idle">
    <span class="dot gray"></span>
    Disconnected
  </div>
{/if}

<style>
  .banner {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 16px;
    font-size: 13px;
    font-weight: 500;
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
    flex-wrap: wrap;
  }
  .banner-ok {
    background: var(--bg-primary);
    color: var(--success);
  }
  .banner-error {
    background: var(--bg-primary);
    border-bottom-color: var(--danger);
    color: var(--danger);
  }
  .banner-idle {
    background: var(--bg-primary);
    color: var(--text-muted);
  }
  .banner-text { flex-shrink: 0; }
  .banner-stats {
    margin-left: auto;
    font-size: 11px;
    font-family: var(--font-mono);
    color: var(--text-muted);
    opacity: 0.8;
  }
  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .dot.green { background: var(--success); }
  .dot.red { background: var(--danger); }
  .dot.gray { background: var(--text-dim); }
  .error-text {
    margin: 4px 0 0 16px;
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--danger);
    white-space: pre-wrap;
    word-break: break-word;
    width: 100%;
  }
</style>
