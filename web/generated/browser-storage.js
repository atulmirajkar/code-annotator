// Browser storage can be unavailable in privacy-restricted contexts. Viewer
// interactions remain functional when tab-local preferences cannot persist.
export function readPreference(storage, key) {
    try {
        return storage.getItem(key);
    }
    catch (_) {
        return null;
    }
}
export function writePreference(storage, key, value) {
    try {
        storage.setItem(key, value);
    }
    catch (_) {
        // The current-page interaction still works without persistence.
    }
}
