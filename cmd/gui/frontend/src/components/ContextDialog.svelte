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
    border: 1px solid var(--border);
    border-radius: 14px;
    padding: 24px;
    width: 480px;
    max-height: 80vh;
    overflow-y: auto;
    color: var(--text-primary);
    box-shadow: 0 16px 48px rgba(0, 0, 0, 0.3);
  }
  h2 {
    margin: 0 0 20px 0;
    font-size: 17px;
    font-weight: 600;
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
    margin-bottom: 8px;
    font-size: 12px;
    font-weight: 600;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.3px;
  }
  label {
    display: block;
    margin-bottom: 6px;
    font-size: 12px;
    font-weight: 500;
    color: var(--text-muted);
  }
  input[type="text"], input[type="number"] {
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
  input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  .server-row {
    display: flex;
    gap: 8px;
    margin-bottom: 8px;
    align-items: center;
  }
  .server-row input:first-child { flex: 2; }
  .server-row input:nth-child(2) { flex: 1; }
  .btn-icon {
    width: 28px;
    height: 28px;
    border: none;
    border-radius: 6px;
    cursor: pointer;
    font-size: 14px;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    transition: background 0.15s, color 0.15s;
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
    color: var(--text-muted);
    padding: 6px 12px;
    border-radius: 8px;
    cursor: pointer;
    font-size: 12px;
    width: 100%;
    transition: border-color 0.15s, color 0.15s, background 0.15s;
  }
  .btn-small:hover {
    border-color: var(--accent);
    color: var(--accent);
    background: var(--accent-soft);
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 20px;
  }
  .btn-primary, .btn-secondary {
    padding: 7px 16px;
    border-radius: 8px;
    border: none;
    cursor: pointer;
    font-size: 13px;
    font-weight: 500;
    transition: background 0.15s, color 0.15s;
  }
  .btn-primary {
    background: var(--accent);
    color: var(--accent-contrast);
  }
  .btn-primary:hover { background: var(--accent-hover); }
  .btn-secondary {
    background: var(--bg-surface-soft);
    color: var(--text-primary);
  }
  .btn-secondary:hover { background: var(--bg-active); color: var(--text-primary); }
  .btn-icon:focus-visible,
  .btn-small:focus-visible,
  .btn-primary:focus-visible,
  .btn-secondary:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  @media (prefers-reduced-motion: reduce) {
    input[type="text"], input[type="number"], .btn-icon, .btn-small, .btn-primary, .btn-secondary {
      transition: none;
    }
  }
</style>

