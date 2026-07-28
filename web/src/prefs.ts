// What the browser remembers between visits.
//
// All of it is per-device convenience: losing an entry costs you one redirect
// and never any data, so every read tolerates a value that is missing,
// unreadable, or stale. localStorage itself throws in some privacy modes, which
// is why nothing here touches it without a guard.

const SCOPE_KEY = 'geetproject.lastScope'
const VIEW_KEY = 'geetproject.lastView'

/** Remembering the last scope makes a bare visit to "/" land where you left off. */
export function lastScope(fallback: string): string {
  return read(SCOPE_KEY) || fallback
}

export function rememberScope(scope: string): void {
  write(SCOPE_KEY, scope)
}

/**
 * The view last looked at in each scope, keyed by scope slug, so returning to a
 * project reopens the tab you left it on rather than always the first one.
 *
 * Returns '' when the scope has no record. Callers must also cope with a slug
 * that no longer exists - a view can be renamed or deleted long after we wrote
 * it down - so treat the result as a preference, not a destination.
 */
export function lastView(scope: string): string {
  return readMap(VIEW_KEY)[scope] ?? ''
}

export function rememberView(scope: string, view: string): void {
  const map = readMap(VIEW_KEY)
  if (map[scope] === view) return
  map[scope] = view
  write(VIEW_KEY, JSON.stringify(map))
}

function read(key: string): string {
  try {
    return localStorage.getItem(key) ?? ''
  } catch {
    return ''
  }
}

function write(key: string, value: string): void {
  try {
    localStorage.setItem(key, value)
  } catch {
    // A blocked or full store just means we forget. Nothing worth failing over.
  }
}

/** Parses a stored slug map, discarding anything that is not the shape we wrote. */
function readMap(key: string): Record<string, string> {
  let parsed: unknown
  try {
    parsed = JSON.parse(read(key) || '{}')
  } catch {
    return {}
  }
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return {}
  const out: Record<string, string> = {}
  for (const [scope, view] of Object.entries(parsed)) {
    if (typeof view === 'string') out[scope] = view
  }
  return out
}
