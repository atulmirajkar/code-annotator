"use strict";
// Restore an explicit theme before CSS is loaded so a full-page navigation
// never paints the operating-system theme first. This file intentionally has
// no imports or exports: it is a blocking classic script in the head.
(() => {
    try {
        const root = document.documentElement;
        const theme = sessionStorage.getItem("code-annotator.theme");
        if (theme === "light" || theme === "dark") {
            root.classList.add(theme === "dark" ? "theme-dark" : "theme-light");
        }
    }
    catch (_) {
        // Storage can be unavailable in privacy-restricted contexts. In that case
        // the stylesheet safely uses the operating-system default.
    }
})();
