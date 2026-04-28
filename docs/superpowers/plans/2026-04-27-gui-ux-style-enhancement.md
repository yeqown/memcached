# GUI UX & Style Enhancement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enhance the Memcached GUI with improved input efficiency, result display, operation switching, and visual style for a terminal-like developer experience.

**Architecture:** Svelte 3 frontend with Wails Go backend. Uses Svelte stores for state management, localStorage for persistence. Components are self-contained with scoped styles using CSS variables defined in `style.css`.

**Tech Stack:** Svelte 3, TypeScript, Vite, Wails, CSS Variables

---

## File Structure

| File | Purpose |
|------|---------|
| `gui/frontend/src/style.css` | CSS variables for theming (modify) |
| `gui/frontend/src/stores/app.ts` | Global state stores (modify) |
| `gui/frontend/src/stores/keyHistory.ts` | Key history store (create) |
| `gui/frontend/src/lib/keyboard.ts` | Keyboard shortcut utilities (create) |
| `gui/frontend/src/lib/storage.ts` | localStorage helpers (create) |
| `gui/frontend/src/App.svelte` | Main layout, global keydown listener (modify) |
| `gui/frontend/src/components/KeyInput.svelte` | Input with auto-complete (create) |
| `gui/frontend/src/components/OperationPanel.svelte` | Operation tabs (modify) |
| `gui/frontend/src/components/ValueDisplay.svelte` | Result display with search (modify) |
| `gui/frontend/src/components/JsonTree.svelte` | JSON tree with expand/collapse all (modify) |
| `gui/frontend/src/components/ConnectionBanner.svelte` | Stats display (modify) |
| `gui/frontend/src/components/StatsCards.svelte` | Stats metric cards (create) |

---

## Task 1: Visual Style Updates

**Files:**
- Modify: `gui/frontend/src/style.css`

- [ ] **Step 1: Update dark theme CSS variables to terminal black**

Replace the dark theme `:root` block (lines 2-43) in `style.css`:

```css
:root,
:root[data-theme='dark'] {
  --bg-primary: #0a0e14;
  --bg-sidebar: #0d1117;
  --bg-surface: #0f1923;
  --bg-surface-soft: #1a2332;
  --bg-input: #0d1117;
  --bg-hover: #1a2332;
  --bg-active: #2a3545;
  --bg-overlay: rgba(0, 0, 0, 0.7);

  --border: #1a2332;
  --border-strong: #2a3545;

  --text-primary: #e2e8f0;
  --text-secondary: #94a3b8;
  --text-muted: #64748b;
  --text-dim: #475569;
  --text-code: #cbd5e1;

  --accent: #3b82f6;
  --accent-hover: #2563eb;
  --accent-contrast: #ffffff;
  --accent-focus-ring: rgba(59, 130, 246, 0.25);

  --danger: #dc2626;
  --danger-hover: #b91c1c;
  --danger-soft: rgba(239, 68, 68, 0.12);

  --success: #22c55e;
  --success-soft: rgba(34, 197, 94, 0.12);

  --warning: #f59e0b;

  --scrollbar-thumb: #2a3545;
  --scrollbar-hover: #3a4555;

  --font-mono: 'JetBrains Mono', 'SF Mono', 'Monaco', 'Menlo', 'Consolas', monospace;

  --json-key: #7dd3fc;
  --json-string: #86efac;
  --json-number: #fbbf24;
  --json-boolean: #c084fc;
}
```

- [ ] **Step 2: Commit visual style changes**

```bash
git add gui/frontend/src/style.css
git commit -m "style(gui): update dark theme to terminal black palette"
```

---

## Task 2: Storage Utilities

**Files:**
- Create: `gui/frontend/src/lib/storage.ts`

- [ ] **Step 1: Create localStorage helper module**

Create `gui/frontend/src/lib/storage.ts`:

```typescript
const PREFIX = 'memcached-gui-'

export function loadString(key: string): string | null {
  return localStorage.getItem(PREFIX + key)
}

export function saveString(key: string, value: string): void {
  localStorage.setItem(PREFIX + key, value)
}

export function loadJson<T>(key: string, fallback: T): T {
  const raw = localStorage.getItem(PREFIX + key)
  if (!raw) return fallback
  try {
    return JSON.parse(raw) as T
  } catch {
    return fallback
  }
}

export function saveJson<T>(key: string, value: T): void {
  localStorage.setItem(PREFIX + key, JSON.stringify(value))
}

export function remove(key: string): void {
  localStorage.removeItem(PREFIX + key)
}
```

