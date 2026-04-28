<script lang="ts">
  import { createEventDispatcher } from 'svelte'
  import { COMMANDS, COMMAND_GROUPS, getCommand, type CommandId } from '../lib/commands'

  export let selected: CommandId = 'get'
  export let disabled = false

  const dispatch = createEventDispatcher<'change', CommandId>()

  let open = false

  function toggleDropdown() {
    if (!disabled) open = !open
  }

  function selectCommand(id: CommandId) {
    selected = id
    open = false
    dispatch('change', id)
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      open = false
    }
  }

  $: selectedLabel = getCommand(selected)?.label || 'Select Command'

  function handleClickOutside(e: MouseEvent) {
    const target = e.target as HTMLElement
    if (!target.closest('.command-selector')) {
      open = false
    }
  }
</script>

<svelte:window on:click={handleClickOutside} on:keydown={handleKeydown} />

<div class="command-selector" class:disabled>
  <button
    type="button"
    class="selector-button"
    on:click|stopPropagation={toggleDropdown}
    {disabled}
    aria-haspopup="listbox"
    aria-expanded={open}
  >
    <span class="selector-label">{selectedLabel}</span>
    <span class="selector-arrow" class:open>▼</span>
  </button>

  {#if open}
    <div class="dropdown" role="listbox">
      {#each COMMAND_GROUPS as group}
        <div class="dropdown-group">
          <div class="group-label">{group.label}</div>
          {#each COMMANDS.filter(c => c.group === group.id) as cmd}
            <button
              type="button"
              class="dropdown-item"
              class:selected={selected === cmd.id}
              on:click={() => selectCommand(cmd.id as CommandId)}
              role="option"
              aria-selected={selected === cmd.id}
            >
              {cmd.label}
            </button>
          {/each}
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .command-selector {
    position: relative;
    display: inline-block;
  }
  .selector-button {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 7px 12px;
    background: var(--bg-surface-soft);
    border: 1px solid var(--border);
    border-radius: 8px;
    color: var(--text-primary);
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
    min-width: 140px;
  }
  .selector-button:hover:not(:disabled) {
    background: var(--bg-surface);
    border-color: var(--accent);
  }
  .selector-button:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .selector-button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .selector-label {
    flex: 1;
    text-align: left;
  }
  .selector-arrow {
    font-size: 10px;
    color: var(--text-muted);
    transition: transform 0.15s;
  }
  .selector-arrow.open {
    transform: rotate(180deg);
  }
  .dropdown {
    position: absolute;
    top: calc(100% + 4px);
    left: 0;
    min-width: 180px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.3);
    z-index: 100;
    padding: 4px;
  }
  .dropdown-group {
    margin-bottom: 4px;
  }
  .dropdown-group:last-child {
    margin-bottom: 0;
  }
  .group-label {
    padding: 6px 10px 4px;
    font-size: 11px;
    font-weight: 600;
    color: var(--text-dim);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .dropdown-item {
    display: block;
    width: 100%;
    padding: 7px 10px;
    background: transparent;
    border: none;
    border-radius: 6px;
    color: var(--text-primary);
    font-size: 13px;
    text-align: left;
    cursor: pointer;
    transition: background 0.1s;
  }
  .dropdown-item:hover {
    background: var(--accent-soft);
  }
  .dropdown-item.selected {
    background: var(--accent);
    color: var(--accent-contrast);
  }
  .dropdown-item:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .disabled .selector-button {
    opacity: 0.5;
    cursor: not-allowed;
  }
  @media (prefers-reduced-motion: reduce) {
    .selector-button, .selector-arrow, .dropdown-item { transition: none; }
  }
</style>
