# GUI UX & Style Enhancement Design

## Context

Memcached GUI built with Wails + Svelte. Used primarily for development debugging (mixed operations: query, batch exploration, modify-verify). Current pain points: low input efficiency, poor result display, cumbersome tab switching, lack of overview.

**Design direction:** Panel-Driven — retain existing layout structure, enhance in four areas.

## 1. Input Efficiency

### Key History Panel

- New `KeyHistoryPanel.svelte` component, placed right of OperationPanel input area
- Shows recent keys (max 50), most recent first, persisted to localStorage
- Click to populate input field
- Clear button to wipe history
- Global store: `keyHistory` writable store, auto-updates on every Get/Set/Delete operation

### Auto-completion

- Input fields show dropdown (max 5 matches) filtered from key history
- Keyboard navigation: Arrow Up/Down to select, Enter/Tab to confirm, Escape to dismiss
- Trigger after 1+ character typed

### Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| `Ctrl+1/2/3/4` | Switch to Get/Set/Delete/Stats tab |
| `Ctrl+Enter` | Execute current operation |
| `Ctrl+L` | Clear result area |

Global keydown listener in `App.svelte`, only active when no input/textarea is focused (except for Ctrl+Enter which works in inputs).

## 2. Result Display

### JSON Search & Filter

- Search input above JSON tree area in `ValueDisplay.svelte`
- Filters tree nodes by matching key or value (case-insensitive)
- Collapsed paths auto-expand on match

### Expand/Collapse All

- "Expand All" / "Collapse All" toggle button next to search

### Copy Enhancements

- Copy button dropdown: "Raw Value" / "Formatted JSON" / "Current Path"
- Current path display when hovering JSON tree node

### Syntax Highlighting

Already good, no change needed for color tokens.

## 3. Operation Switching

### Input State Preservation

- Each tab (get, set, delete) maintains its own form state independently
- Use separate reactive variables per tab (already partially done), ensure no state loss on tab switch

### Tab Memory

- Persist `activeOperationTab` to localStorage
- Restore on app startup

## 4. Information Overview

### ConnectionBanner Enhancement

- After connecting, auto-fetch stats once, then refresh every 30s
- Display compact metrics right of connection name: `keys: 1,234 | hits: 89% | mem: 128MB/512MB`
- New derived store: `connectionStats` to hold parsed stats data

### Stats Visualization

- When Stats tab returns results, show metric cards above the raw JSON:
  - Memory: `bytes_used / limit_maxbytes` with progress bar
  - Hit rate: `get_hits / (get_hits + get_misses)` percentage
  - Connections: `curr_connections` number
- Cards use existing CSS variables, compact layout (3 columns)

## 5. Visual Style

### Color Palette Adjustment

Dark theme background shift toward terminal black:
- `--bg-primary`: `#1a2332` → `#0a0e14`
- `--bg-sidebar`: `#0f172a` → `#0d1117`
- `--bg-surface`: `#1e293b` → `#0f1923`

Border refinement:
- `--border`: `#1e293b` → `#1a2332` (subtler)
- `--border-strong`: `#334155` → `#2a3545`

### Spacing

- Reduce padding in panels from 16px to 12px
- Tighter form row gaps (8px → 6px)
- Tab padding: 6px 16px → 4px 12px

### Font

- All UI text remains system font stack
- Monospace areas (log, value display) explicitly use `'JetBrains Mono', 'SF Mono', 'Monaco', 'Menlo', 'Consolas', monospace`

## Scope

In scope:
- Key history panel with localStorage persistence
- Auto-completion on input fields
- Keyboard shortcuts
- JSON search/filter in ValueDisplay
- Expand/collapse all for JSON tree
- Copy options (raw/formatted/path)
- Tab input state preservation + tab memory
- ConnectionBanner stats display (periodic refresh)
- Stats metric cards
- Dark theme color/spacing refinement

Out of scope:
- New operations (TTL touch, CAS update, etc.)
- Multi-server cluster management
- Data import/export
- Unit test changes (unless directly related)