- [ ] **Step 2: Commit storage utilities**

```bash
git add gui/frontend/src/lib/storage.ts
git commit -m "feat(gui): add localStorage helper utilities"
```

---

## Task 3: Key History Store

**Files:**
- Create: `gui/frontend/src/stores/keyHistory.ts`

- [ ] **Step 1: Create key history store with persistence**

Create `gui/frontend/src/stores/keyHistory.ts`:

```typescript
import { writable, get } from 'svelte/store'
import { loadJson, saveJson } from '../lib/storage'

const MAX_HISTORY = 50
const STORAGE_KEY = 'key-history'

function createKeyHistoryStore() {
  const initial = loadJson<string[]>(STORAGE_KEY, [])
  const { subscribe, update, set } = writable<string[]>(initial)

  function add(key: string) {
    if (!key.trim()) return
    update(list => {
      const filtered = list.filter(k => k !== key)
      const updated = [key, ...filtered].slice(0, MAX_HISTORY)
      saveJson(STORAGE_KEY, updated)
      return updated
    })
  }

  function clear() {
    set([])
    saveJson(STORAGE_KEY, [])
  }

  function filterByPrefix(prefix: string, limit = 5): string[] {
    if (!prefix) return []
    const list = get({ subscribe })
    return list
      .filter(k => k.toLowerCase().includes(prefix.toLowerCase()))
      .slice(0, limit)
  }

  return { subscribe, add, clear, filterByPrefix }
}

export const keyHistory = createKeyHistoryStore()
```

- [ ] **Step 2: Commit key history store**

```bash
git add gui/frontend/src/stores/keyHistory.ts
git commit -m "feat(gui): add key history store with localStorage persistence"
```

---

## Task 4: Keyboard Shortcut Utilities

**Files:**
- Create: `gui/frontend/src/lib/keyboard.ts`

- [ ] **Step 1: Create keyboard shortcut module**

Create `gui/frontend/src/lib/keyboard.ts`:

```typescript
export type KeyboardHandler = (e: KeyboardEvent) => void

const ACTIVE_INPUT_TAGS = new Set(['INPUT', 'TEXTAREA', 'SELECT'])

export function isInputFocused(): boolean {
  const el = document.activeElement
  if (!el) return false
  if (ACTIVE_INPUT_TAGS.has(el.tagName)) return true
  if (el.getAttribute('contenteditable') === 'true') return true
  return false
}

export function isCtrlOrCmd(e: KeyboardEvent): boolean {
  return e.ctrlKey || e.metaKey
}

export function matchesShortcut(e: KeyboardEvent, key: string): boolean {
  return isCtrlOrCmd(e) && e.key.toLowerCase() === key.toLowerCase()
}

export type OperationTab = 'get' | 'set' | 'delete' | 'stats'

export interface ShortcutConfig {
  onTabSwitch?: (tab: OperationTab) => void
  onExecute?: () => void
  onClear?: () => void
}

export function createShortcutHandler(config: ShortcutConfig): KeyboardHandler {
  return (e: KeyboardEvent) => {
    if (matchesShortcut(e, '1') && config.onTabSwitch) {
      e.preventDefault()
      config.onTabSwitch('get')
    } else if (matchesShortcut(e, '2') && config.onTabSwitch) {
      e.preventDefault()
      config.onTabSwitch('set')
    } else if (matchesShortcut(e, '3') && config.onTabSwitch) {
      e.preventDefault()
      config.onTabSwitch('delete')
    } else if (matchesShortcut(e, '4') && config.onTabSwitch) {
      e.preventDefault()
      config.onTabSwitch('stats')
    } else if (matchesShortcut(e, 'enter') && config.onExecute) {
      if (isInputFocused()) {
        e.preventDefault()
        config.onExecute()
      }
    } else if (matchesShortcut(e, 'l') && config.onClear) {
      if (!isInputFocused()) {
        e.preventDefault()
        config.onClear()
      }
    }
  }
}
```

- [ ] **Step 2: Commit keyboard utilities**

```bash
git add gui/frontend/src/lib/keyboard.ts
git commit -m "feat(gui): add keyboard shortcut utilities"
```

