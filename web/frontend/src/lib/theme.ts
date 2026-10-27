/**
 * Theme manager — aligned byte-for-byte to watch-alert src/theme/theme.js and
 * haven-console's pinia theme store so all three apps share one preference.
 *
 * Contract (cross-app, do not fork):
 * - persisted key `hc_theme`, value `{"theme":"dark"|"light"}` (JSON, haven-console
 *   pinia persistedstate format) with legacy plain-string fallback;
 * - applied on <html> as a `.light` class (tokens live under `:root.light`);
 *   `.dark` is maintained too for Tailwind `dark:` variants;
 * - in-app toggles dispatch `hc-theme-change`; cross-tab via the `storage` event.
 */

export const THEME_KEY = "hc_theme"
export const THEME_DARK = "dark"
export const THEME_LIGHT = "light"

export type Theme = typeof THEME_DARK | typeof THEME_LIGHT

/** Read the persisted theme; null when absent. Accepts JSON {"theme"} and plain string. */
export function getStoredTheme(): Theme | null {
  if (typeof localStorage === "undefined") return null
  const raw = localStorage.getItem(THEME_KEY)
  if (!raw) return null
  if (raw === THEME_DARK || raw === THEME_LIGHT) return raw
  try {
    const obj = JSON.parse(raw) as { theme?: unknown }
    const t = obj?.theme
    return t === THEME_DARK || t === THEME_LIGHT ? (t as Theme) : null
  } catch {
    return null
  }
}

function getSystemPreference(): Theme {
  if (
    typeof window === "undefined" ||
    typeof window.matchMedia !== "function"
  ) {
    return THEME_DARK
  }
  return window.matchMedia("(prefers-color-scheme: light)").matches
    ? THEME_LIGHT
    : THEME_DARK
}

/** Resolve the effective theme: persisted → system preference → dark. */
export function resolveTheme(): Theme {
  return getStoredTheme() ?? getSystemPreference()
}

/** Apply the theme to <html> (`.light` for the token layer, `.dark` for Tailwind). */
export function applyTheme(
  theme: Theme,
  doc: Document | null = typeof document === "undefined" ? null : document,
): void {
  if (!doc) return
  doc.documentElement.classList.toggle("light", theme === THEME_LIGHT)
  doc.documentElement.classList.toggle("dark", theme !== THEME_LIGHT)
}

/** Persist in haven-console pinia format: JSON {"theme": ...}. */
export function persistTheme(theme: Theme): void {
  if (typeof localStorage === "undefined") return
  localStorage.setItem(THEME_KEY, JSON.stringify({ theme }))
}

/** Toggle: returns the new theme, applies it, persists it, broadcasts to this tab. */
export function toggleTheme(): Theme {
  const next = resolveTheme() === THEME_DARK ? THEME_LIGHT : THEME_DARK
  applyTheme(next)
  persistTheme(next)
  if (typeof window !== "undefined") {
    window.dispatchEvent(new Event("hc-theme-change"))
  }
  return next
}

/** App bootstrap: resolve + apply (idempotent). Call on mount of the shell. */
export function initTheme(): Theme {
  const theme = resolveTheme()
  applyTheme(theme)
  return theme
}

/** Subscribe to theme changes from this tab (hc-theme-change) and other tabs (storage). */
export function watchTheme(onChange: () => void): () => void {
  if (typeof window === "undefined") return () => {}
  window.addEventListener("hc-theme-change", onChange)
  window.addEventListener("storage", onChange)
  return () => {
    window.removeEventListener("hc-theme-change", onChange)
    window.removeEventListener("storage", onChange)
  }
}
