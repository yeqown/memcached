import { writable, derived } from 'svelte/store'

export interface McServer {
  host: string
  port: number
}

export interface McContext {
  id: string
  name: string
  servers: McServer[]
}

export interface LogEntry {
  time: string
  op: string
  key?: string
  status: 'success' | 'error' | 'info'
  message: string
}

export interface QueryResult {
  success: boolean
  data: string
  error?: string
  key?: string
  value?: string
  ttl?: number
  lastAccessedTime?: number
  cas?: number
  flags?: number
  size?: number
  hitBefore?: boolean
  opaque?: number
  valueKind?: string
}

export type ConnectionStatus = 'disconnected' | 'connected' | 'error'
export type DisplayMode = 'text' | 'json' | 'meta'
export type ThemeMode = 'system' | 'light' | 'dark'
export type OperationTab = 'get' | 'set' | 'delete' | 'stats'

// Context list
export const contexts = writable<McContext[]>([])
export const activeContextId = writable<string | null>(null)
export const activeContext = derived([activeContextId, contexts], ([$id, $ctxs]) => {
  if (!$id) return null
  return $ctxs.find(c => c.id === $id) || null
})

// Connection state
export const connected = writable(false)
export const connectionStatus = writable<ConnectionStatus>('disconnected')
export const connectionError = writable<string>('')
export const activeContextName = writable<string>('')

// Operation log
export const logs = writable<LogEntry[]>([])

// Display value (legacy compat, will be replaced by queryResult)
export const displayValue = writable<string>('')
export const displayMode = writable<DisplayMode>('text')

// Structured query result
export const queryResult = writable<QueryResult | null>(null)
export const activeOperationTab = writable<OperationTab>('get')

// Theme
export const themeMode = writable<ThemeMode>('system')

// Helpers
export function addLog(entry: Omit<LogEntry, 'time'>) {
  const now = new Date()
  const time = now.toLocaleTimeString()
  logs.update(v => [{ ...entry, time }, ...v].slice(0, 200))
}
