// Browser storage can be unavailable in privacy-restricted contexts. Viewer
// interactions remain functional when tab-local preferences cannot persist.
export function readPreference(storage: Storage, key: string): string | null {
  try {
    return storage.getItem(key);
  } catch (_) {
    return null;
  }
}

export function writePreference(
  storage: Storage,
  key: string,
  value: string,
): void {
  try {
    storage.setItem(key, value);
  } catch (_) {
    // The current-page interaction still works without persistence.
  }
}
