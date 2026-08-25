function requireRecord(value, label) {
    if (typeof value !== "object" || value === null || Array.isArray(value))
        throw new Error(`${label} must be an object`);
    return value;
}
function requireString(value, label) {
    if (typeof value !== "string")
        throw new Error(`${label} must be a string`);
    return value;
}
function requireBoolean(value, label) {
    if (typeof value !== "boolean")
        throw new Error(`${label} must be a boolean`);
    return value;
}
function requireInteger(value, label) {
    if (typeof value !== "number" || !Number.isInteger(value) || value < 0)
        throw new Error(`${label} must be a non-negative integer`);
    return value;
}
function requireMember(value, allowed, label) {
    const match = allowed.find((candidate) => candidate === value);
    if (match === undefined)
        throw new Error(`${label} is invalid`);
    return match;
}
function parseItem(value, index) {
    const item = requireRecord(value, `documents[${index}]`);
    const path = requireString(item.path, `documents[${index}].path`);
    const name = requireString(item.name, `documents[${index}].name`);
    const directory = requireString(item.directory, `documents[${index}].directory`);
    const url = requireString(item.url, `documents[${index}].url`);
    if (!path || !name)
        throw new Error("document path and name must not be empty");
    if (path.startsWith("/") || path.includes("\\") || path.split("/").some((segment) => segment === "" || segment === "." || segment === "..")) {
        throw new Error("document path is invalid");
    }
    if (directory !== (path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : ""))
        throw new Error("document directory is inconsistent");
    if (!url.startsWith("/view/"))
        throw new Error("document URL is invalid");
    return {
        path,
        name,
        directory,
        kind: requireMember(item.kind, ["markdown", "code"], `documents[${index}].kind`),
        url,
        selected: requireBoolean(item.selected, `documents[${index}].selected`),
        changed: requireBoolean(item.changed, `documents[${index}].changed`),
        openCommentCount: requireInteger(item.openCommentCount, `documents[${index}].openCommentCount`),
    };
}
export function parseDocumentCatalogState(value) {
    const root = requireRecord(value, "document state");
    if (root.schemaVersion !== 1)
        throw new Error("document state schemaVersion is unsupported");
    if (!Array.isArray(root.documents))
        throw new Error("documents must be an array");
    const selectedPath = root.selectedPath === null ? null : requireString(root.selectedPath, "selectedPath");
    const changedAvailable = requireBoolean(root.changedAvailable, "changedAvailable");
    const changedError = requireBoolean(root.changedError, "changedError");
    if (changedAvailable && changedError)
        throw new Error("changed state cannot be available and failed");
    const documents = root.documents.map(parseItem);
    const documentsByPath = new Map();
    documents.forEach((item) => {
        if (documentsByPath.has(item.path))
            throw new Error("document paths must be unique");
        documentsByPath.set(item.path, item);
    });
    const selected = documents.filter((item) => item.selected);
    if (selectedPath === null ? selected.length !== 0 : selected.length !== 1 || selected[0]?.path !== selectedPath) {
        throw new Error("selected document state is inconsistent");
    }
    if (!changedAvailable && documents.some((item) => item.changed))
        throw new Error("changed documents require available changed state");
    return {
        schemaVersion: 1,
        selectedPath,
        mode: requireMember(root.mode, ["file", "diff"], "mode"),
        changedAvailable,
        changedError,
        reviewAvailable: requireBoolean(root.reviewAvailable, "reviewAvailable"),
        documents,
        documentsByPath,
    };
}
export async function fetchDocumentCatalogState(documentPath = "", mode = "file") {
    const query = new URLSearchParams();
    if (documentPath)
        query.set("document", documentPath);
    if (mode === "diff")
        query.set("mode", mode);
    const response = await fetch(`/ui/document-state?${query.toString()}`, { headers: { Accept: "application/json" } });
    if (!response.ok)
        throw new Error(`document state request failed: ${response.status}`);
    const payload = await response.json();
    return parseDocumentCatalogState(payload);
}
