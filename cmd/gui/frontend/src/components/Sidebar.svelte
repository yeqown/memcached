<script lang="ts">
  import {
    contexts,
    activeContextId,
    connected,
    addLog,
    connectionStatus,
    connectionError,
    activeContextName,
    type McContext,
  } from '../stores/app'
  import {
    LoadContexts,
    SaveContext,
    DeleteContext,
    Connect,
    Disconnect,
  } from '../../wailsjs/go/service/ConnectionService.js'
  import { service } from '../../wailsjs/go/models'
  import ContextDialog from './ContextDialog.svelte'

  let showDialog = $state(false)
  let editingContext = $state<McContext | null>(null)

  async function load() {
    try {
      const list = await LoadContexts()
      contexts.set(list || [])
    } catch (e: unknown) {
      const message = e instanceof Error ? e.message : String(e)
      addLog({ op: 'Load', status: 'error', message })
    }
  }

  async function handleConnect(id: string) {
    if ($activeContextId === id && $connected) {
      await handleDisconnect()
      return
    }
    try {
      await Connect(id)
      connected.set(true)
      activeContextId.set(id)
      const ctx = $contexts.find((c) => c.id === id)
      connectionStatus.set('connected')
      connectionError.set('')
      activeContextName.set(ctx?.name || id)
      addLog({ op: 'Connect', status: 'success', message: `Connected to ${ctx?.name || id}` })
    } catch (e: unknown) {
      const message = e instanceof Error ? e.message : String(e)
      connected.set(false)
      activeContextId.set(null)
      connectionStatus.set('error')
      connectionError.set(message)
      activeContextName.set('')
      addLog({ op: 'Connect', status: 'error', message })
    }
  }

  async function handleDisconnect() {
    try {
      await Disconnect()
      connected.set(false)
      activeContextId.set(null)
      connectionStatus.set('disconnected')
      connectionError.set('')
      activeContextName.set('')
      addLog({ op: 'Disconnect', status: 'info', message: 'Disconnected' })
    } catch (e: unknown) {
      const message = e instanceof Error ? e.message : String(e)
      addLog({ op: 'Disconnect', status: 'error', message })
    }
  }

  async function handleSaveContext(ctx: McContext) {
    try {
      await SaveContext(service.Context.createFrom(ctx))
      await load()
      addLog({ op: 'Save', status: 'success', message: `Context "${ctx.name}" saved` })
    } catch (e: unknown) {
      const message = e instanceof Error ? e.message : String(e)
      addLog({ op: 'Save', status: 'error', message })
    }
  }

  async function handleDeleteContext(id: string, name: string) {
    if (!confirm(`Delete context "${name}"?`)) return
    try {
      await DeleteContext(id)
      if ($activeContextId === id) {
        await handleDisconnect()
      }
      await load()
      addLog({ op: 'Delete', status: 'info', message: `Context "${name}" deleted` })
    } catch (e: unknown) {
      const message = e instanceof Error ? e.message : String(e)
      addLog({ op: 'Delete', status: 'error', message })
    }
  }

  function handleEdit(ctx: McContext, e: MouseEvent) {
    e.stopPropagation()
    editingContext = ctx
    showDialog = true
  }

  function handleDeleteClick(id: string, name: string, e: MouseEvent) {
    e.stopPropagation()
    void handleDeleteContext(id, name)
  }

  function handleAdd() {
    editingContext = null
    showDialog = true
  }

  function onItemKeydown(event: KeyboardEvent, id: string) {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      void handleConnect(id)
    }
  }

  void load()
</script>

