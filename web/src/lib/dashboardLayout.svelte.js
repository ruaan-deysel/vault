// Customizable Dashboard layout state (issue: Dashboard 5A / E13).
//
// Manages the ordered list of visible tile ids plus edit mode, persisting the
// order to localStorage under our own key. The 5A layout is simply the default
// order. Unknown / duplicate ids are dropped on load so a stale or hand-edited
// value can never render a broken tile.

const KEY = 'vault:dash:layout:v1'

// Default-on tiles = the fixed "5A" layout, in order.
export const DEFAULT_LAYOUT = [
  'health', 'protected', 'nextrun', 'lastbackup',
  'threetwoone', 'progress', 'activity', 'jobs', 'protection',
]

/**
 * @param {string[]} validIds - every tile id the catalog knows about; ids not
 *   in this list are ignored when loading persisted state.
 */
export function createDashboardLayout(validIds) {
  // Plain arrays (not Set) — these are throwaway locals, not reactive state.
  const isValid = (id) => validIds.includes(id)

  function load() {
    try {
      const raw = localStorage.getItem(KEY)
      if (!raw) return [...DEFAULT_LAYOUT]
      const arr = JSON.parse(raw)
      if (!Array.isArray(arr)) return [...DEFAULT_LAYOUT]
      const cleaned = []
      for (const id of arr) {
        if (typeof id === 'string' && isValid(id) && !cleaned.includes(id)) {
          cleaned.push(id)
        }
      }
      return cleaned.length ? cleaned : [...DEFAULT_LAYOUT]
    } catch {
      return [...DEFAULT_LAYOUT]
    }
  }

  let order = $state(load())
  let editMode = $state(false)

  function persist() {
    try { localStorage.setItem(KEY, JSON.stringify(order)) } catch { /* ignore */ }
  }

  return {
    get order() { return order },
    get editMode() { return editMode },

    toggleEdit() { editMode = !editMode },

    add(id) {
      if (isValid(id) && !order.includes(id)) {
        order = [...order, id]
        persist()
      }
    },

    remove(id) {
      order = order.filter((x) => x !== id)
      persist()
    },

    // Move the tile at index `from` to index `to` (drag & drop).
    move(from, to) {
      if (from === to || from < 0 || to < 0 || from >= order.length || to >= order.length) return
      const next = [...order]
      const [moved] = next.splice(from, 1)
      next.splice(to, 0, moved)
      order = next
      persist()
    },

    // Nudge a tile one slot up (-1) or down (+1) — the keyboard / touch path.
    moveBy(id, dir) {
      const i = order.indexOf(id)
      if (i < 0) return
      const j = i + dir
      if (j < 0 || j >= order.length) return
      const next = [...order]
      ;[next[i], next[j]] = [next[j], next[i]]
      order = next
      persist()
    },

    reset() {
      order = [...DEFAULT_LAYOUT]
      persist()
    },
  }
}
