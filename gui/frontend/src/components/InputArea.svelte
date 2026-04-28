<script lang="ts">
  import { getCommand, type CommandId } from '../lib/commands'
  import KeyInput from './KeyInput.svelte'

  export let command: CommandId = 'get'
  export let disabled = false

  let key = ''
  let value = ''
  let flags = 0
  let expiry = 0
  let delta = 1

  export function getValues(): Record<string, string | number> {
    const cmd = getCommand(command)
    if (!cmd) return {}

    const result: Record<string, string | number> = {}
    for (const input of cmd.inputs) {
      if (input.id === 'key') result.key = key
      else if (input.id === 'value') result.value = value
      else if (input.id === 'flags') result.flags = flags
      else if (input.id === 'expiry') result.expiry = expiry
      else if (input.id === 'delta') result.delta = delta
    }
    return result
  }

  export function getKey(): string {
    return key
  }

  export function clearKey() {
    key = ''
  }

  $: {
    const cmd = getCommand(command)
    if (cmd) {
      for (const input of cmd.inputs) {
        if (input.id === 'delta' && input.defaultValue !== undefined) {
          delta = input.defaultValue as number
        }
        if (input.id === 'flags' && input.defaultValue !== undefined) {
          flags = input.defaultValue as number
        }
        if (input.id === 'expiry' && input.defaultValue !== undefined) {
          expiry = input.defaultValue as number
        }
      }
    }
  }
</script>

<div class="input-area">
  {#each getCommand(command)?.inputs || [] as field}
    <div class="input-field">
      {#if field.type === 'text'}
        <div class="field">
          <label for="input-{field.id}">{field.label}</label>
          <KeyInput
            id="input-{field.id}"
            bind:value={key}
            placeholder={field.placeholder || field.label}
            {disabled}
          />
        </div>
      {:else if field.type === 'textarea'}
        <div class="field">
          <label for="input-{field.id}">{field.label}</label>
          <textarea
            id="input-{field.id}"
            bind:value={value}
            placeholder={field.placeholder || field.label}
            rows="4"
            {disabled}
          ></textarea>
        </div>
      {:else if field.type === 'number'}
        <div class="field">
          <label for="input-{field.id}">{field.label}</label>
          <input
            id="input-{field.id}"
            type="number"
            bind:value={field.id === 'delta' ? delta : field.id === 'flags' ? flags : expiry}
            min="0"
            {disabled}
          />
        </div>
      {/if}
    </div>
  {/each}
</div>

<style>
  .input-area {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .input-field {
    display: flex;
    flex-direction: column;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  label {
    font-size: 12px;
    font-weight: 500;
    color: var(--text-muted);
  }
  textarea {
    width: 100%;
    padding: 8px 12px;
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: 8px;
    color: var(--text-primary);
    font-size: 13px;
    font-family: var(--font-mono);
    box-sizing: border-box;
    resize: vertical;
    transition: border-color 0.15s, box-shadow 0.15s;
  }
  textarea:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  textarea:disabled {
    opacity: 0.5;
  }
  input[type="number"] {
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
  input[type="number"]:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  input[type="number"]:disabled {
    opacity: 0.5;
  }
  @media (prefers-reduced-motion: reduce) {
    textarea, input[type="number"] { transition: none; }
  }
</style>
