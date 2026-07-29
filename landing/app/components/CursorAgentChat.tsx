"use client";

import React from "react";

type Entry =
  | { type: "user"; text: string }
  | { type: "assistant"; text: string }
  | { type: "tool"; name: string; arg: string; result: string }
  | { type: "callout"; endpoint: string }
  | { type: "summary"; text: string };

const script: Entry[] = [
  { type: "user", text: "Build a workflow that syncs new Stripe customers to HubSpot contacts." },
  {
    type: "assistant",
    text: "I'll check your connections, write the snippet, and ship it to dev first.",
  },
  {
    type: "tool",
    name: "list_connections",
    arg: "",
    result: "Stripe · HubSpot connected",
  },
  {
    type: "tool",
    name: "get_integration_docs",
    arg: 'provider: "hubspot"',
    result: "Loaded Contacts API reference",
  },
  {
    type: "tool",
    name: "create_snippet",
    arg: 'slug: "stripe-to-hubspot"',
    result: "Snippet created · bun",
  },
  {
    type: "tool",
    name: "update_draft",
    arg: "code: 48 lines",
    result: "Saved version 1",
  },
  {
    type: "tool",
    name: "publish_snippet",
    arg: 'env: "dev"',
    result: "Published v1 → dev",
  },
  { type: "assistant", text: "Running a few test invocations on dev." },
  {
    type: "tool",
    name: "invoke_snippet",
    arg: 'env: "dev"',
    result: "200 OK · 1 contact created",
  },
  {
    type: "tool",
    name: "get_logs",
    arg: "limit: 10",
    result: "3 runs · 0 errors · avg 240ms",
  },
  {
    type: "assistant",
    text: "Tested 3 times on dev, all passing. Should I publish to prod?",
  },
  { type: "user", text: "Yes, ship it." },
  {
    type: "tool",
    name: "publish_snippet",
    arg: 'env: "prod"',
    result: "Published v1 → prod",
  },
  { type: "callout", endpoint: "api.velane.sh/v1/invoke/stripe-to-hubspot" },
  {
    type: "summary",
    text: "Live in production. Tested 3 times on dev, all passing.",
  },
];

function ToolIcon() {
  return (
    <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="m14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z" />
    </svg>
  );
}

function Spinner() {
  return (
    <svg className="animate-spin text-zinc-400" viewBox="0 0 24 24" width="13" height="13" fill="none">
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="3" strokeOpacity="0.2" />
      <path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
    </svg>
  );
}

function Check() {
  return (
    <svg viewBox="0 0 24 24" width="11" height="11" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
      <path d="M20 6 9 17l-5-5" />
    </svg>
  );
}

export default function CursorAgentChat() {
  const [visible, setVisible] = React.useState(0);
  const [done, setDone] = React.useState<Set<number>>(new Set());
  const scrollRef = React.useRef<HTMLDivElement>(null);

  React.useEffect(() => {
    const el = scrollRef.current;
    if (el) {
      el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
    }
  }, [visible, done]);

  React.useEffect(() => {
    let cancelled = false;
    const timeouts: ReturnType<typeof setTimeout>[] = [];

    let delay = 450;
    script.forEach((entry, i) => {
      timeouts.push(
        setTimeout(() => {
          if (!cancelled) setVisible(i + 1);
        }, delay)
      );
      delay += entry.type === "tool" ? 950 : 800;

      if (entry.type === "tool") {
        timeouts.push(
          setTimeout(() => {
            if (!cancelled)
              setDone((prev) => {
                const next = new Set(prev);
                next.add(i);
                return next;
              });
          }, delay - 350)
        );
      }
    });
    return () => {
      cancelled = true;
      timeouts.forEach(clearTimeout);
    };
  }, []);

  return (
    <div className="relative mx-auto w-full max-w-md">
      <div className="absolute inset-0 -z-10 rounded-[2rem] bg-zinc-300/40 blur-3xl" />

      <div className="overflow-hidden rounded-2xl border border-black/5 bg-white shadow-xl">
        {/* Agent header */}
        <div className="flex items-center justify-between border-b border-black/5 bg-zinc-50/80 px-4 py-3">
          <div className="flex items-center gap-2">
            <span className="flex h-5 w-5 items-center justify-center rounded-md bg-zinc-900 text-[10px] font-semibold text-white">
              ⌘
            </span>
            <span className="text-xs font-medium text-zinc-600">Agent</span>
          </div>
          <span className="rounded-full border border-black/5 bg-white px-2 py-0.5 text-[10px] font-medium text-zinc-500">
            velane · MCP
          </span>
        </div>

        <div
          ref={scrollRef}
          className="h-[420px] space-y-3 overflow-y-auto p-4 [scrollbar-width:thin]"
        >
          {script.slice(0, visible).map((entry, i) => {
            if (entry.type === "user") {
              return (
                <div
                  key={i}
                  className="rounded-xl border border-black/5 bg-zinc-50 px-3.5 py-2.5 text-sm text-zinc-800"
                >
                  {entry.text}
                </div>
              );
            }

            if (entry.type === "assistant") {
              return (
                <p key={i} className="px-1 text-sm leading-relaxed text-zinc-600">
                  {entry.text}
                </p>
              );
            }

            if (entry.type === "callout") {
              return (
                <div
                  key={i}
                  className="flex items-center gap-2.5 rounded-xl border border-emerald-200 bg-emerald-50 px-3.5 py-3"
                >
                  <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="shrink-0 text-emerald-600">
                    <circle cx="12" cy="12" r="10" />
                    <path d="M2 12h20" />
                    <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z" />
                  </svg>
                  <div className="min-w-0">
                    <div className="text-[11px] font-medium text-emerald-700">
                      Workflow live
                    </div>
                    <div className="truncate font-mono text-xs text-emerald-800">
                      {entry.endpoint}
                    </div>
                  </div>
                </div>
              );
            }

            if (entry.type === "summary") {
              return (
                <div
                  key={i}
                  className="flex items-start gap-2 rounded-xl bg-zinc-900 px-3.5 py-3 text-sm text-white"
                >
                  <span className="mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-emerald-500 text-white">
                    <Check />
                  </span>
                  <span className="leading-relaxed">{entry.text}</span>
                </div>
              );
            }

            const isDone = done.has(i);
            return (
              <div
                key={i}
                className="flex items-center gap-2.5 rounded-xl border border-black/5 bg-white px-3 py-2 shadow-sm"
              >
                <span className="text-zinc-400">
                  <ToolIcon />
                </span>
                <div className="flex min-w-0 flex-1 items-baseline gap-2">
                  <span className="font-mono text-xs font-medium text-zinc-800">
                    {entry.name}
                  </span>
                  {entry.arg && (
                    <span className="truncate font-mono text-[11px] text-zinc-400">
                      {entry.arg}
                    </span>
                  )}
                </div>
                {isDone ? (
                  <span className="flex items-center gap-1.5 whitespace-nowrap text-[11px] text-zinc-500">
                    <span className="flex h-3.5 w-3.5 items-center justify-center rounded-full bg-emerald-500 text-white">
                      <Check />
                    </span>
                    <span className="hidden sm:inline">{entry.result}</span>
                  </span>
                ) : (
                  <Spinner />
                )}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
