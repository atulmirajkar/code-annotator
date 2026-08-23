export function badge(text: string, extraClass = ""): HTMLSpanElement {
  const item = element("span", `annotation-badge ${extraClass}`.trim());
  item.textContent = String(text).replaceAll("_", " ");
  return item;
}

export function element<K extends keyof HTMLElementTagNameMap>(tag: K, className: string): HTMLElementTagNameMap[K] {
  const item = document.createElement(tag);
  item.className = className;
  return item;
}
