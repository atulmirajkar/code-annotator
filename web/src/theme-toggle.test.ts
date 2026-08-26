// @vitest-environment happy-dom

import { beforeEach, describe, expect, it, vi } from "vitest";

import { bindThemeToggle, effectiveTheme, themeChangeEvent } from "./theme-toggle.js";

function mediaQuery(matches: boolean): MediaQueryList {
  return {
    matches,
    media: "(prefers-color-scheme: dark)",
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  };
}

describe("theme toggle", () => {
  beforeEach(() => {
    document.documentElement.classList.remove("theme-light", "theme-dark");
    document.body.innerHTML = `<button type="button" class="theme-toggle">
      <span class="theme-icon-sun"></span><span class="theme-icon-moon"></span>
    </button>`;
    window.sessionStorage.clear();
  });

  it("follows the system theme until the user chooses an override", () => {
    vi.spyOn(window, "matchMedia").mockReturnValue(mediaQuery(true));
    bindThemeToggle(document, window, window.sessionStorage);

    const toggle = document.querySelector<HTMLButtonElement>(".theme-toggle")!;
    expect(effectiveTheme(document, window)).toBe("dark");
    expect(toggle.getAttribute("aria-label")).toBe("Switch to light theme");

    toggle.click();
    expect(document.documentElement.classList.contains("theme-light")).toBe(true);
    expect(window.sessionStorage.getItem("code-annotator.theme")).toBe("light");
    expect(toggle.getAttribute("aria-label")).toBe("Switch to dark theme");
  });

  it("restores a saved choice and announces changes", () => {
    vi.spyOn(window, "matchMedia").mockReturnValue(mediaQuery(false));
    window.sessionStorage.setItem("code-annotator.theme", "dark");
    const changed = vi.fn();
    window.addEventListener(themeChangeEvent, changed, { once: true });

    bindThemeToggle(document, window, window.sessionStorage);
    const toggle = document.querySelector<HTMLButtonElement>(".theme-toggle")!;
    expect(effectiveTheme(document, window)).toBe("dark");
    expect(toggle.querySelector(".theme-icon-sun")?.hasAttribute("hidden")).toBe(true);

    toggle.click();
    expect(changed).toHaveBeenCalledOnce();
    expect(document.documentElement.classList.contains("theme-light")).toBe(true);
  });
});
