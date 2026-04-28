import type { OperationTab } from '../stores/app'
export type { OperationTab }

export type KeyboardHandler = (e: KeyboardEvent) => void

const ACTIVE_INPUT_TAGS = new Set(['INPUT', 'TEXTAREA', 'SELECT'])

export function isInputFocused(): boolean {
  const el = document.activeElement
  if (!el) return false
  if (ACTIVE_INPUT_TAGS.has(el.tagName)) return true
  if (el.getAttribute('contenteditable') === 'true') return true
  return false
}

export function isCtrlOrCmd(e: KeyboardEvent): boolean {
  return e.ctrlKey || e.metaKey
}

export function matchesShortcut(e: KeyboardEvent, key: string): boolean {
  return isCtrlOrCmd(e) && e.key.toLowerCase() === key.toLowerCase()
}

export interface ShortcutConfig {
  onTabSwitch?: (tab: OperationTab) => void
  onExecute?: () => void
  onClear?: () => void
}

const COMMAND_KEYS: OperationTab[] = ['get', 'set', 'delete', 'incr', 'decr', 'stats', 'flushall', 'version']

export function createShortcutHandler(config: ShortcutConfig): KeyboardHandler {
  return (e: KeyboardEvent) => {
    for (let i = 0; i < COMMAND_KEYS.length; i++) {
      if (matchesShortcut(e, String(i + 1)) && config.onTabSwitch) {
        e.preventDefault()
        config.onTabSwitch(COMMAND_KEYS[i])
        return
      }
    }

    if (matchesShortcut(e, 'enter') && config.onExecute) {
      if (isInputFocused()) {
        e.preventDefault()
        config.onExecute()
      }
    }

    if (matchesShortcut(e, 'l') && config.onClear) {
      if (!isInputFocused()) {
        e.preventDefault()
        config.onClear()
      }
    }
  }
}
