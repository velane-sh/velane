"use client";

import React, { useState } from "react";

const faqs = [
  {
    question: "Is Velane open source?",
    answer:
      "Yes. Velane is dual-licensed under AGPL-3.0 for open-source use and a commercial license for proprietary deployments. You can inspect, fork, and self-host the full stack. The repo is on GitHub.",
  },
  {
    question: "Where does my code run?",
    answer:
      "Every invocation runs in a fully isolated Bun or Python sandbox on Velane's infrastructure. Sandboxes are ephemeral, spun up per request and torn down immediately after, so one workflow can never affect another.",
  },
  {
    question: "How are OAuth tokens and credentials managed?",
    answer:
      "Credentials are handled entirely server-side. Your snippet code never sees raw tokens. It calls integrations through Velane's internal proxy endpoint, which exchanges tokens on the fly. Connections are stored encrypted at rest and never exposed to the browser or your agent.",
  },
  {
    question: "Which AI agents does Velane support?",
    answer:
      "Any agent that supports the Model Context Protocol (MCP). This includes Cursor, Claude Code, OpenAI Codex, and any custom agent built on an MCP-compatible framework. If it speaks MCP, it works with Velane.",
  },
  {
    question: "Can I self-host Velane?",
    answer:
      "Yes. The full stack, including the control plane, executor runtime, and MCP server, can be deployed on your own infrastructure. OpenTofu configs for EKS are included in the repo under infra/terraform/.",
  },
  {
    question: "Is there a free tier?",
    answer:
      "Yes. You can start building for free on the hosted version. Limits are generous for solo developers and small teams exploring the platform.",
  },
  {
    question: "What happens if I hit an error during execution?",
    answer:
      "Every invocation is logged with its full stdout, stderr, exit code, and duration. Your agent can call the get_logs MCP tool to read those logs directly in the chat and autonomously debug and retry without context switching.",
  },
];

export default function FAQSection() {
  const [open, setOpen] = useState<number | null>(null);

  return (
    <section className="border-t border-black/5 py-24">
      <div className="mx-auto max-w-5xl px-6">
        <div className="mb-12">
          <p className="mb-3 text-xs font-semibold uppercase tracking-[0.2em] text-zinc-400">
            FAQ
          </p>
          <h2 className="text-3xl font-medium tracking-tight text-zinc-900">
            Common questions
          </h2>
        </div>
        <div className="max-w-2xl divide-y divide-black/5">
          {faqs.map((faq, i) => (
            <div key={i} className="py-5">
              <button
                className="flex w-full items-center justify-between gap-4 text-left"
                onClick={() => setOpen(open === i ? null : i)}
                aria-expanded={open === i}
              >
                <span className="text-sm font-medium text-zinc-900">
                  {faq.question}
                </span>
                <svg
                  viewBox="0 0 24 24"
                  width="16"
                  height="16"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  className={`shrink-0 text-zinc-400 transition-transform duration-200 ${open === i ? "rotate-45" : ""}`}
                >
                  <path d="M12 5v14M5 12h14" />
                </svg>
              </button>
              {open === i && (
                <p className="mt-3 text-sm leading-relaxed text-zinc-500">
                  {faq.answer}
                </p>
              )}
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
