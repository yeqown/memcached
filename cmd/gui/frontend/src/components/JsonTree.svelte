<script lang="ts">
  import JsonTree from './JsonTree.svelte'

  interface Props {
    data: unknown
    expanded?: boolean
    depth?: number
    searchQuery?: string
    forceExpanded?: boolean
    forceCollapsed?: boolean
  }

  let {
    data,
    expanded = $bindable(true),
    depth = 0,
    searchQuery = '',
    forceExpanded = false,
    forceCollapsed = false,
  }: Props = $props()

  function typeOf(v: unknown): string {
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

  const kind = $derived(typeOf(data))
  const isContainer = $derived(kind === 'object' || kind === 'array')
  const entries = $derived(
    isContainer
      ? kind === 'array'
        ? (data as unknown[]).map((v, i) => [String(i), v] as [string, unknown])
        : Object.entries(data as Record<string, unknown>)
      : ([] as [string, unknown][]),
  )
  const count = $derived(entries.length)
  const effectiveExpanded = $derived(forceExpanded ? true : forceCollapsed ? false : expanded)
  const matchesSearch = $derived(!searchQuery || nodeMatches(data, searchQuery, kind))
  const childMatchesSearch = $derived(
    searchQuery && isContainer
      ? entries.some(([key, val]) => childNodeMatches(key, val, searchQuery))
      : false,
  )
  const showChildren = $derived(effectiveExpanded || (!!searchQuery && childMatchesSearch))

  function nodeMatches(value: unknown, query: string, valueKind: string): boolean {
    if (valueKind === 'string') return String(value).toLowerCase().includes(query.toLowerCase())
    if (valueKind === 'number' || valueKind === 'boolean')
      return String(value).toLowerCase().includes(query.toLowerCase())
    return false
  }

  function childNodeMatches(key: string, value: unknown, query: string): boolean {
    if (key.toLowerCase().includes(query.toLowerCase())) return true
    const childKind = typeOf(value)
    if (childKind === 'object' || childKind === 'array') {
      const childEntries =
        childKind === 'array'
          ? (value as unknown[]).map((v, i) => [String(i), v] as [string, unknown])
          : Object.entries(value as Record<string, unknown>)
      return childEntries.some(([k, v]) => childNodeMatches(k, v, query))
    }
    return nodeMatches(value, query, childKind)
  }
</script>

{#if !searchQuery || matchesSearch || childMatchesSearch}
  <span class="depth-pad" style="padding-left: {depth * 16}px"></span>

  {#if isContainer}
    <button
      type="button"
      class="toggle"
      onclick={toggle}
      onkeydown={onToggleKeydown}
      aria-label={effectiveExpanded ? 'Collapse node' : 'Expand node'}
    >
      <span class="toggle-icon" class:expanded={effectiveExpanded} aria-hidden="true"></span>
    </button>
    <span class="bracket">{kind === 'array' ? '[' : '{'}</span>
    {#if !showChildren}
      <button
        type="button"
        class="ellipsis"
        onclick={toggle}
        onkeydown={onToggleKeydown}
        aria-label="Expand node"
      >
        &hellip;{count}&nbsp;{kind === 'array' ? 'items' : 'keys'}
      </button>
      <span class="bracket">{kind === 'array' ? ']' : '}'}</span>
    {:else}
      <div class="children">
        {#each entries as [key, val], i (key)}
          {#if !searchQuery || String(key)
              .toLowerCase()
              .includes(searchQuery.toLowerCase()) || childNodeMatches(key, val, searchQuery)}
            <div class="entry">
              <span class="depth-pad" style="padding-left: {(depth + 1) * 16}px"></span>
              {#if kind === 'object'}
                <span class="json-key">&quot;{key}&quot;</span><span class="colon">: </span>
              {:else}
                <span class="json-index">{key}</span><span class="colon">: </span>
              {/if}
              <JsonTree
                data={val}
                depth={depth + 1}
                expanded={depth < 1}
                {searchQuery}
                {forceExpanded}
                {forceCollapsed}
              />
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
  .depth-pad {
    display: inline;
  }
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
    transition:
      background 0.2s ease-out,
      color 0.2s ease-out;
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
  .bracket {
    color: var(--text-secondary);
  }
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
  .colon {
    color: var(--text-secondary);
  }
  .comma {
    color: var(--text-muted);
  }
  .json-key {
    color: var(--json-key);
  }
  .json-index {
    color: var(--text-secondary);
  }
  .json-string {
    color: var(--json-string);
  }
  .json-number {
    color: var(--json-number);
  }
  .json-boolean {
    color: var(--json-boolean);
  }
  .json-null {
    color: var(--text-muted);
    font-style: italic;
  }
</style>
