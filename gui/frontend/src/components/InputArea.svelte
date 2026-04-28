<script lang="ts">
  import { getCommand, type CommandId } from '../lib/commands'
  import KeyInput from './KeyInput.svelte'

  export let command: CommandId = 'get'
  export let disabled = false

  // Store values for each possible input
  let values: Record<string, string | number> = {
    key: '',
    value: '',
    flags: 0,
    expiry: 0,
    delta: 1,
  }

  export function getValues(): Record<string, string | number> {
    const cmd = getCommand(command)
    if (!cmd) return {}

    const result: Record<string, string | number> = {}
    for (const input of cmd.inputs) {
      result[input.id] = values[input.id] ?? ''
    }
    return result
  }

  export function getKey(): string {
    return values.key as string
  }

  export function clearKey() {
    values.key = ''
  }

  // Reset default values when command changes
  $: {
    const cmd = getCommand(command)
    if (cmd) {
      for (const input of cmd.inputs) {
        if (input.defaultValue !== undefined) {
          values[input.id] = input.defaultValue
        }
      }
    }
  }
</script>

<div class="input-area">
  {#each getCommand(command)?.inputs || [] as field (field.id)}
    <div class="input-field">
      {#if field.type === 'text'}
        <div class="field">
          <label for="input-{field.id}">{field.label}</label>
          <KeyInput
            id="input-{field.id}"
            bind:value={values.key}
            placeholder={field.placeholder || field.label}
            {disabled}
          />
        </div>
      {:else if field.type === 'textarea'}
        <div class="field">
          <label for="input-{field.id}">{field.label}</label>
          <textarea
            id="input-{field.id}"
            bind:value={values.value}
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
            bind:value={values[field.id]}
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