const PREFIX = 'memcached-gui-'

export function loadString(key: string): string | null {
  return localStorage.getItem(PREFIX + key)
}

export function saveString(key: string, value: string): void {
  localStorage.setItem(PREFIX + key, value)
}

export function loadJson<T>(key: string, fallback: T): T {
  const raw = localStorage.getItem(PREFIX + key)
  if (!raw) return fallback
  try {
    return JSON.parse(raw) as T
  } catch {
    return fallback
  }
}

export function saveJson<T>(key: string, value: T): void {
  localStorage.setItem(PREFIX + key, JSON.stringify(value))
}

export function remove(key: string): void {
  localStorage.removeItem(PREFIX + key)
}
