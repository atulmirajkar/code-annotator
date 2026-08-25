function browserHTMX() {
    return Reflect.get(globalThis, "htmx") ?? null;
}
export function defaultViewerEnvironment() {
    return {
        document,
        window,
        location,
        storage: sessionStorage,
        resizeObserver: ResizeObserver,
        htmx: browserHTMX(),
    };
}
