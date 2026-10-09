<script lang="ts">
  import { queryResult, displayMode, displayValue, activeOperationTab } from '../stores/app'
  import JsonTree from './JsonTree.svelte'

  let copied = false
  let metaExpanded = false
  let searchQuery = ''
  let forceExpanded = false
  let forceCollapsed = false
  let showCopyMenu = false

  function handleCopyRaw() {
    copyToClipboard(rawValue)
    showCopyMenu = false
  }

  function handleCopyFormatted() {
    if (parsedJson !== null) {
      copyToClipboard(JSON.stringify(parsedJson, null, 2))
    }
    showCopyMenu = false
  }

  function copyToClipboard(text: string) {
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

  function toggleExpandAll() {
    if (forceExpanded) {
      forceExpanded = false
      forceCollapsed = true
    } else {
      forceExpanded = true
      forceCollapsed = false
    }
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

  function handleCopyBlur() {
    setTimeout(() => { showCopyMenu = false }, 150)
  }
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
        </div>
      </div>
      <div class="header-right">
        {#if effectiveMode === 'json' && hasContent}
          <button type="button" class="btn-tool" on:click={toggleExpandAll} title={forceExpanded ? 'Collapse all' : 'Expand all'}>
            {forceExpanded ? 'Collapse' : 'Expand'}
          </button>
          <input
            type="text"
            class="search-input"
            bind:value={searchQuery}
            placeholder="Search..."
            aria-label="Search JSON"
          />
        {/if}
        <div class="copy-wrapper">
          <button type="button" class="btn-copy" on:click={() => showCopyMenu = !showCopyMenu} disabled={!hasContent}>
            {copied ? 'Copied!' : 'Copy'}
          </button>
          {#if showCopyMenu}
            <div class="copy-menu" role="menu" on:blur={handleCopyBlur}>
              <button type="button" role="menuitem" on:click={handleCopyRaw}>Raw Value</button>
              <button type="button" role="menuitem" on:click={handleCopyFormatted} disabled={!isJson}>Formatted JSON</button>
            </div>
          {/if}
        </div>
      </div>
    </div>
    <div class="value-content" aria-live="polite">
      {#if !hasContent}
        <div class="empty">No data to display</div>
      {:else if effectiveMode === 'json' && parsedJson !== null}
        <div class="json-tree"><JsonTree data={parsedJson} depth={0} expanded={true} {searchQuery} {forceExpanded} {forceCollapsed} /></div>
      {:else}
        <pre>{rawValue}</pre>
      {/if}
    </div>
  </div>
{/if}

<style>
  .meta-summary {
    border-bottom: 1px solid var(--border);
    background: var(--bg-primary);
  }
  .meta-summary-toggle {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 16px;
    border: none;
    background: transparent;
    color: var(--text-muted);
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.3px;
    cursor: pointer;
    transition: background 0.15s, color 0.15s;
  }
  .meta-summary-toggle:hover {
    background: var(--bg-hover);
    color: var(--text-secondary);
  }
  .meta-summary-toggle:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .meta-toggle-icon {
    width: 0;
    height: 0;
    border-top: 4px solid transparent;
    border-bottom: 4px solid transparent;
    border-left: 6px solid currentColor;
    transform: rotate(0deg);
    transform-origin: 35% 50%;
    transition: transform 0.15s;
  }
  .meta-summary-toggle.expanded .meta-toggle-icon { transform: rotate(90deg); }
  .meta-summary-body { padding: 0 16px 12px; }

  .value-panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
    background: var(--bg-primary);
  }
  .value-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 16px;
    border-bottom: 1px solid var(--border);
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.3px;
    color: var(--text-muted);
    gap: 8px;
    flex-wrap: wrap;
  }
  .header-left {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-shrink: 0;
  }
  .header-right {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .mode-toggle {
    display: inline-flex;
    background: var(--bg-surface-soft);
    border-radius: 6px;
    padding: 2px;
    gap: 0;
  }
  .mode-btn {
    padding: 3px 10px;
    border: none;
    background: transparent;
    color: var(--text-dim);
    cursor: pointer;
    font-size: 11px;
    font-weight: 500;
    border-radius: 4px;
    transition: background 0.15s, color 0.15s;
  }
  .mode-btn:hover {
    background: rgba(255, 255, 255, 0.08);
    color: var(--text-primary);
  }
  .mode-btn.active {
    background: var(--accent);
    color: var(--accent-contrast);
  }
  .mode-btn:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  .mode-btn:disabled { opacity: 0.4; cursor: not-allowed; }
  .btn-tool {
    padding: 3px 8px;
    border-radius: 5px;
    border: 1px solid var(--border);
    background: transparent;
    color: var(--text-dim);
    cursor: pointer;
    font-size: 11px;
    font-weight: 500;
    white-space: nowrap;
  }
  .btn-tool:hover { color: var(--text-secondary); background: var(--bg-hover); }
  .search-input {
    width: 120px;
    padding: 3px 8px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-input);
    color: var(--text-primary);
    font-size: 11px;
    font-family: var(--font-mono);
  }
  .search-input:focus { outline: none; border-color: var(--accent); box-shadow: 0 0 0 2px var(--accent-soft); }
  .copy-wrapper { position: relative; }
  .btn-copy {
    padding: 3px 10px;
    border-radius: 5px;
    border: 1px solid var(--border);
    background: transparent;
    color: var(--text-dim);
    cursor: pointer;
    font-size: 11px;
    font-weight: 500;
    transition: background 0.15s, color 0.15s;
  }
  .btn-copy:hover:not(:disabled) { color: var(--text-secondary); background: var(--bg-hover); }
  .btn-copy:disabled { opacity: 0.4; cursor: not-allowed; }
  .copy-menu {
    position: absolute;
    top: 100%;
    right: 0;
    margin-top: 4px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.3);
    z-index: 50;
    min-width: 140px;
    padding: 4px;
  }
  .copy-menu button {
    display: block;
    width: 100%;
    padding: 6px 10px;
    border: none;
    background: transparent;
    color: var(--text-primary);
    cursor: pointer;
    font-size: 12px;
    text-align: left;
    border-radius: 6px;
  }
  .copy-menu button:hover { background: var(--bg-hover); }
  .copy-menu button:disabled { opacity: 0.4; cursor: not-allowed; }
  .value-content {
    flex: 1;
    overflow-y: auto;
    padding: 12px 16px;
    font-family: var(--font-mono);
    font-size: 12px;
    line-height: 1.6;
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
  .json-tree { line-height: 1.6; }

  @media (prefers-reduced-motion: reduce) {
    .meta-summary-toggle, .meta-toggle-icon, .mode-btn, .btn-copy, .btn-tool {
      transition: none;
    }
  }
</style>
