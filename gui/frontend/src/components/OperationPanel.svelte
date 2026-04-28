<script lang="ts">
  import { connected, addLog, displayValue, displayMode, queryResult, activeOperationTab } from '../stores/app'
  import { keyHistory } from '../stores/keyHistory'
  import { Get, Set, Delete } from '../../wailsjs/go/service/OperationService.js'
  import ThemeToggle from './ThemeToggle.svelte'
  import KeyInput from './KeyInput.svelte'

  let activeTab: 'get' | 'set' | 'delete' = 'get'
  let getKey = ''
  let setKey = ''
  let setValue = ''
  let setFlags = 0
  let setExpiry = 0
  let deleteKey = ''

  $: activeOperationTab.set(activeTab)

  export function setTab(tab: 'get' | 'set' | 'delete') {
    activeTab = tab
  }

  export function executeCurrent() {
    if (activeTab === 'get') {
      handleGet()
    } else if (activeTab === 'set') {
      handleSet()
    } else if (activeTab === 'delete') {
      handleDelete()
    }
  }

  async function handleGet() {
    if (!getKey.trim()) return
    try {
      addLog({ op: 'GET', key: getKey, status: 'info', message: 'Fetching...' })
      const result = await Get(getKey)
      keyHistory.add(getKey)
      queryResult.set(result)
      if (result.success) {
        displayValue.set(result.data || result.value || '(empty value)')
        displayMode.set(result.valueKind === 'json' ? 'json' : 'text')
        addLog({ op: 'GET', key: getKey, status: 'success', message: 'OK' })
      } else {
        displayValue.set(result.error)
        displayMode.set('text')
        addLog({ op: 'GET', key: getKey, status: 'error', message: result.error })
      }
    } catch (e: any) {
      queryResult.set(null)
      addLog({ op: 'GET', key: getKey, status: 'error', message: e.message || String(e) })
    }
  }

  async function handleSet() {
    if (!setKey.trim()) return
    try {
      addLog({ op: 'SET', key: setKey, status: 'info', message: 'Storing...' })
      await Set(setKey, setValue, setFlags, setExpiry)
      keyHistory.add(setKey)
      addLog({ op: 'SET', key: setKey, status: 'success', message: 'OK' })
    } catch (e: any) {
      addLog({ op: 'SET', key: setKey, status: 'error', message: e.message || String(e) })
    }
  }

  async function handleDelete() {
    if (!deleteKey.trim()) return
    if (!confirm(`Delete key "${deleteKey}"?`)) return
    try {
      addLog({ op: 'DELETE', key: deleteKey, status: 'info', message: 'Deleting...' })
      await Delete(deleteKey)
      keyHistory.add(deleteKey)
      displayValue.set('')
      addLog({ op: 'DELETE', key: deleteKey, status: 'success', message: 'OK' })
    } catch (e: any) {
      addLog({ op: 'DELETE', key: deleteKey, status: 'error', message: e.message || String(e) })
    }
  }

  const tabs: Array<{ id: 'get' | 'set' | 'delete'; label: string }> = [
    { id: 'get', label: 'Get' },
    { id: 'set', label: 'Set' },
    { id: 'delete', label: 'Delete' },
  ]
</script>