---

## Task 5: KeyInput Component with Auto-complete

**Files:**
- Create: `gui/frontend/src/components/KeyInput.svelte`

- [ ] **Step 1: Create KeyInput component**

Create `gui/frontend/src/components/KeyInput.svelte`:

```svelte
<script lang="ts">
  import { createEventDispatcher } from 'svelte'
  import { keyHistory } from '../stores/keyHistory'

  export let value = ''
  export let placeholder = 'Key'
  export let disabled = false
  export let id = ''

  const dispatch = createEventDispatcher()

  let showSuggestions = false
  let selectedIndex = 0
  let suggestions: string[] = []
  let inputEl: HTMLInputElement

  $: {
    if (value) {
      suggestions = keyHistory.filterByPrefix(value, 5)
      selectedIndex = 0
      showSuggestions = suggestions.length > 0 && value.length > 0
    } else {
      showSuggestions = false
      suggestions = []
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (!showSuggestions) return

    if (e.key === 'ArrowDown') {
      e.preventDefault()
      selectedIndex = Math.min(selectedIndex + 1, suggestions.length - 1)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      selectedIndex = Math.max(selectedIndex - 1, 0)
    } else if (e.key === 'Enter' || e.key === 'Tab') {
      e.preventDefault()
      value = suggestions[selectedIndex]
      showSuggestions = false
    } else if (e.key === 'Escape') {
      showSuggestions = false
    }
  }

  function handleBlur() {
    setTimeout(() => { showSuggestions = false }, 150)
  }

  export function focus() {
    inputEl.focus()
  }
</script>

<div class="key-input-wrapper">
  <input
    bind:this={inputEl}
    {id}
    type="text"
    bind:value
    {placeholder}
    {disabled}
    on:keydown={handleKeydown}
    on:blur={handleBlur}
    aria-label={placeholder}
    aria-autocomplete="list"
    aria-expanded={showSuggestions}
  />
  {#if showSuggestions}
    <ul class="suggestions" role="listbox">
      {#each suggestions as suggestion, i}
        <li
          role="option"
          class:selected={i === selectedIndex}
          on:mousedown|preventDefault={() => {
            value = suggestion
            showSuggestions = false
          }}
        >
          {suggestion}
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .key-input-wrapper {
    position: relative;
    flex: 1;
  }
  input {
    width: 100%;
    padding: 8px 12px;
    background: var(--bg-input);
    border: 1px solid var(--border-strong);
    border-radius: 6px;
    color: var(--text-primary);
    font-size: 14px;
    font-family: inherit;
    box-sizing: border-box;
    transition: border-color 0.2s ease-out, box-shadow 0.2s ease-out;
  }
  input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 2px var(--accent-focus-ring);
  }
  input:disabled { opacity: 0.5; }
  .suggestions {
    position: absolute;
    top: 100%;
    left: 0;
    right: 0;
    margin: 4px 0 0;
    padding: 4px 0;
    background: var(--bg-surface);
    border: 1px solid var(--border-strong);
    border-radius: 6px;
    list-style: none;
    z-index: 50;
    max-height: 200px;
    overflow-y: auto;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.3);
  }
  li {
    padding: 6px 12px;
    cursor: pointer;
    font-family: var(--font-mono);
    font-size: 13px;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  li:hover, li.selected {
    background: var(--bg-active);
    color: var(--accent);
  }
  @media (prefers-reduced-motion: reduce) {
    input { transition: none; }
  }
</style>
```

- [ ] **Step 2: Commit KeyInput component**

```bash
git add gui/frontend/src/components/KeyInput.svelte
git commit -m "feat(gui): add KeyInput component with auto-complete"
```

---

## Task 6: OperationPanel with Keyboard Shortcuts & Input State

**Files:**
- Modify: `gui/frontend/src/components/OperationPanel.svelte`

- [ ] **Step 1: Rewrite OperationPanel with KeyInput, key history tracking, and tab state preservation**

Replace `gui/frontend/src/components/OperationPanel.svelte` entirely. Key changes:
- Import `KeyInput` component instead of raw `<input>` for Get and Delete
- Import `keyHistory` and call `keyHistory.add(key)` after every Get/Set/Delete
- Export `executeCurrent` and `setTab` functions for App.svelte to call from keyboard handler
- Each tab's form state is already separate variables (getKey, setKey, etc.) — no change needed there
- Tab padding: `6px 16px` → `4px 12px`, panel padding: `16px` → `12px`
- Add monospace font to value textarea

