export interface InputField {
  id: string
  label: string
  type: 'text' | 'textarea' | 'number'
  placeholder?: string
  defaultValue?: string | number
  required: boolean
}

export interface CommandDef {
  id: string
  label: string
  group: 'storage' | 'counter' | 'admin'
  inputs: InputField[]
  needsConfirmation: boolean
}

export const COMMANDS: CommandDef[] = [
  { id: 'get', label: 'Get', group: 'storage', inputs: [{ id: 'key', label: 'Key', type: 'text', required: true }], needsConfirmation: false },
  { id: 'set', label: 'Set', group: 'storage', inputs: [
    { id: 'key', label: 'Key', type: 'text', required: true },
    { id: 'value', label: 'Value', type: 'textarea', required: true },
    { id: 'flags', label: 'Flags', type: 'number', defaultValue: 0, required: false },
    { id: 'expiry', label: 'Expiry (s)', type: 'number', defaultValue: 0, required: false },
  ], needsConfirmation: false },
  { id: 'delete', label: 'Delete', group: 'storage', inputs: [{ id: 'key', label: 'Key', type: 'text', required: true }], needsConfirmation: true },
  { id: 'incr', label: 'Incr', group: 'counter', inputs: [
    { id: 'key', label: 'Key', type: 'text', required: true },
    { id: 'delta', label: 'Delta', type: 'number', defaultValue: 1, required: true },
  ], needsConfirmation: false },
  { id: 'decr', label: 'Decr', group: 'counter', inputs: [
    { id: 'key', label: 'Key', type: 'text', required: true },
    { id: 'delta', label: 'Delta', type: 'number', defaultValue: 1, required: true },
  ], needsConfirmation: false },
  { id: 'stats', label: 'Stats', group: 'admin', inputs: [], needsConfirmation: false },
  { id: 'flushall', label: 'FlushAll', group: 'admin', inputs: [], needsConfirmation: true },
  { id: 'version', label: 'Version', group: 'admin', inputs: [], needsConfirmation: false },
]

export const COMMAND_GROUPS = [
  { id: 'storage', label: 'Storage Operations' },
  { id: 'counter', label: 'Counter Operations' },
  { id: 'admin', label: 'Admin Operations' },
]

export function getCommand(id: string): CommandDef | undefined {
  return COMMANDS.find(c => c.id === id)
}

export type CommandId = 'get' | 'set' | 'delete' | 'incr' | 'decr' | 'stats' | 'flushall' | 'version'
