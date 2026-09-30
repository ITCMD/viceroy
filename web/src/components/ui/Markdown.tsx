import { Fragment, type ReactNode } from "react";

/**
 * Renders the small Markdown subset chat answers use: paragraphs, #-headings, bullet and
 * numbered lists, pipe tables, fenced code, **bold**, *italic* and `code`. Builds React
 * elements (never HTML strings), so model output can't inject markup.
 */
export function Markdown({ text }: { text: string }) {
  return <div className="flex flex-col gap-2 text-sm leading-relaxed">{blocks(text)}</div>;
}

const isTableRow = (l: string) => /^\s*\|.*\|\s*$/.test(l);
const isRule = (l: string) => /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/.test(l);
const bullet = /^\s*[-*+]\s+(.*)$/;
const numbered = /^\s*\d+[.)]\s+(.*)$/;

function cells(row: string) {
  return row.trim().replace(/^\|/, "").replace(/\|$/, "").split("|").map((c) => c.trim());
}

function blocks(text: string): ReactNode[] {
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  const out: ReactNode[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (!line.trim()) {
      i++;
      continue;
    }
    if (line.trim().startsWith("```")) {
      const code: string[] = [];
      i++;
      while (i < lines.length && !lines[i].trim().startsWith("```")) code.push(lines[i++]);
      i++;
      out.push(
        <pre key={out.length} className="overflow-x-auto rounded-lg bg-surface-2 px-3 py-2 text-[13px]">
          <code>{code.join("\n")}</code>
        </pre>,
      );
      continue;
    }
    const h = /^(#{1,4})\s+(.*)$/.exec(line);
    if (h) {
      out.push(
        <p key={out.length} className={h[1].length <= 2 ? "pt-1 text-[15px] font-semibold" : "pt-1 font-semibold"}>
          {inline(h[2])}
        </p>,
      );
      i++;
      continue;
    }
    if (isTableRow(line) && i + 1 < lines.length && isRule(lines[i + 1])) {
      const head = cells(line);
      i += 2;
      const rows: string[][] = [];
      while (i < lines.length && isTableRow(lines[i])) rows.push(cells(lines[i++]));
      const numeric = (c: string) => /^[-+]?\$?[\d,.]+%?$/.test(c.replace(/\*/g, ""));
      out.push(
        <div key={out.length} className="overflow-x-auto rounded-lg border border-border">
          <table className="w-full text-[13px]">
            <thead className="bg-surface-2 text-muted">
              <tr>
                {head.map((c, j) => (
                  <th key={j} className="px-3 py-1.5 text-left font-medium">
                    {inline(c)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {rows.map((r, k) => (
                <tr key={k}>
                  {r.map((c, j) => (
                    <td key={j} className={numeric(c) ? "px-3 py-1.5 text-right tabular-nums" : "px-3 py-1.5"}>
                      {inline(c)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>,
      );
      continue;
    }
    const list = bullet.test(line) ? bullet : numbered.test(line) ? numbered : null;
    if (list) {
      const items: string[] = [];
      while (i < lines.length && list.test(lines[i])) {
        let item = list.exec(lines[i++])![1];
        // Continuation lines (indented, not a new item) belong to the item.
        while (i < lines.length && /^\s{2,}\S/.test(lines[i]) && !bullet.test(lines[i]) && !numbered.test(lines[i])) item += " " + lines[i++].trim();
        items.push(item);
      }
      const Tag = list === bullet ? "ul" : "ol";
      out.push(
        <Tag key={out.length} className={list === bullet ? "list-disc space-y-0.5 pl-5" : "list-decimal space-y-0.5 pl-5"}>
          {items.map((it, j) => (
            <li key={j}>{inline(it)}</li>
          ))}
        </Tag>,
      );
      continue;
    }
    const para: string[] = [];
    while (
      i < lines.length &&
      lines[i].trim() &&
      !bullet.test(lines[i]) &&
      !numbered.test(lines[i]) &&
      !/^#{1,4}\s/.test(lines[i]) &&
      !lines[i].trim().startsWith("```") &&
      !isTableRow(lines[i])
    )
      para.push(lines[i++]);
    if (para.length === 0) para.push(lines[i++]); // a lone table-looking row
    out.push(
      <p key={out.length}>
        {para.map((l, j) => (
          <Fragment key={j}>
            {j > 0 && <br />}
            {inline(l)}
          </Fragment>
        ))}
      </p>,
    );
  }
  return out;
}

function inline(s: string): ReactNode[] {
  const out: ReactNode[] = [];
  // Underscore emphasis is left out on purpose: it would mangle snake_case names.
  const re = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^*\s][^*]*\*)/g;
  let last = 0;
  for (let m = re.exec(s); m; m = re.exec(s)) {
    if (m.index > last) out.push(s.slice(last, m.index));
    const t = m[0];
    if (m[1]) out.push(<code key={m.index} className="rounded bg-surface-2 px-1 text-[12.5px]">{t.slice(1, -1)}</code>);
    else if (m[2]) out.push(<strong key={m.index} className="font-semibold">{t.slice(2, -2)}</strong>);
    else out.push(<em key={m.index}>{t.slice(1, -1)}</em>);
    last = m.index + t.length;
  }
  if (last < s.length) out.push(s.slice(last));
  return out;
}