```svelte
<script lang="ts">
  import { connected, addLog, displayValue, displayMode, queryResult, activeOperationTab } from '../stores/app'
  import { keyHistory } from '../stores/keyHistory'
  import { Get, Set, Delete, Stats } from '../../wailsjs/go/service/OperationService.js'
  import ThemeToggle from './ThemeToggle.svelte'
  import KeyInput from './KeyInput.svelte'

  let activeTab: 'get' | 'set' | 'delete' | 'stats' = 'get'
  let getKey = ''
  let setKey = ''
  let setValue = ''
  let setFlags = 0
  let setExpiry = 0
  let deleteKey = ''

  $: activeOperationTab.set(activeTab)

  // Expose for keyboard shortcuts
  export function setTab(tab: 'get' | 'set' | 'delete' | 'stats') {
    activeTab = tab
  }

  export function executeCurrent() {
    if (!activeTab || activeTab === 'stats') {
      handleStats()
    } else if (activeTab === 'get') {
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

  async function handleStats() {
    try {
      addLog({ op: 'STATS', status: 'info', message: 'Fetching stats...' })
      const result = await Stats()
      queryResult.set(result)
      if (result && result.success) {
        displayValue.set(result.data)
        displayMode.set('json')
        addLog({ op: 'STATS', status: 'success', message: 'OK' })
      } else if (result) {
        addLog({ op: 'STATS', status: 'error', message: result.error })
      }
    } catch (e: any) {
      queryResult.set(null)
      addLog({ op: 'STATS', status: 'error', message: e.message || String(e) })
    }
  }

  const tabs: Array<{ id: 'get' | 'set' | 'delete' | 'stats'; label: string }> = [
    { id: 'get', label: 'Get' },
    { id: 'set', label: 'Set' },
    { id: 'delete', label: 'Delete' },
    { id: 'stats', label: 'Stats' },
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
    {:else if activeTab === 'stats'}
      <button type="button" on:click={handleStats} disabled={!$connected}>Get Stats</button>
    {/if}
  </div>
</div>

<style>
  .panel {
    padding: 12px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-surface);
  }
  .panel-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 12px;
  }
  .tabs {
    display: flex;
    gap: 4px;
    min-width: 0;
  }
  .panel-tools {
    flex-shrink: 0;
  }
  .tab {
    padding: 4px 12px;
    border-radius: 6px;
    border: none;
    background: transparent;
    color: var(--text-secondary);
    cursor: pointer;
    font-size: 13px;
    font-weight: 500;
    transition: background 0.2s ease-out, color 0.2s ease-out;
  }
  .tab:hover:not(:disabled) {
    background: var(--bg-hover);
    color: var(--text-primary);
  }
  .tab.active {
    background: var(--bg-active);
    color: var(--accent);
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
    font-size: 14px;
    z-index: 10;
    pointer-events: none;
  }
  textarea {
    width: 100%;
    padding: 8px 12px;
    background: var(--bg-input);
    border: 1px solid var(--border-strong);
    border-radius: 6px;
    color: var(--text-primary);
    font-size: 14px;
    font-family: var(--font-mono);
    box-sizing: border-box;
    transition: border-color 0.2s ease-out, box-shadow 0.2s ease-out;
    resize: vertical;
  }
  textarea:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 2px var(--accent-focus-ring);
  }
  textarea:disabled { opacity: 0.5; }
  input[type="number"] {
    width: 100%;
    padding: 8px 12px;
    background: var(--bg-input);
    border: 1px solid var(--border-strong);
    border-radius: 6px;
    color: var(--text-primary);
    font-size: 14px;
    box-sizing: border-box;
    transition: border-color 0.2s ease-out, box-shadow 0.2s ease-out;
  }
  input[type="number"]:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 2px var(--accent-focus-ring);
  }
  .form-row {
    display: flex;
    gap: 6px;
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
    color: var(--text-muted);
    margin-bottom: 4px;
  }
  button {
    padding: 8px 20px;
    border-radius: 6px;
    border: none;
    background: var(--accent);
    color: var(--accent-contrast);
    cursor: pointer;
    font-size: 14px;
    font-weight: 500;
    transition: background 0.2s ease-out, filter 0.2s ease-out;
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
```

