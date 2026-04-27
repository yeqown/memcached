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

export type OperationTab = 'get' | 'set' | 'delete' | 'stats'

export interface ShortcutConfig {
  onTabSwitch?: (tab: OperationTab) => void
  onExecute?: () => void
  onClear?: () => void
}

export function createShortcutHandler(config: ShortcutConfig): KeyboardHandler {
  return (e: KeyboardEvent) => {
    if (matchesShortcut(e, '1') && config.onTabSwitch) {
      e.preventDefault()
      config.onTabSwitch('get')
    } else if (matchesShortcut(e, '2') && config.onTabSwitch) {
      e.preventDefault()
      config.onTabSwitch('set')
    } else if (matchesShortcut(e, '3') && config.onTabSwitch) {
      e.preventDefault()
      config.onTabSwitch('delete')
    } else if (matchesShortcut(e, '4') && config.onTabSwitch) {
      e.preventDefault()
      config.onTabSwitch('stats')
    } else if (matchesShortcut(e, 'enter') && config.onExecute) {
      if (isInputFocused()) {
        e.preventDefault()
        config.onExecute()
      }
    } else if (matchesShortcut(e, 'l') && config.onClear) {
      if (!isInputFocused()) {
        e.preventDefault()
        config.onClear()
      }
    }
  }
}
