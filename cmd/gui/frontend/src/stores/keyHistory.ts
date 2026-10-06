import { writable, get } from 'svelte/store'
import { loadJson, saveJson } from '../lib/storage'

const MAX_HISTORY = 50
const STORAGE_KEY = 'key-history'

function createKeyHistoryStore() {
  const initial = loadJson<string[]>(STORAGE_KEY, [])
  const { subscribe, update, set } = writable<string[]>(initial)

  function add(key: string) {
    if (!key.trim()) return
    update(list => {
      const filtered = list.filter(k => k !== key)
      const updated = [key, ...filtered].slice(0, MAX_HISTORY)
      saveJson(STORAGE_KEY, updated)
      return updated
    })
  }

  function clear() {
    set([])
    saveJson(STORAGE_KEY, [])
  }

  function filterByPrefix(prefix: string, limit = 5): string[] {
    if (!prefix) return []
    const list = get({ subscribe })
    return list
      .filter(k => k.toLowerCase().includes(prefix.toLowerCase()))
      .slice(0, limit)
  }

  return { subscribe, add, clear, filterByPrefix }
}

export const keyHistory = createKeyHistoryStore()