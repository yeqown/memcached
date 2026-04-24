<script lang="ts">
  import { createEventDispatcher } from 'svelte'
  import type { McContext, McServer } from '../stores/app'

  export let show = false
  export let editContext: McContext | null = null

  let name = ''
  let servers: McServer[] = [{ host: 'localhost', port: 11211 }]
  const dispatch = createEventDispatcher()

  $: if (show) {
    if (editContext) {
      name = editContext.name
      servers = editContext.servers.map(s => ({ ...s }))
    } else {
      name = ''
      servers = [{ host: 'localhost', port: 11211 }]
    }
  }

  function addServer() {
    servers = [...servers, { host: 'localhost', port: 11211 }]
  }

  function removeServer(index: number) {
    if (servers.length <= 1) return
    servers = servers.filter((_, i) => i !== index)
  }

  function handleSave() {
    if (!name.trim()) return
    const ctx: McContext = {
      id: editContext?.id || '',
      name: name.trim(),
      servers: servers.filter(s => s.host.trim()).map(s => ({
        host: s.host.trim(),
        port: Number(s.port),
      })),
    }
    dispatch('save', ctx)
    show = false
  }

  function handleCancel() {
    show = false
  }

  function handleOverlayKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.preventDefault()
      handleCancel()
    }
  }
</script>

{#if show}
  <div class="overlay" role="presentation" on:click|self={handleCancel} on:keydown={handleOverlayKeydown}>
    <div class="dialog" role="dialog" aria-modal="true" aria-labelledby="ctx-dialog-title">
      <h2 id="ctx-dialog-title">{editContext ? 'Edit Context' : 'Add Context'}</h2>

      <div class="form-group">
        <label for="ctx-name">Name</label>
        <input id="ctx-name" type="text" bind:value={name} placeholder="My Cluster" />
      </div>

      <fieldset class="form-group server-fieldset">
        <legend>Servers</legend>
        {#each servers as server, i}
          <div class="server-row">
            <input
              id={`server-host-${i}`}
              type="text"
              bind:value={server.host}
              placeholder="host"
              aria-label={`Server ${i + 1} host`}
            />
            <input
              id={`server-port-${i}`}
              type="number"
              bind:value={server.port}
              placeholder="port"
              aria-label={`Server ${i + 1} port`}
            />
            {#if servers.length > 1}
              <button type="button" class="btn-icon btn-danger" on:click={() => removeServer(i)} aria-label={`Remove server ${i + 1}`}>×</button>
            {/if}
          </div>
        {/each}
        <button type="button" class="btn-small" on:click={addServer}>+ Add Server</button>
      </fieldset>

      <div class="actions">
        <button type="button" class="btn-secondary" on:click={handleCancel}>Cancel</button>
        <button type="button" class="btn-primary" on:click={handleSave}>
          {editContext ? 'Update' : 'Create'}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .overlay {
    position: fixed;
    top: 0; left: 0; right: 0; bottom: 0;
    background: var(--bg-overlay);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
  }
  .dialog {
    background: var(--bg-surface);
    border: 1px solid var(--border-strong);
    border-radius: 12px;
    padding: 24px;
    width: 480px;
    max-height: 80vh;
    overflow-y: auto;
    color: var(--text-primary);
    box-shadow: 0 16px 40px rgba(0, 0, 0, 0.28);
  }
  h2 {
    margin: 0 0 20px 0;
    font-size: 18px;
    color: var(--text-primary);
  }
  .form-group {
    margin-bottom: 16px;
  }
  .server-fieldset {
    border: none;
    padding: 0;
    margin: 0 0 16px 0;
  }
  .server-fieldset legend {
    display: block;
    margin-bottom: 6px;
    font-size: 13px;
    color: var(--text-secondary);
    font-weight: 500;
  }
  label {
    display: block;
    margin-bottom: 6px;
    font-size: 13px;
    color: var(--text-secondary);
    font-weight: 500;
  }
  input[type="text"], input[type="number"] {
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
  input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 2px var(--accent-focus-ring);
  }
  .server-row {
    display: flex;
    gap: 8px;
    margin-bottom: 8px;
    align-items: center;
  }
  .server-row input:first-child {
    flex: 2;
  }
  .server-row input:nth-child(2) {
    flex: 1;
  }
  .btn-icon {
    width: 28px;
    height: 28px;
    border: none;
    border-radius: 4px;
    cursor: pointer;
    font-size: 14px;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    transition: background 0.2s ease-out, color 0.2s ease-out;
  }
  .btn-danger {
    background: var(--danger-soft);
    color: var(--danger);
  }
  .btn-danger:hover {
    background: var(--danger);
    color: var(--accent-contrast);
  }
  .btn-small {
    background: transparent;
    border: 1px dashed var(--border-strong);
    color: var(--text-secondary);
    padding: 6px 12px;
    border-radius: 6px;
    cursor: pointer;
    font-size: 13px;
    width: 100%;
    transition: border-color 0.2s ease-out, color 0.2s ease-out, background 0.2s ease-out;
  }
  .btn-small:hover {
    border-color: var(--accent);
    color: var(--accent);
    background: var(--accent-focus-ring);
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
    margin-top: 20px;
  }
  .btn-primary, .btn-secondary {
    padding: 8px 20px;
    border-radius: 6px;
    border: none;
    cursor: pointer;
    font-size: 14px;
    font-weight: 500;
    transition: background 0.2s ease-out, color 0.2s ease-out;
  }
  .btn-primary {
    background: var(--accent);
    color: var(--accent-contrast);
  }
  .btn-primary:hover {
    background: var(--accent-hover);
  }
  .btn-secondary {
    background: var(--bg-surface-soft);
    color: var(--text-secondary);
  }
  .btn-secondary:hover {
    background: var(--bg-active);
    color: var(--text-primary);
  }
  .btn-icon:focus-visible,
  .btn-small:focus-visible,
  .btn-primary:focus-visible,
  .btn-secondary:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  @media (prefers-reduced-motion: reduce) {
    input[type="text"],
    input[type="number"],
    .btn-icon,
    .btn-small,
    .btn-primary,
    .btn-secondary {
      transition: none;
    }
  }
</style>

