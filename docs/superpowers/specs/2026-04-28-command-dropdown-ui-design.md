# Command-Based UI Design

## Context

Memcached GUI currently uses tab-based navigation (Get/Set/Delete) which only supports limited operations. User wants a command dropdown selector that dynamically shows appropriate input fields based on the selected command.

**Backend supported commands:**
- Storage: Get, Set, Delete
- Counter: Incr, Decr
- Admin: Stats, FlushAll, Version

**Stats handling:** ConnectionBanner continues periodic refresh (compact metrics), while Stats command in dropdown allows manual execution for full JSON view.

## 1. Command Organization

Grouped dropdown selector with three categories:

```
Storage Operations
  - Get (key)
  - Set (key, value, flags, expiry)
  - Delete (key, confirmation)

Counter Operations
  - Incr (key, delta)
  - Decr (key, delta)

Admin Operations
  - Stats (no input, full JSON output)
  - FlushAll (no input, confirmation)
  - Version (no input)
```

## 2. Input Requirements Per Command

| Command | Input Fields | Notes |
|---------|--------------|-------|
| Get | Key | Returns value + meta (TTL, CAS, flags, size) |
| Set | Key, Value, Flags, Expiry | Value is textarea, Flags/Expiry are numbers |
| Delete | Key | Requires confirmation before execution |
| Incr | Key, Delta | Delta defaults to 1 |
| Decr | Key, Delta | Delta defaults to 1 |
| Stats | None | Outputs full JSON statistics |
| FlushAll | None | Requires confirmation before execution |
| Version | None | Outputs version string |

## 3. Dynamic Input Area

Show only relevant input fields for selected command:

- **Storage commands with key:** Key input field (with autocomplete from history)
- **Set:** Additional textarea for value, number inputs for flags/expiry
- **Counter commands:** Key input + Delta number input (default 1)
- **Admin without input:** Execute button directly
- **Dangerous operations (Delete, FlushAll):** Confirmation dialog before execution

## 4. Component Structure

### CommandSelector.svelte
- Custom dropdown with grouped options
- Search/filter within dropdown
- Keyboard navigation support
- macOS native styling (accent colors, rounded corners)

### InputArea.svelte
- Dynamic form rendering based on selected command
- Uses `class:hidden` for instant field visibility change (no component recreation)
- Key input uses KeyInput.svelte (autocomplete from history)
- Value textarea with monospace font
- Number inputs for Flags, Expiry, Delta

### OperationPanel.svelte (simplified)
- CommandSelector dropdown
- InputArea (dynamic)
- Execute button
- ConnectionBanner above (unchanged)

## 5. UI Layout

```
┌─────────────────────────────────────────────────────┐
│ ConnectionBanner (compact stats, periodic refresh)  │
├─────────────────────────────────────────────────────┤
│ [Command Dropdown ▼]          [Theme Toggle]        │
├─────────────────────────────────────────────────────┤
│ Input Area (dynamic based on command):              │
│   Key: [_______________]                            │
│   Value/Delta/... (as needed)                       │
│   [Execute]                                         │
├─────────────────────────────────────────────────────┤
│ ValueDisplay (results, JSON tree)                   │
├─────────────────────────────────────────────────────┤
│ ProtocolLog                                         │
└─────────────────────────────────────────────────────┤
```

## 6. Command Definition

```typescript
interface CommandDef {
  id: string
  label: string
  group: 'storage' | 'counter' | 'admin'
  inputs: InputField[]
  needsConfirmation: boolean
}

interface InputField {
  id: string
  label: string
  type: 'text' | 'textarea' | 'number'
  placeholder?: string
  defaultValue?: string | number
  required: boolean
}

const COMMANDS: CommandDef[] = [
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
```

## 7. Keyboard Shortcuts Update

- `Ctrl+1-9`: Quick command switch (Get=1, Set=2, Delete=3, Incr=4, Decr=5, Stats=6, FlushAll=7, Version=8)
- `Ctrl+Enter`: Execute current command
- `Ctrl+L`: Clear result area

## 8. Files to Create/Modify

**Create:**
- `gui/frontend/src/components/CommandSelector.svelte` — Grouped dropdown
- `gui/frontend/src/components/InputArea.svelte` — Dynamic input renderer

**Modify:**
- `gui/frontend/src/components/OperationPanel.svelte` — Replace tabs with CommandSelector + InputArea
- `gui/frontend/src/stores/app.ts` — Update OperationTab type with all command IDs
- `gui/frontend/src/lib/keyboard.ts` — Update shortcuts for 8 commands
- `gui/frontend/src/App.svelte` — Ensure result area shows for all commands (not just get)

## 9. Scope

**In scope:**
- Command dropdown with grouped categories
- Dynamic input fields per command
- Confirmation dialogs for Delete and FlushAll
- Keyboard shortcuts for quick command switching
- Stats command shows full JSON in ValueDisplay

**Out of scope:**
- Adding new backend commands
- Changes to ConnectionBanner periodic stats
- Changes to ValueDisplay component structure
- ProtocolLog changes