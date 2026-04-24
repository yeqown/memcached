<script lang="ts">
  import { onDestroy } from 'svelte'
  import { themeMode } from '../stores/app'

  function resolveActual(mode: string): string {
    if (mode === 'system') {
      return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
    }
    return mode
  }

  function applyTheme(mode: string) {
    const actual = resolveActual(mode)
    document.documentElement.setAttribute('data-theme', actual)
  }

  function handleSwitch(mode: string) {
    themeMode.set(mode)
    localStorage.setItem('memcached-gui-theme', mode)
    applyTheme(mode)
  }

  const saved = localStorage.getItem('memcached-gui-theme')
  const initial = saved || 'system'
  themeMode.set(initial)
  applyTheme(initial)

  const mql = window.matchMedia('(prefers-color-scheme: dark)')
  function onSystemChange() {
    const current = localStorage.getItem('memcached-gui-theme') || 'system'
    if (current === 'system') {
      applyTheme('system')
    }
  }
  mql.addEventListener('change', onSystemChange)

  onDestroy(() => {
    mql.removeEventListener('change', onSystemChange)
  })

  const options = [
    { id: 'system', label: 'System', title: 'Follow system' },
    { id: 'light', label: 'Light', title: 'Light theme' },
    { id: 'dark', label: 'Dark', title: 'Dark theme' },
  ]
</script>

<div class="theme-toggle" role="group" aria-label="Theme mode">
  {#each options as opt}
    <button
      class="mode-btn"
      class:active={$themeMode === opt.id}
      on:click={() => handleSwitch(opt.id)}
      title={opt.title}
      aria-label={opt.title}
      type="button"
    >
      {opt.label}
    </button>
  {/each}
</div>

<style>
  .theme-toggle {
    display: flex;
    border: 1px solid var(--border-strong);
    border-radius: 8px;
    overflow: hidden;
    background: var(--bg-surface-soft);
  }

  .mode-btn {
    padding: 6px 12px;
    border: none;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    font-size: 12px;
    font-weight: 600;
    line-height: 1.2;
    min-width: 62px;
    transition: background 0.2s ease-out, color 0.2s ease-out;
  }

  .mode-btn:hover {
    color: var(--text-secondary);
    background: var(--bg-hover);
  }

  .mode-btn.active {
    background: var(--bg-active);
    color: var(--text-primary);
  }

  .mode-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  @media (prefers-reduced-motion: reduce) {
    .mode-btn {
      transition: none;
    }
  }
</style>