- [ ] **Step 2: Commit OperationPanel changes**

```bash
git add gui/frontend/src/components/OperationPanel.svelte
git commit -m "feat(gui): add KeyInput with auto-complete and key history to OperationPanel"
```

---

## Task 7: App.svelte with Keyboard Shortcuts & Tab Memory

**Files:**
- Modify: `gui/frontend/src/App.svelte`

- [ ] **Step 1: Add keyboard shortcut handler and tab memory to App.svelte**

Replace `gui/frontend/src/App.svelte`:

```svelte
<script lang="ts">
  import { onMount } from 'svelte'
  import Sidebar from './components/Sidebar.svelte'
  import OperationPanel from './components/OperationPanel.svelte'
  import ProtocolLog from './components/ProtocolLog.svelte'
  import ValueDisplay from './components/ValueDisplay.svelte'
  import ConnectionBanner from './components/ConnectionBanner.svelte'
  import { activeOperationTab, displayValue, displayMode, queryResult } from './stores/app'
  import { createShortcutHandler, type OperationTab } from './lib/keyboard'
  import { loadString, saveString } from './lib/storage'

  let operationPanelRef: OperationPanel

  function clearResult() {
    displayValue.set('')
    displayMode.set('text')
    queryResult.set(null)
  }

  const shortcutHandler = createShortcutHandler({
    onTabSwitch: (tab: OperationTab) => {
      operationPanelRef.setTab(tab)
    },
    onExecute: () => {
      operationPanelRef.executeCurrent()
    },
    onClear: () => {
      clearResult()
    },
  })

  function handleKeydown(e: KeyboardEvent) {
    shortcutHandler(e)
  }

  onMount(() => {
    window.addEventListener('keydown', handleKeydown)

    // Restore tab from localStorage
    const savedTab = loadString('active-tab') as OperationTab | null
    if (savedTab) {
      operationPanelRef.setTab(savedTab)
    }

    return () => {
      window.removeEventListener('keydown', handleKeydown)
    }
  })

  // Persist tab on change
  $: if ($activeOperationTab) {
    saveString('active-tab', $activeOperationTab)
  }
</script>

<div class="app-layout">
  <div class="main-content">
    <div class="left-column">
      <Sidebar />
    </div>
    <div class="right-column">
      <div class="right-top">
        <ConnectionBanner />
      </div>
      <div class="operation-panel-wrap">
        <OperationPanel bind:this={operationPanelRef} />
      </div>
      {#if $activeOperationTab !== 'set' && $activeOperationTab !== 'delete'}
        <div class="result-area">
          <ValueDisplay />
        </div>
      {/if}
    </div>
  </div>

  <div class="log-dock-full">
    <ProtocolLog />
  </div>
</div>

<style>
  .app-layout {
    display: flex;
    flex-direction: column;
    height: 100vh;
    overflow: hidden;
    background: var(--bg-primary);
    color: var(--text-primary);
  }
  .main-content {
    flex: 1;
    display: flex;
    min-height: 0;
    overflow: hidden;
  }
  .left-column {
    display: flex;
    flex-direction: column;
    width: 260px;
    min-width: 260px;
    border-right: 1px solid var(--border);
    overflow: hidden;
  }
  .right-column {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    min-width: 0;
  }
  .right-top {
    flex-shrink: 0;
    border-bottom: 1px solid var(--border);
    background: var(--bg-surface);
  }
  .right-top :global(.banner) {
    width: 100%;
    border-bottom: none;
  }
  .result-area {
    flex: 1;
    overflow: hidden;
    min-height: 0;
  }
  .log-dock-full {
    flex: 0 0 30%;
    min-height: 192px;
    max-height: 440px;
    border-top: 1px solid var(--border);
    background: var(--bg-surface);
    overflow: hidden;
  }
</style>
```

- [ ] **Step 2: Commit App.svelte changes**

```bash
git add gui/frontend/src/App.svelte
git commit -m "feat(gui): add keyboard shortcuts and tab memory to App"
```

---

## Task 8: ValueDisplay with JSON Search & Expand/Collapse All

**Files:**
- Modify: `gui/frontend/src/components/ValueDisplay.svelte`
- Modify: `gui/frontend/src/components/JsonTree.svelte`

- [ ] **Step 1: Update JsonTree to support search filter and global expand/collapse**

