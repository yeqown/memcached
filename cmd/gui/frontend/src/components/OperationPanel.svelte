<script lang="ts">
  import { connected, addLog, displayValue, displayMode, queryResult, activeOperationTab } from '../stores/app'
  import { keyHistory } from '../stores/keyHistory'
  import { Get, Set, Delete, Incr, Decr, Stats, FlushAll, Version } from '../../wailsjs/go/service/OperationService.js'
  import { getCommand, type CommandId } from '../lib/commands'
  import ThemeToggle from './ThemeToggle.svelte'
  import CommandSelector from './CommandSelector.svelte'
  import InputArea from './InputArea.svelte'

  let activeCommand: CommandId = 'get'
  let inputArea: InputArea

  $: activeOperationTab.set(activeCommand)

  export function setTab(tab: CommandId) {
    activeCommand = tab
  }

  export function executeCurrent() {
    executeCommand(activeCommand)
  }

  async function executeCommand(cmd: CommandId) {
    const def = getCommand(cmd)
    if (!def) return

    if (def.needsConfirmation) {
      const key = inputArea?.getKey() || ''
      const msg = cmd === 'delete' ? `Delete key "${key}"?` : cmd === 'flushall' ? 'Flush ALL data? This cannot be undone.' : 'Confirm?'
      if (!confirm(msg)) return
    }

    const values = inputArea?.getValues() || {}
    const key = (values.key as string) || ''

    try {
      switch (cmd) {
        case 'get':
          if (!key.trim()) return
          addLog({ op: 'GET', key, status: 'info', message: 'Fetching...' })
          const getResult = await Get(key)
          keyHistory.add(key)
          queryResult.set(getResult)
          if (getResult.success) {
            displayValue.set(getResult.data || getResult.value || '(empty value)')
            displayMode.set(getResult.valueKind === 'json' ? 'json' : 'text')
            addLog({ op: 'GET', key, status: 'success', message: 'OK' })
          } else {
            displayValue.set(getResult.error)
            displayMode.set('text')
            addLog({ op: 'GET', key, status: 'error', message: getResult.error })
          }
          break

        case 'set':
          if (!key.trim()) return
          addLog({ op: 'SET', key, status: 'info', message: 'Storing...' })
          await Set(key, values.value as string, values.flags as number, values.expiry as number)
          keyHistory.add(key)
          addLog({ op: 'SET', key, status: 'success', message: 'OK' })
          break

        case 'delete':
          if (!key.trim()) return
          addLog({ op: 'DELETE', key, status: 'info', message: 'Deleting...' })
          await Delete(key)
          keyHistory.add(key)
          displayValue.set('')
          addLog({ op: 'DELETE', key, status: 'success', message: 'OK' })
          break

        case 'incr':
          if (!key.trim()) return
          addLog({ op: 'INCR', key, status: 'info', message: 'Incrementing...' })
          const incrResult = await Incr(key, values.delta as number)
          keyHistory.add(key)
          displayValue.set(incrResult.data || 'OK')
          displayMode.set('text')
          addLog({ op: 'INCR', key, status: 'success', message: 'OK' })
          break

        case 'decr':
          if (!key.trim()) return
          addLog({ op: 'DECR', key, status: 'info', message: 'Decrementing...' })
          const decrResult = await Decr(key, values.delta as number)
          keyHistory.add(key)
          displayValue.set(decrResult.data || 'OK')
          displayMode.set('text')
          addLog({ op: 'DECR', key, status: 'success', message: 'OK' })
          break

        case 'stats':
          addLog({ op: 'STATS', status: 'info', message: 'Fetching stats...' })
          const statsResult = await Stats()
          if (statsResult.success) {
            displayValue.set(JSON.stringify(statsResult.data, null, 2))
            displayMode.set('json')
            addLog({ op: 'STATS', status: 'success', message: 'OK' })
          } else {
            displayValue.set(statsResult.error)
            displayMode.set('text')
            addLog({ op: 'STATS', status: 'error', message: statsResult.error })
          }
          break

        case 'flushall':
          addLog({ op: 'FLUSHALL', status: 'info', message: 'Flushing...' })
          await FlushAll()
          displayValue.set('')
          addLog({ op: 'FLUSHALL', status: 'success', message: 'OK' })
          break

        case 'version':
          addLog({ op: 'VERSION', status: 'info', message: 'Checking version...' })
          const versionResult = await Version()
          displayValue.set(versionResult || 'OK')
          displayMode.set('text')
          addLog({ op: 'VERSION', status: 'success', message: 'OK' })
          break
      }
    } catch (e: any) {
      addLog({ op: cmd.toUpperCase(), key, status: 'error', message: e.message || String(e) })
    }
  }

  function handleCommandChange(cmd: CommandId) {
    activeCommand = cmd
    inputArea?.clearKey()
  }
</script>

<div class="panel">
  <div class="panel-head">
    <CommandSelector
      selected={activeCommand}
      on:change={(e) => handleCommandChange(e.detail)}
      disabled={!$connected}
    />
    <div class="panel-tools">
      <ThemeToggle />
    </div>
  </div>

  <div class="panel-body">
    {#if !$connected}
      <div class="disabled-overlay">Connect to a context first</div>
    {/if}

    <div class="input-section" class:hidden={!getCommand(activeCommand)?.inputs.length}>
      <InputArea bind:this={inputArea} command={activeCommand} disabled={!$connected} />
    </div>

    <div class="action-row">
      <button
        type="button"
        on:click={executeCurrent}
        disabled={!$connected}
        class:btn-danger={getCommand(activeCommand)?.needsConfirmation}
      >
        {getCommand(activeCommand)?.label || 'Execute'}
      </button>
    </div>
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
  .panel-tools {
    flex-shrink: 0;
  }
  .panel-body {
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
  .input-section {
    margin-bottom: 12px;
  }
  .input-section.hidden {
    display: none;
  }
  .action-row {
    display: flex;
    justify-content: flex-end;
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
  button:hover:not(:disabled) {
    background: var(--accent-hover);
  }
  button:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  button:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .btn-danger {
    background: var(--danger);
  }
  .btn-danger:hover:not(:disabled) {
    background: var(--danger-hover);
  }
  @media (prefers-reduced-motion: reduce) {
    button { transition: none; }
  }
</style>
