<script lang="ts">
  import { queryResult, displayMode, displayValue, activeOperationTab } from '../stores/app'
  import JsonTree from './JsonTree.svelte'

  let copied = false
  let metaExpanded = false

  function handleCopy() {
    const text = $queryResult
      ? $queryResult.value || $queryResult.data || ''
      : $displayValue
    navigator.clipboard.writeText(text)
    copied = true
    setTimeout(() => copied = false, 1500)
  }

  function tryParseJson(str: string): any | null {
    try {
      return JSON.parse(str)
    } catch {
      return null
    }
  }

  function toggleMetaExpanded() {
    metaExpanded = !metaExpanded
  }

  $: rawValue = $queryResult
    ? $queryResult.value || $queryResult.data || ''
    : $displayValue
  $: hasContent = rawValue.length > 0
  $: parsedJson = tryParseJson(rawValue)
  $: isJson = parsedJson !== null
  $: isMetaResult = $queryResult && ($queryResult.ttl !== undefined || $queryResult.cas !== undefined)
  $: showValuePanel = $activeOperationTab !== 'set' && $activeOperationTab !== 'delete'
  $: showMetaSummary = $activeOperationTab === 'get' && isMetaResult
  $: effectiveMode = (() => {
    if ($displayMode === 'json' && isJson) return 'json'
    if ($displayMode === 'meta' && isMetaResult) return 'meta'
    return 'text'
  })()

  $: metaFields = $queryResult ? [
    { label: 'Key', value: $queryResult.key },
    { label: 'TTL', value: $queryResult.ttl },
    { label: 'Last Access', value: $queryResult.lastAccessedTime },
    { label: 'CAS', value: $queryResult.cas },
    { label: 'Flags', value: $queryResult.flags },
    { label: 'Size', value: $queryResult.size },
    { label: 'Hit Before', value: $queryResult.hitBefore != null ? String($queryResult.hitBefore) : undefined },
    { label: 'Opaque', value: $queryResult.opaque },
  ].filter(f => f.value !== undefined && f.value !== null) : []
</script>

