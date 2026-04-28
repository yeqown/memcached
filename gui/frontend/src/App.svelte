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

    const savedTab = loadString('active-tab') as OperationTab | null
    if (savedTab) {
      operationPanelRef.setTab(savedTab)
    }

    return () => {
      window.removeEventListener('keydown', handleKeydown)
    }
  })

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
      <OperationPanel bind:this={operationPanelRef} />
      <div class="result-area" class:hidden={$activeOperationTab === 'set'}>
        <ValueDisplay />
      </div>
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
    width: 240px;
    min-width: 240px;
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
  .result-area.hidden {
    display: none;
  }
  .log-dock-full {
    flex: 0 0 30%;
    min-height: 192px;
    max-height: 440px;
    border-top: 1px solid var(--border);
    overflow: hidden;
  }
</style>
