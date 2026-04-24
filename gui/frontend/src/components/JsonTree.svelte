<script lang="ts">
  export let data: any
  export let expanded = true
  export let depth = 0

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
</script>

<span class="depth-pad" style="padding-left: {depth * 16}px"></span>

{#if isContainer}
  <button type="button" class="toggle" on:click={toggle} on:keydown={onToggleKeydown} aria-label={expanded ? 'Collapse node' : 'Expand node'}>
    <span class="toggle-icon" class:expanded={expanded} aria-hidden="true"></span>
  </button>
  <span class="bracket">{kind === 'array' ? '[' : '{'}</span>
  {#if !expanded}
    <button type="button" class="ellipsis" on:click={toggle} on:keydown={onToggleKeydown} aria-label="Expand node">
      &hellip;{count}&nbsp;{kind === 'array' ? 'items' : 'keys'}
    </button>
    <span class="bracket">{kind === 'array' ? ']' : '}'}</span>
  {:else}
    <div class="children">
      {#each entries as [key, val], i}
        <div class="entry">
          <span class="depth-pad" style="padding-left: {(depth + 1) * 16}px"></span>
          {#if kind === 'object'}
            <span class="json-key">&quot;{key}&quot;</span><span class="colon">: </span>
          {:else}
            <span class="json-index">{key}</span><span class="colon">: </span>
          {/if}
          <svelte:self data={val} depth={depth + 1} expanded={depth < 1} />
          {#if i < count - 1}<span class="comma">,</span>{/if}
        </div>
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
  .toggle-icon.expanded {
    transform: rotate(90deg);
  }
  .toggle:focus-visible,
  .ellipsis:focus-visible {
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
  .children {
    display: flex;
    flex-direction: column;
  }
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
