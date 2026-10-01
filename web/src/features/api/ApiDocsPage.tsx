import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import clsx from "clsx";
import { ArrowLeft } from "lucide-react";
import { useMemo } from "react";
import { Card, PageHeader } from "@/components/ui";
import { apiDocsQuery, type ApiDoc } from "./api";

const methodTone: Record<string, string> = {
  GET: "bg-positive/10 text-positive",
  POST: "bg-accent-soft text-accent",
  PUT: "bg-accent-soft text-accent",
  PATCH: "bg-accent-soft text-accent",
  DELETE: "bg-negative/10 text-negative",
};

const accessLabel: Record<ApiDoc["access"], string> = {
  public: "No key needed",
  session: "App only",
  read: "Read key",
  write: "Write key",
};

export function ApiDocsPage() {
  const { data } = useQuery(apiDocsQuery);
  const base = `${window.location.origin}/api`;
  const groups = useMemo(() => {
    const out = new Map<string, ApiDoc[]>();
    for (const e of data?.endpoints ?? []) out.set(e.group, [...(out.get(e.group) ?? []), e]);
    return [...out.entries()];
  }, [data]);

  return (
    <>
      <PageHeader
        title="API documentation"
        actions={
          <Link to={"/settings" as string} hash="api" className="inline-flex items-center gap-1 text-[13px] font-medium text-accent hover:underline">
            <ArrowLeft size={14} /> Settings
          </Link>
        }
      />
      <div className="mx-auto flex max-w-4xl flex-col gap-4 p-4 md:p-6">
        <Card title="Getting started">
          <div className="flex flex-col gap-3 text-sm">
            <p>
              Viceroy's REST API is the same JSON API the app uses. An admin turns it on and generates keys in Settings → API. Every request sends a
              key:
            </p>
            <pre className="overflow-x-auto rounded-lg bg-surface-2 px-3 py-2 font-mono text-xs">
              {`curl -H "Authorization: Bearer vk_..." ${base}/accounts

curl -X PATCH -H "Authorization: Bearer vk_..." -H "Content-Type: application/json" \\
  -d '{"category_id": 12}' ${base}/transactions/345`}
            </pre>
            <ul className="list-disc space-y-1 pl-5 text-[13px] text-muted">
              <li>A key acts as the admin who made it. Read-only keys can only use GET.</li>
              <li>Responses are JSON. Money in responses is integer cents (-1234 = -$12.34, negative = money out); in requests, amounts are dollar strings like "12.34".</li>
              <li>Dates are YYYY-MM-DD. Errors return a 4xx/5xx status with {"{"}"error": "message"{"}"}.</li>
              <li>The server's IP allowlist (allowed_cidrs in viceroy.toml) still applies to API calls.</li>
              <li>
                Machine-readable:{" "}
                <a href="/api/openapi.json" className="font-medium text-accent hover:underline">
                  openapi.json
                </a>{" "}
                (OpenAPI 3) for tools and client generators.
              </li>
            </ul>
          </div>
        </Card>
        {groups.map(([group, endpoints]) => (
          <Card key={group} title={group}>
            <ul className="-mx-4 -my-4 divide-y divide-border" data-testid="api-doc-group">
              {endpoints.map((e) => (
                <li key={e.method + e.path} className="flex flex-col gap-1 px-4 py-3">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className={clsx("w-14 shrink-0 rounded px-1.5 py-0.5 text-center font-mono text-[11px] font-semibold", methodTone[e.method])}>{e.method}</span>
                    <code className="font-mono text-[13px]">/api{e.path}</code>
                    <span className="ml-auto text-xs text-muted">{accessLabel[e.access]}</span>
                  </div>
                  <p className="text-[13px]">{e.summary}</p>
                  {e.params && (
                    <p className="text-xs text-muted">
                      <span className="font-medium">{e.method === "GET" ? "Query" : "Body"}:</span> {e.params}
                    </p>
                  )}
                </li>
              ))}
            </ul>
          </Card>
        ))}
      </div>
    </>
  );
}
