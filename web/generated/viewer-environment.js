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
        requestAnimationFrame: (callback) => window.requestAnimationFrame(callback),
        htmx: browserHTMX(),
    };
}
