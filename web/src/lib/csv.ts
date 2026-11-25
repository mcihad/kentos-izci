/** A CSV table for spreadsheets: semicolons and a BOM, as Excel with Turkish settings expects. */
export function toCsv(head: string[], rows: (string | number | bigint)[][]): string {
  const cell = (v: string | number | bigint) => {
    const s = String(v);
    return /[";\r\n]/.test(s) ? `"${s.replaceAll('"', '""')}"` : s;
  };
  return `﻿${[head, ...rows].map((r) => r.map(cell).join(";")).join("\r\n")}`;
}

/** Offers text as a file download. */
export function downloadText(name: string, text: string, type = "text/csv;charset=utf-8") {
  const url = URL.createObjectURL(new Blob([text], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}