{#if showMetaSummary}
  <div class="meta-summary">
    <button
      type="button"
      class="meta-summary-toggle"
      class:expanded={metaExpanded}
      on:click={toggleMetaExpanded}
      aria-expanded={metaExpanded}
      aria-controls="get-meta-panel"
    >
      <span class="meta-toggle-icon" aria-hidden="true"></span>
      <span>Meta</span>
    </button>

    {#if metaExpanded}
      <div id="get-meta-panel" class="meta-summary-body" aria-live="polite">
        <table class="meta-table">
          {#each metaFields as field}
            <tr>
              <td class="meta-label">{field.label}</td>
              <td class="meta-value">{field.value}</td>
            </tr>
          {/each}
        </table>
      </div>
    {/if}
  </div>
{/if}

{#if showValuePanel}
  <div class="value-panel">
    <div class="value-header">
      <div class="header-left">
        <span>Value</span>
        <div class="mode-toggle" role="tablist" aria-label="Value display mode">
          <button type="button" class="mode-btn" class:active={effectiveMode === 'text'} on:click={() => displayMode.set('text')} role="tab" aria-selected={effectiveMode === 'text'}>Text</button>
          <button type="button" class="mode-btn" class:active={effectiveMode === 'json'} on:click={() => displayMode.set('json')} disabled={!isJson} role="tab" aria-selected={effectiveMode === 'json'}>JSON</button>
          {#if isMetaResult}
            <button type="button" class="mode-btn" class:active={effectiveMode === 'meta'} on:click={() => displayMode.set('meta')} role="tab" aria-selected={effectiveMode === 'meta'}>Meta</button>
          {/if}
        </div>
      </div>
      <button type="button" class="btn-copy" on:click={handleCopy} disabled={!hasContent}>
        {copied ? 'Copied!' : 'Copy'}
      </button>
    </div>
    <div class="value-content" aria-live="polite">
      {#if !hasContent}
        <div class="empty">No data to display</div>
      {:else if effectiveMode === 'json' && parsedJson !== null}
        <div class="json-tree"><JsonTree data={parsedJson} depth={0} expanded={true} /></div>
      {:else if effectiveMode === 'meta' && metaFields.length > 0}
        <table class="meta-table">
          {#each metaFields as field}
            <tr>
              <td class="meta-label">{field.label}</td>
              <td class="meta-value">{field.value}</td>
            </tr>
          {/each}
        </table>
        <div class="meta-raw">
          <div class="meta-raw-label">Value</div>
          <pre>{rawValue}</pre>
        </div>
      {:else}
        <pre>{rawValue}</pre>
      {/if}
    </div>
  </div>
{/if}

<style>
  .meta-summary {
    border-bottom: 1px solid var(--border);
    background: var(--bg-surface);
  }
  .meta-summary-toggle {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    border: none;
    background: transparent;
    color: var(--text-secondary);
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.2s ease-out, color 0.2s ease-out;
  }
  .meta-summary-toggle:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }
  .meta-summary-toggle:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .meta-toggle-icon {
    width: 0;
    height: 0;
    border-top: 5px solid transparent;
    border-bottom: 5px solid transparent;
    border-left: 7px solid currentColor;
    transform: rotate(0deg);
    transform-origin: 35% 50%;
    transition: transform 0.15s ease-out;
  }
  .meta-summary-toggle.expanded .meta-toggle-icon {
    transform: rotate(90deg);
  }
  .meta-summary-body {
    padding: 0 12px 12px;
  }

  .value-panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
    background: var(--bg-surface);
  }
  .value-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 12px;
    border-bottom: 1px solid var(--border);
    font-size: 13px;
    font-weight: 500;
    color: var(--text-secondary);
  }
  .header-left {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .mode-toggle {
    display: flex;
    border: 1px solid var(--border-strong);
    border-radius: 4px;
    overflow: hidden;
  }
  .mode-btn {
    padding: 2px 10px;
    border: none;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 11px;
    font-weight: 500;
    transition: background 0.2s ease-out, color 0.2s ease-out;
  }
  .mode-btn.active {
    background: var(--bg-active);
    color: var(--text-primary);
  }
  .mode-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .mode-btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .btn-copy {
    padding: 2px 10px;
    border-radius: 4px;
    border: 1px solid var(--border-strong);
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 12px;
    transition: background 0.2s ease-out, color 0.2s ease-out;
  }
  .btn-copy:hover:not(:disabled) {
    color: var(--text-secondary);
    background: var(--bg-hover);
  }
  .btn-copy:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .btn-copy:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .value-content {
    flex: 1;
    overflow-y: auto;
    padding: 12px;
    font-family: 'SF Mono', 'Monaco', 'Menlo', 'Consolas', monospace;
    font-size: 13px;
    color: var(--text-code);
    white-space: pre-wrap;
    word-break: break-all;
  }
  .empty {
    text-align: center;
    color: var(--text-dim);
    padding: 24px 0;
  }
  pre {
    margin: 0;
    white-space: pre-wrap;
    word-break: break-all;
  }
  .json-tree {
    line-height: 1.6;
  }
  .meta-table {
    border-collapse: collapse;
    width: auto;
  }
  .meta-table td {
    padding: 4px 12px 4px 0;
    border-bottom: 1px solid var(--border);
    font-size: 13px;
  }
  .meta-label {
    color: var(--text-muted);
    font-weight: 500;
    white-space: nowrap;
  }
  .meta-value {
    color: var(--text-primary);
  }
  .meta-raw {
    margin-top: 12px;
    border-top: 1px solid var(--border);
    padding-top: 12px;
  }
  .meta-raw-label {
    font-size: 12px;
    color: var(--text-muted);
    margin-bottom: 4px;
  }

  @media (prefers-reduced-motion: reduce) {
    .meta-summary-toggle,
    .meta-toggle-icon,
    .mode-btn,
    .btn-copy {
      transition: none;
    }
  }
</style>