<div class="panel">
  <div class="panel-head">
    <div class="tabs" role="tablist" aria-label="Memcached operations">
      {#each tabs as tab}
        <button
          type="button"
          class="tab"
          class:active={activeTab === tab.id}
          on:click={() => activeTab = tab.id}
          disabled={!$connected}
          role="tab"
          aria-selected={activeTab === tab.id}
        >
          {tab.label}
        </button>
      {/each}
    </div>
    <div class="panel-tools">
      <ThemeToggle />
    </div>
  </div>

  <div class="tab-content" aria-live="polite">
    {#if !$connected}
      <div class="disabled-overlay">Connect to a context first</div>
    {/if}

    {#if activeTab === 'get'}
      <div class="form-row">
        <KeyInput
          id="get-key"
          bind:value={getKey}
          placeholder="Key"
          disabled={!$connected}
        />
        <button type="button" on:click={handleGet} disabled={!$connected || !getKey.trim()}>Retrieve</button>
      </div>
    {:else if activeTab === 'set'}
      <div class="form-col">
        <KeyInput
          id="set-key"
          bind:value={setKey}
          placeholder="Key"
          disabled={!$connected}
        />
        <textarea
          id="set-value"
          bind:value={setValue}
          placeholder="Value"
          rows="3"
          disabled={!$connected}
          aria-label="Set value"
        ></textarea>
        <div class="form-row-inline">
          <div class="field">
            <label for="set-flags">Flags</label>
            <input id="set-flags" type="number" bind:value={setFlags} min="0" disabled={!$connected} />
          </div>
          <div class="field">
            <label for="set-expiry">Expiry (s)</label>
            <input id="set-expiry" type="number" bind:value={setExpiry} min="0" disabled={!$connected} />
          </div>
        </div>
        <button type="button" on:click={handleSet} disabled={!$connected || !setKey.trim()}>Store</button>
      </div>
    {:else if activeTab === 'delete'}
      <div class="form-row">
        <KeyInput
          id="delete-key"
          bind:value={deleteKey}
          placeholder="Key"
          disabled={!$connected}
        />
        <button type="button" class="btn-danger" on:click={handleDelete} disabled={!$connected || !deleteKey.trim()}>
          Delete
        </button>
      </div>
    {/if}
  </div>
</div>

<style>
  .panel {
    padding: 14px 16px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-primary);
  }
  .panel-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 14px;
  }
  .tabs {
    display: inline-flex;
    background: var(--bg-surface-soft);
    border-radius: 8px;
    padding: 2px;
    gap: 0;
  }
  .panel-tools {
    flex-shrink: 0;
  }
  .tab {
    padding: 5px 14px;
    border-radius: 6px;
    border: none;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 12px;
    font-weight: 500;
    transition: background 0.15s, color 0.15s;
  }
  .tab:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.08);
    color: var(--text-primary);
  }
  .tab.active {
    background: var(--accent);
    color: var(--accent-contrast);
  }
  .tab:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .tab:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .tab-content {
    position: relative;
  }
  .disabled-overlay {
    position: absolute;
    top: 0; left: 0; right: 0; bottom: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--text-dim);
    font-size: 13px;
    z-index: 10;
    pointer-events: none;
  }
  textarea {
    width: 100%;
    padding: 8px 12px;
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: 8px;
    color: var(--text-primary);
    font-size: 13px;
    font-family: var(--font-mono);
    box-sizing: border-box;
    transition: border-color 0.15s, box-shadow 0.15s;
    resize: vertical;
  }
  textarea:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  textarea:disabled { opacity: 0.5; }
  input[type="number"] {
    width: 100%;
    padding: 8px 12px;
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: 8px;
    color: var(--text-primary);
    font-size: 13px;
    box-sizing: border-box;
    transition: border-color 0.15s, box-shadow 0.15s;
  }
  input[type="number"]:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  .form-row {
    display: flex;
    gap: 8px;
    align-items: center;
  }
  .form-col {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .form-row-inline {
    display: flex;
    gap: 12px;
  }
  .field { flex: 1; }
  .field label {
    display: block;
    font-size: 12px;
    font-weight: 500;
    color: var(--text-muted);
    margin-bottom: 4px;
  }
  button {
    padding: 7px 16px;
    border-radius: 8px;
    border: none;
    background: var(--accent);
    color: var(--accent-contrast);
    cursor: pointer;
    font-size: 13px;
    font-weight: 500;
    transition: background 0.15s;
  }
  button:hover:not(:disabled) { background: var(--accent-hover); }
  button:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  button:disabled { opacity: 0.4; cursor: not-allowed; }
  .btn-danger { background: var(--danger); }
  .btn-danger:hover:not(:disabled) { background: var(--danger-hover); }

  @media (max-width: 980px) {
    .panel-head { flex-wrap: wrap; }
    .panel-tools { width: 100%; }
    .panel-tools :global(.theme-toggle) { width: 100%; justify-content: center; }
  }
  @media (prefers-reduced-motion: reduce) {
    .tab, textarea, input[type="number"], button { transition: none; }
  }
</style>
