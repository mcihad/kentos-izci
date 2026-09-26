/** One line of a hunk with its numbers on the old and new side. */
export type DiffLine = {
  kind: "context" | "add" | "del";
  text: string;
  oldNo?: number;
  newNo?: number;
};

export type DiffHunk = {
  /** "old,new" start lines; unique within a file. */
  id: string;
  header: string;
  lines: DiffLine[];
};

export type DiffFile = {
  path: string;
  status: "added" | "deleted" | "modified";
  binary: boolean;
  hunks: DiffHunk[];
  additions: number;
  deletions: number;
};

const octal = /\\([0-7]{3})|\\(.)/g;

/** Decodes a C-style quoted path as git writes non-ASCII file names. */
function unquote(p: string): string {
  if (!p.startsWith('"') || !p.endsWith('"')) return p;
  const bytes: number[] = [];
  const body = p.slice(1, -1);
  let last = 0;
  const push = (s: string) => {
    for (const b of new TextEncoder().encode(s)) bytes.push(b);
  };
  for (const m of body.matchAll(octal)) {
    push(body.slice(last, m.index));
    if (m[1]) bytes.push(Number.parseInt(m[1], 8));
    else push(({ n: "\n", t: "\t", r: "\r" } as Record<string, string>)[m[2] ?? ""] ?? m[2] ?? "");
    last = (m.index ?? 0) + m[0].length;
  }
  push(body.slice(last));
  return new TextDecoder().decode(new Uint8Array(bytes));
}

function stripPrefix(p: string): string {
  const u = unquote(p.trim());
  return u.startsWith("a/") || u.startsWith("b/") ? u.slice(2) : u;
}

const hunkHeader = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(.*)$/;

/** Parses the output of `git diff` (without renames) into files. */
export function parseDiff(text: string): DiffFile[] {
  const files: DiffFile[] = [];
  let file: DiffFile | undefined;
  let hunk: DiffHunk | undefined;
  let oldNo = 0;
  let newNo = 0;
  // Lines the current hunk still has on each side.
  let oldLeft = 0;
  let newLeft = 0;
  let inBinaryPatch = false;
  for (const line of text.split("\n")) {
    if (line.startsWith("diff --git ")) {
      const header = line.slice("diff --git ".length);
      // "a/x b/x": both halves are the same path without renames.
      const path = header.startsWith('"')
        ? stripPrefix(header.slice(0, header.indexOf('" ') + 1))
        : header.slice(2, (header.length - 1) / 2);
      file = { path, status: "modified", binary: false, hunks: [], additions: 0, deletions: 0 };
      files.push(file);
      hunk = undefined;
      inBinaryPatch = false;
      continue;
    }
    if (!file || inBinaryPatch) continue;
    if (!hunk) {
      if (line.startsWith("new file mode")) file.status = "added";
      else if (line.startsWith("deleted file mode")) file.status = "deleted";
      else if (line.startsWith("+++ ") && !line.endsWith("/dev/null")) file.path = stripPrefix(line.slice(4));
      else if (line.startsWith("--- ") && file.status === "deleted" && !line.endsWith("/dev/null"))
        file.path = stripPrefix(line.slice(4));
      else if (line.startsWith("Binary files ")) file.binary = true;
      else if (line === "GIT binary patch") {
        file.binary = true;
        inBinaryPatch = true;
      }
    }
    const h = hunkHeader.exec(line);
    if (h) {
      oldNo = Number(h[1]);
      newNo = Number(h[3]);
      oldLeft = h[2] === undefined ? 1 : Number(h[2]);
      newLeft = h[4] === undefined ? 1 : Number(h[4]);
      hunk = { id: `${oldNo},${newNo}`, header: (h[5] ?? "").trim(), lines: [] };
      file.hunks.push(hunk);
      continue;
    }
    if (!hunk || (oldLeft <= 0 && newLeft <= 0)) continue;
    // A blank line inside a hunk is a context line whose space was lost.
    const sign = line === "" ? " " : line[0];
    const body = line.slice(1);
    if (sign === "+") {
      hunk.lines.push({ kind: "add", text: body, newNo: newNo++ });
      file.additions++;
      newLeft--;
    } else if (sign === "-") {
      hunk.lines.push({ kind: "del", text: body, oldNo: oldNo++ });
      file.deletions++;
      oldLeft--;
    } else if (sign === " ") {
      hunk.lines.push({ kind: "context", text: body, oldNo: oldNo++, newNo: newNo++ });
      oldLeft--;
      newLeft--;
    }
    // "\ No newline at end of file" is skipped.
  }
  return files;
}