<div class="sidebar">
  <div class="sidebar-header">
    <h3>Contexts</h3>
    <button type="button" class="btn-add" onclick={handleAdd} aria-label="Add context">+</button>
  </div>

  <div class="context-list" aria-live="polite">
    {#if $contexts.length === 0}
      <div class="empty">No contexts yet</div>
    {:else}
      {#each $contexts as ctx (ctx.id)}
        <div
          class="context-item"
          class:active={$activeContextId === ctx.id}
          class:connected={$activeContextId === ctx.id && $connected}
          onclick={() => void handleConnect(ctx.id)}
          onkeydown={(event) => onItemKeydown(event, ctx.id)}
          tabindex="0"
          role="button"
          aria-pressed={$activeContextId === ctx.id && $connected}
          aria-label={`Connect context ${ctx.name}`}
        >
          <div class="context-info">
            <div class="context-name">
              {#if $activeContextId === ctx.id && $connected}
                <span class="dot green"></span>
              {:else}
                <span class="dot gray"></span>
              {/if}
              {ctx.name}
            </div>
            <div class="context-servers">
              {ctx.servers.length} server{ctx.servers.length !== 1 ? 's' : ''}
            </div>
          </div>
          <div class="context-actions">
            <button
              type="button"
              class="btn-tiny"
              onclick={(e) => handleEdit(ctx, e)}
              title="Edit"
              aria-label={`Edit context ${ctx.name}`}
            >
              &#9998;
            </button>
            <button
              type="button"
              class="btn-tiny btn-tiny-danger"
              onclick={(e) => handleDeleteClick(ctx.id, ctx.name, e)}
              title="Delete"
              aria-label={`Delete context ${ctx.name}`}
            >
              &#10005;
            </button>
          </div>
        </div>
      {/each}
    {/if}
  </div>
</div>

<ContextDialog bind:show={showDialog} editContext={editingContext} onsave={handleSaveContext} />

<style>
  .sidebar {
    flex: 1;
    background: var(--bg-sidebar);
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .sidebar-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 14px 16px;
    border-bottom: 1px solid var(--border);
  }
  .sidebar-header h3 {
    margin: 0;
    font-size: 11px;
    font-weight: 600;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .btn-add {
    width: 26px;
    height: 26px;
    border-radius: 6px;
    border: 1px dashed var(--border-strong);
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 16px;
    display: flex;
    align-items: center;
    justify-content: center;
    transition:
      border-color 0.15s,
      color 0.15s,
      background 0.15s;
  }
  .btn-add:hover {
    border-color: var(--accent);
    color: var(--accent);
    background: var(--accent-soft);
  }
  .btn-add:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .context-list {
    flex: 1;
    overflow-y: auto;
    padding: 8px;
  }
  .empty {
    text-align: center;
    color: var(--text-dim);
    padding: 24px 0;
    font-size: 13px;
  }
  .context-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 10px;
    border-radius: 8px;
    cursor: pointer;
    margin-bottom: 2px;
    transition: background 0.15s;
  }
  .context-item:hover {
    background: var(--bg-hover);
  }
  .context-item.active {
    background: var(--accent-soft);
  }
  .context-item:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .context-info {
    flex: 1;
    min-width: 0;
  }
  .context-name {
    font-size: 13px;
    font-weight: 500;
    color: var(--text-primary);
    display: flex;
    align-items: center;
    gap: 8px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .dot.green {
    background: var(--success);
  }
  .dot.gray {
    background: var(--text-dim);
  }
  .context-servers {
    font-size: 11px;
    color: var(--text-muted);
    margin-top: 2px;
    padding-left: 16px;
  }
  .context-actions {
    display: flex;
    gap: 4px;
    opacity: 0;
    transition: opacity 0.15s;
  }
  .context-item:hover .context-actions,
  .context-item:focus-within .context-actions {
    opacity: 1;
  }
  .btn-tiny {
    width: 28px;
    height: 28px;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 14px;
    font-weight: 600;
    line-height: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    transition:
      background 0.15s,
      color 0.15s;
  }
  .btn-tiny:hover {
    background: var(--bg-active);
  }
  .btn-tiny:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .btn-tiny-danger:hover {
    background: var(--danger-soft);
    color: var(--danger);
  }

  @media (prefers-reduced-motion: reduce) {
    .btn-add,
    .context-item,
    .context-actions,
    .btn-tiny {
      transition: none;
    }
  }
</style>
