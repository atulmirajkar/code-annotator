import { readPreference, writePreference } from "./browser-storage.js";

export type ViewerTheme = "light" | "dark";

const themePreferenceKey = "code-annotator.theme";
export const themeChangeEvent = "code-annotator:theme-change";

function isViewerTheme(value: string | null | undefined): value is ViewerTheme {
  return value === "light" || value === "dark";
}

export function effectiveTheme(document: Document, window: Window): ViewerTheme {
  if (document.documentElement.classList.contains("theme-dark")) return "dark";
  if (document.documentElement.classList.contains("theme-light")) return "light";
  return window.matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light";
}

export function bindThemeToggle(
  document: Document,
  window: Window,
  storage: Storage,
): void {
  const toggle = document.querySelector<HTMLButtonElement>(".theme-toggle");
  if (!toggle) return;

  const stored = readPreference(storage, themePreferenceKey);
  if (isViewerTheme(stored)) document.documentElement.classList.add(`theme-${stored}`);

  const media = window.matchMedia("(prefers-color-scheme: dark)");
  const updateToggle = (): void => {
    const current = effectiveTheme(document, window);
    const next = current === "dark" ? "light" : "dark";
    toggle.classList.toggle("theme-current-dark", current === "dark");
    toggle.setAttribute("aria-label", `Switch to ${next} theme`);
    toggle.title = `Switch to ${next} theme`;
    toggle.querySelector(".theme-icon-sun")?.toggleAttribute("hidden", current !== "light");
    toggle.querySelector(".theme-icon-moon")?.toggleAttribute("hidden", current !== "dark");
  };

  updateToggle();
  media.addEventListener("change", updateToggle);
  toggle.addEventListener("click", () => {
    const theme: ViewerTheme = effectiveTheme(document, window) === "dark"
      ? "light"
      : "dark";
    document.documentElement.classList.remove("theme-light", "theme-dark");
    document.documentElement.classList.add(`theme-${theme}`);
    writePreference(storage, themePreferenceKey, theme);
    updateToggle();
    window.dispatchEvent(new CustomEvent<ViewerTheme>(themeChangeEvent, { detail: theme }));
  });
}
