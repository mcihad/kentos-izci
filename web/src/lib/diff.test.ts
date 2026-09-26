import { describe, expect, it } from "vitest";
import { parseDiff } from "./diff";

const sample = `diff --git a/calc.go b/calc.go
index 1a2b3c4..5d6e7f8 100644
--- a/calc.go
+++ b/calc.go
@@ -1,6 +1,11 @@ package calc
 package calc

+import "errors"
+
 // Divide returns a / b.
-func Divide(a, b int) int {
-	return a / b
+func Divide(a, b int) (int, error) {
+	if b == 0 {
+		return 0, errors.New("division by zero")
+	}
+	return a / b, nil
 }
diff --git a/calc_test.go b/calc_test.go
new file mode 100644
index 0000000..abcdef0
--- /dev/null
+++ b/calc_test.go
@@ -0,0 +1,2 @@
+package calc
+// test
diff --git a/old.txt b/old.txt
deleted file mode 100644
index 1111111..0000000
--- a/old.txt
+++ /dev/null
@@ -1 +0,0 @@
-gone
\\ No newline at end of file
diff --git a/logo.png b/logo.png
new file mode 100644
index 0000000..2222222
GIT binary patch
literal 12
TcmZ?wbhEHbRA2y!0rCMs

literal 0
HcmV?d00001

diff --git "a/\\303\\266zet.md" "b/\\303\\266zet.md"
index 3333333..4444444 100644
--- "a/\\303\\266zet.md"
+++ "b/\\303\\266zet.md"
@@ -2 +2 @@
-eski
+yeni
`;

describe("parseDiff", () => {
  const files = parseDiff(sample);

  it("splits files with status and counts", () => {
    expect(files.map((f) => [f.path, f.status, f.additions, f.deletions, f.binary])).toEqual([
      ["calc.go", "modified", 7, 2, false],
      ["calc_test.go", "added", 2, 0, false],
      ["old.txt", "deleted", 0, 1, false],
      ["logo.png", "added", 0, 0, true],
      ["özet.md", "modified", 1, 1, false],
    ]);
  });

  it("numbers lines on both sides", () => {
    const [calc] = files;
    const lines = calc?.hunks[0]?.lines ?? [];
    expect(calc?.hunks[0]?.header).toBe("package calc");
    expect(lines[0]).toEqual({ kind: "context", text: "package calc", oldNo: 1, newNo: 1 });
    expect(lines[2]).toEqual({ kind: "add", text: 'import "errors"', newNo: 3 });
    expect(lines.find((l) => l.kind === "del")).toEqual({
      kind: "del",
      text: "func Divide(a, b int) int {",
      oldNo: 4,
    });
    expect(lines.at(-1)).toEqual({ kind: "context", text: "}", oldNo: 6, newNo: 11 });
    expect(files[4]?.hunks[0]?.lines).toEqual([
      { kind: "del", text: "eski", oldNo: 2 },
      { kind: "add", text: "yeni", newNo: 2 },
    ]);
  });
});