Replace `gui/frontend/src/components/JsonTree.svelte`:

```svelte
<script lang="ts">
  export let data: any
  export let expanded = true
  export let depth = 0
  export let searchQuery = ''
  export let forceExpanded = false
  export let forceCollapsed = false

  function typeOf(v: any): string {
    if (v === null) return 'null'
    if (Array.isArray(v)) return 'array'
    return typeof v
  }

  function toggle() {
    expanded = !expanded
  }

  function onToggleKeydown(event: KeyboardEvent) {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      toggle()
    }
  }

  $: kind = typeOf(data)
  $: isContainer = kind === 'object' || kind === 'array'
  $: entries = isContainer
    ? (kind === 'array' ? data.map((v: any, i: number) => [String(i), v]) : Object.entries(data))
    : []
  $: count = entries.length
  $: effectiveExpanded = forceExpanded ? true : (forceCollapsed ? false : expanded)
  $: matchesSearch = !searchQuery || nodeMatches(data, searchQuery, kind)
  $: childMatchesSearch = searchQuery && isContainer
    ? entries.some(([key, val]) => childNodeMatches(key, val, searchQuery))
    : false
  $: showChildren = effectiveExpanded || (searchQuery && childMatchesSearch)

  function nodeMatches(value: any, query: string, kind: string): boolean {
    if (kind === 'string') return String(value).toLowerCase().includes(query.toLowerCase())
    if (kind === 'number' || kind === 'boolean') return String(value).toLowerCase().includes(query.toLowerCase())
    return false
  }

  function childNodeMatches(key: string, value: any, query: string): boolean {
    if (key.toLowerCase().includes(query.toLowerCase())) return true
    const childKind = typeOf(value)
    if (childKind === 'object' || childKind === 'array') {
      const childEntries = childKind === 'array'
        ? value.map((v: any, i: number) => [String(i), v])
        : Object.entries(value)
      return childEntries.some(([k, v]) => childNodeMatches(k, v, query))
    }
    return nodeMatches(value, query, childKind)
  }
</script>

{#if !searchQuery || matchesSearch || childMatchesSearch}
  <span class="depth-pad" style="padding-left: {depth * 16}px"></span>

  {#if isContainer}
    <button type="button" class="toggle" on:click={toggle} on:keydown={onToggleKeydown} aria-label={effectiveExpanded ? 'Collapse node' : 'Expand node'}>
      <span class="toggle-icon" class:expanded={effectiveExpanded} aria-hidden="true"></span>
    </button>
    <span class="bracket">{kind === 'array' ? '[' : '{'}</span>
    {#if !showChildren}
      <button type="button" class="ellipsis" on:click={toggle} on:keydown={onToggleKeydown} aria-label="Expand node">
        &hellip;{count}&nbsp;{kind === 'array' ? 'items' : 'keys'}
      </button>
      <span class="bracket">{kind === 'array' ? ']' : '}'}</span>
    {:else}
      <div class="children">
        {#each entries as [key, val], i}
          {#if !searchQuery || String(key).toLowerCase().includes(searchQuery.toLowerCase()) || childNodeMatches(key, val, searchQuery)}
            <div class="entry">
              <span class="depth-pad" style="padding-left: {(depth + 1) * 16}px"></span>
              {#if kind === 'object'}
                <span class="json-key">&quot;{key}&quot;</span><span class="colon">: </span>
              {:else}
                <span class="json-index">{key}</span><span class="colon">: </span>
              {/if}
              <svelte:self data={val} depth={depth + 1} expanded={depth < 1} {searchQuery} {forceExpanded} {forceCollapsed} />
              {#if i < count - 1}<span class="comma">,</span>{/if}
            </div>
          {/if}
        {/each}
      </div>
      <span class="depth-pad" style="padding-left: {depth * 16}px"></span>
      <span class="bracket">{kind === 'array' ? ']' : '}'}</span>
    {/if}
  {:else if kind === 'string'}
    <span class="json-string">&quot;{data}&quot;</span>
  {:else if kind === 'number'}
    <span class="json-number">{data}</span>
  {:else if kind === 'boolean'}
    <span class="json-boolean">{String(data)}</span>
  {:else}
    <span class="json-null">null</span>
  {/if}
{/if}

<style>
  .depth-pad { display: inline; }
  .toggle {
    width: 22px;
    height: 22px;
    border: none;
    background: var(--bg-surface-soft);
    cursor: pointer;
    color: var(--text-secondary);
    margin-right: 6px;
    user-select: none;
    padding: 0;
    line-height: 1;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: 4px;
    transition: background 0.2s ease-out, color 0.2s ease-out;
  }
  .toggle:hover {
    background: var(--bg-active);
    color: var(--text-primary);
  }
  .toggle-icon {
    width: 0;
    height: 0;
    border-top: 5px solid transparent;
    border-bottom: 5px solid transparent;
    border-left: 7px solid currentColor;
    transform: rotate(0deg);
    transform-origin: 35% 50%;
    transition: transform 0.15s ease-out;
  }
  .toggle-icon.expanded { transform: rotate(90deg); }
  .toggle:focus-visible, .ellipsis:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
    border-radius: 2px;
  }
  .bracket { color: var(--text-secondary); }
  .ellipsis {
    border: none;
    background: transparent;
    cursor: pointer;
    color: var(--text-muted);
    font-size: 12px;
    margin: 0 2px;
    padding: 0;
  }
  .children { display: flex; flex-direction: column; }
  .entry {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
  }
  .colon { color: var(--text-secondary); }
  .comma { color: var(--text-muted); }
  .json-key { color: var(--json-key); }
  .json-index { color: var(--text-secondary); }
  .json-string { color: var(--json-string); }
  .json-number { color: var(--json-number); }
  .json-boolean { color: var(--json-boolean); }
  .json-null { color: var(--text-muted); font-style: italic; }
</style>
```

