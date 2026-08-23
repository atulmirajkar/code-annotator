export function badge(text, extraClass = "") {
  const item = element("span", `annotation-badge ${extraClass}`.trim());
  item.textContent = String(text).replaceAll("_", " ");
  return item;
}

export function element(tag, className) {
  const item = document.createElement(tag);
  item.className = className;
  return item;
}
