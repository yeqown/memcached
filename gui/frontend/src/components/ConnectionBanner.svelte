<script lang="ts">
  import { connectionStatus, connectionError, activeContextName } from '../stores/app'
</script>

{#if $connectionStatus === 'connected'}
  <div class="banner banner-ok">
    <span class="dot green"></span>
    Connected to {$activeContextName}
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
  }
  .banner-ok {
    background: var(--success-soft);
    color: var(--success);
  }
  .banner-error {
    flex-wrap: wrap;
    background: var(--danger-soft);
    border-bottom-color: var(--danger);
    color: var(--danger);
  }
  .banner-idle {
    color: var(--text-muted);
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
    font-family: 'SF Mono', 'Monaco', 'Menlo', 'Consolas', monospace;
    font-size: 12px;
    color: var(--danger);
    white-space: pre-wrap;
    word-break: break-word;
    width: 100%;
  }
</style>