- [ ] **Step 2: Update ValueDisplay with search bar and copy options**

Replace `gui/frontend/src/components/ValueDisplay.svelte`:

```svelte
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
          {#if isMetaResult}
            <button type="button" class="mode-btn" class:active={effectiveMode === 'meta'} on:click={() => displayMode.set('meta')} role="tab" aria-selected={effectiveMode === 'meta'}>Meta</button>
          {/if}
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
  .meta-summary-toggle.expanded .meta-toggle-icon { transform: rotate(90deg); }
  .meta-summary-body { padding: 0 12px 12px; }

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
  .mode-btn:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  .mode-btn:disabled { opacity: 0.4; cursor: not-allowed; }
  .btn-tool {
    padding: 2px 8px;
    border-radius: 4px;
    border: 1px solid var(--border-strong);
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 11px;
    font-weight: 500;
    white-space: nowrap;
  }
  .btn-tool:hover { color: var(--text-secondary); background: var(--bg-hover); }
  .search-input {
    width: 120px;
    padding: 2px 8px;
    border: 1px solid var(--border-strong);
    border-radius: 4px;
    background: var(--bg-input);
    color: var(--text-primary);
    font-size: 12px;
    font-family: var(--font-mono);
  }
  .search-input:focus { outline: none; border-color: var(--accent); }
  .copy-wrapper { position: relative; }
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
  .btn-copy:hover:not(:disabled) { color: var(--text-secondary); background: var(--bg-hover); }
  .btn-copy:disabled { opacity: 0.4; cursor: not-allowed; }
  .copy-menu {
    position: absolute;
    top: 100%;
    right: 0;
    margin-top: 4px;
    background: var(--bg-surface);
    border: 1px solid var(--border-strong);
    border-radius: 6px;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.3);
    z-index: 50;
    min-width: 140px;
    padding: 4px 0;
  }
  .copy-menu button {
    display: block;
    width: 100%;
    padding: 6px 12px;
    border: none;
    background: transparent;
    color: var(--text-primary);
    cursor: pointer;
    font-size: 12px;
    text-align: left;
  }
  .copy-menu button:hover { background: var(--bg-active); }
  .copy-menu button:disabled { opacity: 0.4; cursor: not-allowed; }
  .value-content {
    flex: 1;
    overflow-y: auto;
    padding: 12px;
    font-family: var(--font-mono);
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
  .json-tree { line-height: 1.6; }
  .meta-table { border-collapse: collapse; width: auto; }
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
  .meta-value { color: var(--text-primary); }
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
    .meta-summary-toggle, .meta-toggle-icon, .mode-btn, .btn-copy, .btn-tool {
      transition: none;
    }
  }
</style>
```

- [ ] **Step 3: Commit ValueDisplay and JsonTree changes**

