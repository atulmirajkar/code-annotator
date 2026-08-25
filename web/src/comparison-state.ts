export interface ComparisonOption {
  commit: string;
  commitShort: string;
  subject?: string;
}

export interface ComparisonState {
  activeCommit: string;
  activeShort: string;
  requestedBase: string;
  options: ReadonlyArray<ComparisonOption>;
}

function record(value: unknown, label: string): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new Error(`${label} must be an object`);
  return value as Record<string, unknown>;
}

function text(value: unknown, label: string): string {
  if (typeof value !== "string") throw new Error(`${label} must be a string`);
  return value;
}

function commit(value: unknown, label: string): string {
  const result = text(value, label);
  if (!/^[0-9a-f]{40}$/.test(result)) throw new Error(`${label} must be a full lowercase commit`);
  return result;
}

export function parseComparisonState(value: unknown): ComparisonState {
  const root = record(value, "comparison state");
  if (!Array.isArray(root.options)) throw new Error("comparison options must be an array");
  const options = root.options.map((candidate, index): ComparisonOption => {
    const option = record(candidate, `options[${index}]`);
    const subject = option.subject === undefined ? undefined : text(option.subject, `options[${index}].subject`);
    return {
      commit: commit(option.commit, `options[${index}].commit`),
      commitShort: text(option.commitShort, `options[${index}].commitShort`),
      ...(subject === undefined ? {} : { subject }),
    };
  });
  if (new Set(options.map((option) => option.commit)).size !== options.length) throw new Error("comparison option commits must be unique");
  return {
    activeCommit: commit(root.activeCommit, "activeCommit"),
    activeShort: text(root.activeShort, "activeShort"),
    requestedBase: text(root.requestedBase, "requestedBase"),
    options,
  };
}
