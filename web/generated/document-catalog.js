export function buildDocumentTree(documents) {
    const roots = [];
    documents.forEach((document) => {
        const segments = document.path.split("/");
        let siblings = roots;
        let parentKey = "";
        segments.forEach((segment, index) => {
            const directory = index !== segments.length - 1;
            const key = directory ? (parentKey ? `${parentKey}/${segment}` : segment) : document.path;
            let node = siblings.find((candidate) => candidate.key === key);
            if (!node) {
                node = { key, name: segment, directory, children: [], document: null };
                siblings.push(node);
            }
            if (directory)
                siblings = node.children;
            else
                node.document = document;
            parentKey = key;
        });
    });
    return roots;
}
export function filterDocuments(documents, queryValue, scope) {
    const query = queryValue.trim().toLocaleLowerCase();
    const matches = documents.filter((document) => {
        const pathMatches = !query || document.path.toLocaleLowerCase().includes(query);
        const scopeMatches = scope === "all"
            || (scope === "changed" && document.changed)
            || (scope === "open-comments" && document.openCommentCount > 0);
        return pathMatches && scopeMatches;
    });
    return {
        documents: matches,
        paths: new Set(matches.map((document) => document.path)),
        status: documentFilterStatus(query, scope, matches.length),
    };
}
export function documentFilterStatus(query, scope, count) {
    if (!query && scope === "all")
        return "";
    const descriptor = scope === "changed"
        ? (query ? "matching changed document" : "changed document")
        : scope === "open-comments"
            ? (query ? "matching document with open comments" : "document with open comments")
            : "matching document";
    const plural = descriptor.replace("document", "documents");
    return count === 0 ? `No ${plural}.` : `${count} ${count === 1 ? descriptor : plural}.`;
}
export function countDocumentsWithOpenComments(documents) {
    return documents.filter((document) => document.openCommentCount > 0).length;
}
export function hasChangedDocuments(documents) {
    return documents.some((document) => document.changed);
}
