export interface DocumentTreeNode {
  key: string;
  name: string;
  directory: boolean;
  element: HTMLLIElement;
  children: DocumentTreeNode[];
  fileItem?: HTMLLIElement;
}

const documentTreeStorageKey = "code-annotator.document-tree-expanded";

export function buildDocumentTree(
  list: HTMLUListElement,
  fileItems: HTMLLIElement[],
  readPreference: (key: string) => string | null,
  writePreference: (key: string, value: string) => void,
): DocumentTreeNode[] {
  const roots: DocumentTreeNode[] = [];
  const directories = new Map<string, DocumentTreeNode>();
  list.replaceChildren();
  for (const fileItem of fileItems) {
    const path = fileItem.dataset.documentPath || "";
    const segments = path.split("/").filter(Boolean);
    let siblings = roots;
    let parentKey = "";
    segments.forEach((segment, index) => {
      const isFile = index === segments.length - 1;
      const key = isFile ? path : (parentKey ? `${parentKey}/${segment}` : segment);
      let node = siblings.find((candidate) => candidate.key === key);
      if (!node) {
        const element = document.createElement("li");
        node = { key, name: segment, directory: !isFile, element, children: [] };
        siblings.push(node);
        if (!isFile) directories.set(key, node);
      }
      if (isFile) node.fileItem = fileItem;
      if (!isFile) siblings = node.children;
      parentKey = key;
    });
  }
  renderDocumentTree(list, roots, directories, readPreference, writePreference);
  return roots;
}

export function updateTreeVisibility(nodes: DocumentTreeNode[], filtering: boolean): boolean {
  // A file's filterMatch is written by viewer.ts. This recursive pass returns
  // whether each subtree contains a matching file, hiding empty directories
  // while preserving the directory structure for matches.
  let anyVisible = false;
  nodes.forEach((node) => {
    if (node.directory) {
      const childVisible = updateTreeVisibility(node.children, filtering);
      node.element.hidden = filtering && !childVisible;
      anyVisible = anyVisible || childVisible;
    } else if (node.fileItem) {
      const visible = node.fileItem.dataset.filterMatch === "true";
      node.fileItem.hidden = !visible;
      anyVisible = anyVisible || visible;
    }
  });
  return anyVisible;
}

function renderDocumentTree(
  list: HTMLUListElement,
  nodes: DocumentTreeNode[],
  directories: Map<string, DocumentTreeNode>,
  readPreference: (key: string) => string | null,
  writePreference: (key: string, value: string) => void,
): void {
  const storedExpansion = readPreference(documentTreeStorageKey);
  const expanded = storedExpansion === null
    ? new Set(directories.keys())
    : readExpandedDirectories(storedExpansion);
  const render = (parent: HTMLElement, children: DocumentTreeNode[]): void => {
    children.forEach((node) => {
      if (node.directory) {
        node.element.className = "document-directory";
        const button = document.createElement("button");
        button.type = "button";
        button.className = "document-directory-toggle";
        button.textContent = node.name;
        button.setAttribute("aria-expanded", String(expanded.has(node.key)));
        button.addEventListener("click", () => {
          const isExpanded = !expanded.has(node.key);
          if (isExpanded) expanded.add(node.key); else expanded.delete(node.key);
          writePreference(documentTreeStorageKey, JSON.stringify(Array.from(expanded).sort()));
          button.setAttribute("aria-expanded", String(isExpanded));
          node.element.classList.toggle("collapsed", !isExpanded);
        });
        node.element.append(button);
        const childList = document.createElement("ul");
        childList.className = "document-tree-children";
        node.element.classList.toggle("collapsed", !expanded.has(node.key));
        node.element.append(childList);
        render(childList, node.children);
      } else if (node.fileItem) {
        node.fileItem.classList.add("document-file");
        parent.append(node.fileItem);
        return;
      }
      parent.append(node.element);
    });
  };
  render(list, nodes);
}

function readExpandedDirectories(stored: string): Set<string> {
  try {
    const values = JSON.parse(stored) as unknown;
    return new Set(Array.isArray(values) ? values.filter((value): value is string => typeof value === "string") : []);
  } catch (_) {
    return new Set();
  }
}