```bash
git add gui/frontend/src/components/ValueDisplay.svelte gui/frontend/src/components/JsonTree.svelte
git commit -m "feat(gui): add JSON search, expand/collapse all, and copy options"
```

---

## Task 9: StatsCards Component

**Files:**
- Create: `gui/frontend/src/components/StatsCards.svelte`

- [ ] **Step 1: Create StatsCards component**

Create `gui/frontend/src/components/StatsCards.svelte`:

```svelte
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

      // Stats returns an object with a single key (server address) containing the stats
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
    gap: 12px;
    padding: 12px;
    overflow-x: auto;
  }
  .stat-card {
    min-width: 140px;
    flex: 1;
    padding: 10px 12px;
    background: var(--bg-primary);
    border: 1px solid var(--border);
    border-radius: 6px;
  }
  .stat-label {
    font-size: 11px;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    margin-bottom: 4px;
  }
  .stat-value {
    font-size: 14px;
    font-weight: 600;
    font-family: var(--font-mono);
    color: var(--text-primary);
    margin-bottom: 6px;
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
```

- [ ] **Step 2: Commit StatsCards component**

```bash
git add gui/frontend/src/components/StatsCards.svelte
git commit -m "feat(gui): add StatsCards component for metrics visualization"
```

---

## Task 10: ConnectionBanner with Stats Display

**Files:**
- Modify: `gui/frontend/src/components/ConnectionBanner.svelte`

- [ ] **Step 1: Update ConnectionBanner with periodic stats refresh**

Replace `gui/frontend/src/components/ConnectionBanner.svelte`:

```svelte
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
    background: var(--success-soft);
    color: var(--success);
  }
  .banner-error {
    background: var(--danger-soft);
    border-bottom-color: var(--danger);
    color: var(--danger);
  }
  .banner-idle {
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
```

- [ ] **Step 2: Integrate StatsCards into App layout**

In `gui/frontend/src/App.svelte`, add StatsCards above the JSON display when on Stats tab. Add import and conditional render in the result area:

Add the import at the top of the script:
```svelte
import StatsCards from './components/StatsCards.svelte'
```

Add before the `<ValueDisplay />` inside the result-area div:
```svelte
{#if $activeOperationTab === 'stats' && $queryResult && $queryResult.success}
  <StatsCards data={$queryResult.data} />
{/if}
```

- [ ] **Step 3: Commit ConnectionBanner and StatsCards integration**

```bash
git add gui/frontend/src/components/ConnectionBanner.svelte gui/frontend/src/App.svelte
git commit -m "feat(gui): add stats display to ConnectionBanner and StatsCards to Stats view"
```

---

## Task 11: Spacing & Font Refinements

**Files:**
- Modify: `gui/frontend/src/components/ProtocolLog.svelte`
- Modify: `gui/frontend/src/components/Sidebar.svelte`

- [ ] **Step 1: Update ProtocolLog monospace font and tighten spacing**

In `gui/frontend/src/components/ProtocolLog.svelte`:
- Change `.log-list` font-family to use `var(--font-mono)` instead of hardcoded list
- Reduce `.log-header` padding from `8px 12px` to `6px 12px`
- Reduce `.log-entry` padding from `4px 12px` to `3px 12px`

- [ ] **Step 2: Update Sidebar spacing**

In `gui/frontend/src/components/Sidebar.svelte`:
- Reduce `.sidebar-header` padding from `16px` to `12px`
- Reduce `.context-list` padding from `8px` to `6px`
- Reduce `.context-item` padding from `10px 12px` to `8px 10px`

- [ ] **Step 3: Commit spacing refinements**

```bash
git add gui/frontend/src/components/ProtocolLog.svelte gui/frontend/src/components/Sidebar.svelte
git commit -m "style(gui): tighten spacing and use monospace font variable"
```

---

## Task 12: Final Verification

- [ ] **Step 1: Build the frontend to check for compilation errors**

```bash
cd gui/frontend && npm run build
```

Expected: Build succeeds with no errors.

- [ ] **Step 2: Build the Wails app to verify full integration**

```bash
cd gui && wails build
```

Expected: Build succeeds. If Wails CLI is not available, the frontend build alone is sufficient.

- [ ] **Step 3: Final commit if any fixes were needed**

```bash
git add -A gui/frontend/src/
git commit -m "fix(gui): address build issues from verification"
```
