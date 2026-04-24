<script lang="ts">
  import { logs } from '../stores/app'
</script>

<div class="log-panel" aria-live="polite">
  <div class="log-header">
    <span>Protocol Log</span>
    <button type="button" class="btn-clear" on:click={() => logs.set([])}>Clear</button>
  </div>
  <div class="log-list">
    {#if $logs.length === 0}
      <div class="empty">No operations yet</div>
    {:else}
      {#each $logs as entry}
        <div class="log-entry" class:success={entry.status === 'success'} class:error={entry.status === 'error'}>
          <span class="log-time">{entry.time}</span>
          <span class="log-op">{entry.op}</span>
          {#if entry.key}
            <span class="log-key">{entry.key}</span>
          {/if}
          <pre class="log-msg">{entry.message}</pre>
        </div>
      {/each}
    {/if}
  </div>
</div>

<style>
  .log-panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
    background: var(--bg-surface);
  }
  .log-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 12px;
    border-bottom: 1px solid var(--border);
    font-size: 13px;
    font-weight: 500;
    color: var(--text-secondary);
  }
  .btn-clear {
    padding: 2px 8px;
    border-radius: 4px;
    border: none;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 12px;
    transition: background 0.2s ease-out, color 0.2s ease-out;
  }
  .btn-clear:hover {
    color: var(--text-secondary);
    background: var(--bg-hover);
  }
  .btn-clear:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .log-list {
    flex: 1;
    overflow-y: auto;
    padding: 4px 0;
    font-family: 'SF Mono', 'Monaco', 'Menlo', 'Consolas', monospace;
    font-size: 12px;
  }
  .empty {
    text-align: center;
    color: var(--text-dim);
    padding: 24px 0;
  }
  .log-entry {
    display: flex;
    gap: 8px;
    padding: 4px 12px;
    color: var(--text-secondary);
    align-items: flex-start;
  }
  .log-entry:hover {
    background: var(--bg-hover);
  }
  .log-entry.success .log-op { color: var(--success); }
  .log-entry.error .log-op { color: var(--danger); }
  .log-time {
    color: var(--text-dim);
    flex-shrink: 0;
    padding-top: 1px;
  }
  .log-op {
    font-weight: 600;
    color: var(--accent);
    flex-shrink: 0;
    min-width: 60px;
    padding-top: 1px;
  }
  .log-key {
    color: var(--warning);
    flex-shrink: 0;
    padding-top: 1px;
  }
  .log-msg {
    margin: 0;
    color: var(--text-secondary);
    white-space: pre-wrap;
    word-break: break-word;
    overflow: visible;
    text-overflow: clip;
    flex: 1;
  }

  @media (prefers-reduced-motion: reduce) {
    .btn-clear {
      transition: none;
    }
  }
</style>

