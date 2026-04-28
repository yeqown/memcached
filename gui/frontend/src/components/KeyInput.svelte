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
    border: 1px solid var(--border);
    border-radius: 8px;
    color: var(--text-primary);
    font-size: 13px;
    font-family: var(--font-mono);
    box-sizing: border-box;
    transition: border-color 0.15s, box-shadow 0.15s;
  }
  input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  input:disabled { opacity: 0.5; }
  .suggestions {
    position: absolute;
    top: 100%;
    left: 0;
    right: 0;
    margin: 4px 0 0;
    padding: 4px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    list-style: none;
    z-index: 50;
    max-height: 200px;
    overflow-y: auto;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.3);
  }
  li {
    padding: 6px 10px;
    cursor: pointer;
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    border-radius: 6px;
  }
  li:hover, li.selected {
    background: var(--accent-soft);
    color: var(--accent);
  }
  @media (prefers-reduced-motion: reduce) {
    input { transition: none; }
  }
</style>