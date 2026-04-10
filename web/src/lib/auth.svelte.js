/**
 * Auth module — API key can be configured via Settings > Security.
 * Exports are used by components that need to know if auth is active.
 */

export function getApiKey() {
  return ''
}

export function isAuthenticated() {
  return true
}

export async function checkAuthStatus() {
  // No-op: auth status is driven by the API key settings endpoint.
}
