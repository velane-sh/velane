import React from "react";
import Image from "next/image";
import Link from "next/link";
import CursorAgentChat from "./components/CursorAgentChat";
import FAQSection from "./components/FAQSection";

const featuredIntegrations = [
  { name: "Salesforce", logo: "/salesforce.jpg" },
  { name: "Zendesk", logo: "/zendesk.svg" },
  { name: "HubSpot", logo: "/hubspot.webp" },
  { name: "Stripe", logo: "/stripe.png" },
  { name: "Notion", logo: "/notion.png" },
  { name: "Slack", logo: "/slack.png" },
  { name: "GitHub", logo: "/github.png" },
  { name: "Linear", logo: "/linear.png" },
];

const comparisonRows = [
  {
    feature: "Agent-native via MCP",
    diy: false,
    pipedream: false,
    velane: true,
  },
  {
    feature: "Zero OAuth setup",
    diy: false,
    pipedream: "partial" as const,
    velane: true,
  },
  {
    feature: "Isolated sandbox per invocation",
    diy: false,
    pipedream: false,
    velane: true,
  },
  {
    feature: "Execution logs in your agent",
    diy: false,
    pipedream: false,
    velane: true,
  },
  {
    feature: "Environment promotion (dev → prod)",
    diy: false,
    pipedream: false,
    velane: true,
  },
  {
    feature: "Write code in your editor",
    diy: true,
    pipedream: false,
    velane: true,
  },
  {
    feature: "Self-hostable",
    diy: true,
    pipedream: false,
    velane: true,
  },
];

function Check({ dim = false }: { dim?: boolean }) {
  return (
    <span
      className={`mx-auto flex h-5 w-5 items-center justify-center rounded-full ${dim ? "bg-zinc-200" : "bg-zinc-900"}`}
    >
      <svg
        viewBox="0 0 24 24"
        width="11"
        height="11"
        fill="none"
        stroke={dim ? "#71717a" : "white"}
        strokeWidth="3"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <path d="M20 6 9 17l-5-5" />
      </svg>
    </span>
  );
}

function Cross() {
  return (
    <span className="mx-auto flex h-5 w-5 items-center justify-center rounded-full bg-zinc-100">
      <svg
        viewBox="0 0 24 24"
        width="10"
        height="10"
        fill="none"
        stroke="#a1a1aa"
        strokeWidth="2.5"
        strokeLinecap="round"
      >
        <path d="M18 6 6 18M6 6l12 12" />
      </svg>
    </span>
  );
}

function CellValue({ value }: { value: boolean | "partial" }) {
  if (value === true) return <Check />;
  if (value === "partial")
    return (
      <span className="block text-center text-xs text-zinc-400">Partial</span>
    );
  return <Cross />;
}

const pipelineSteps = [
  { label: "Your Agent", sub: "Cursor · Claude Code" },
  { label: "Velane MCP", sub: "mcp.velane.sh" },
  { label: "Sandbox", sub: "Bun · Python" },
  { label: "Integration", sub: "800+ APIs" },
];

export default function Home() {
  return (
    <div className="min-h-screen bg-[#FAFAFA] text-zinc-900 selection:bg-zinc-200">
      {/* Header */}
      <header className="sticky top-0 z-50 border-b border-black/5 bg-[#FAFAFA]/80 backdrop-blur-md">
        <div className="mx-auto flex w-full max-w-5xl items-center justify-between px-6 py-4">
          <div className="flex items-center gap-2">
            <Image
              src="/logo.png"
              alt="Velane Logo"
              width={24}
              height={24}
              className="rounded"
            />
            <div className="text-xl font-medium tracking-tight">Velane</div>
          </div>
          <div className="flex items-center gap-6">
            <a
              className="hidden text-sm text-zinc-500 transition-colors hover:text-zinc-900 sm:inline-block"
              href="https://docs.velane.sh"
              target="_blank"
              rel="noreferrer"
            >
              Docs
            </a>
            <a
              className="hidden items-center gap-2 rounded-full border border-black/10 px-3 py-1.5 text-sm font-medium text-zinc-600 transition-colors hover:bg-black/5 hover:text-zinc-900 sm:flex"
              href="https://github.com/abskrj/velane"
              target="_blank"
              rel="noreferrer"
            >
              <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor">
                <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z" />
              </svg>
              Star
            </a>
            <a
              className="rounded-full bg-zinc-900 px-4 py-2 text-sm font-medium text-white shadow-sm transition-transform hover:scale-105 hover:bg-zinc-800"
              href="https://app.velane.sh"
            >
              Start building
            </a>
          </div>
        </div>
      </header>

      <main>
        {/* Hero */}
        <section className="relative flex min-h-[calc(100vh-65px)] flex-col justify-center overflow-hidden py-20 md:py-32">
          <div
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 bg-[linear-gradient(to_right,rgb(0_0_0/0.06)_1px,transparent_1px),linear-gradient(to_bottom,rgb(0_0_0/0.06)_1px,transparent_1px)] [background-size:28px_28px] [mask-image:radial-gradient(ellipse_90%_90%_at_50%_50%,black_20%,transparent_90%)]"
          />
          <div
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_52%_68%_at_28%_45%,rgba(250,250,250,0.98)_0%,transparent_68%)]"
          />
          <div className="relative mx-auto grid w-full max-w-5xl gap-16 px-6 md:grid-cols-2 md:items-center">
            <div className="max-w-xl">
              <h1 className="text-5xl font-medium leading-[1.1] tracking-tight text-zinc-900 md:text-6xl">
                Integration lifecycle infrastructure for AI agents
              </h1>
              <p className="mt-6 text-lg leading-relaxed text-zinc-600">
                Give your agents one place to discover integrations, build and
                test workflows, and promote them safely from development to
                production, all without leaving the chat.
              </p>
              <div className="mt-10 flex flex-wrap items-center gap-4">
                <a
                  className="rounded-full bg-zinc-900 px-6 py-3 text-sm font-medium text-white shadow-md transition-all hover:scale-105 hover:bg-zinc-800 hover:shadow-lg"
                  href="https://app.velane.sh"
                >
                  Connect your agent
                </a>
                <a
                  className="inline-flex items-center gap-2 rounded-full border border-black/10 px-6 py-3 text-sm font-medium text-zinc-600 transition-colors hover:bg-black/5"
                  href="https://github.com/abskrj/velane"
                  target="_blank"
                  rel="noreferrer"
                >
                  <Image
                    src="/github.png"
                    alt=""
                    width={16}
                    height={16}
                    aria-hidden
                    className="h-4 w-4 object-contain"
                  />
                  View GitHub
                </a>
              </div>
            </div>
            <CursorAgentChat />
          </div>
        </section>

        {/* Traction strip */}
        <div className="border-y border-black/5 bg-zinc-50 py-5">
          <div className="mx-auto flex max-w-5xl flex-wrap items-center justify-center gap-x-10 gap-y-3 px-6">
            <span className="flex items-center gap-2 text-sm text-zinc-500">
              <span className="inline-block h-1.5 w-1.5 rounded-full bg-emerald-500" />
              Open source · AGPL-3.0
            </span>
            <span className="text-sm text-zinc-500">800+ integrations</span>
            <span className="text-sm text-zinc-500">Bun & Python runtimes</span>
            <span className="text-sm text-zinc-500">
              Isolated sandbox per invocation
            </span>
            <span className="text-sm text-zinc-500">
              3 environments: dev · staging · prod
            </span>
          </div>
        </div>

        {/* Integrations */}
        <section className="border-b border-black/5 bg-white py-20">
          <div className="mx-auto w-full max-w-5xl px-6">
            <div className="mb-12 text-center">
              <p className="mb-3 text-xs font-semibold uppercase tracking-[0.2em] text-zinc-400">
                Integrations
              </p>
              <h2 className="text-3xl font-medium tracking-tight text-zinc-900">
                800+ tools. Zero tokens in your code.
              </h2>
              <p className="mx-auto mt-4 max-w-lg text-lg text-zinc-600">
                Your agent discovers connected accounts and calls any API
                through Velane — no OAuth flows, no credentials, no glue code.
              </p>
            </div>

            <div className="mx-auto grid max-w-4xl grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4">
              {featuredIntegrations.map((item) => (
                <div
                  key={item.name}
                  className="group flex items-center gap-4 rounded-2xl border border-black/5 bg-white p-5 shadow-sm transition-all hover:-translate-y-px hover:border-black/10 hover:shadow-md"
                >
                  <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-black/5 bg-zinc-50 p-1.5">
                    <Image
                      src={item.logo}
                      alt={`${item.name} logo`}
                      width={28}
                      height={28}
                      className="h-full w-full object-contain"
                    />
                  </div>
                  <span className="font-medium text-zinc-800 group-hover:text-zinc-950">
                    {item.name}
                  </span>
                </div>
              ))}
            </div>

            <div className="mt-10 text-center">
              <a
                href="https://nango.dev/api-integrations/"
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1 text-sm font-medium text-zinc-600 hover:text-zinc-900"
              >
                Browse the full catalog on Nango{" "}
                <span aria-hidden="true">→</span>
              </a>
            </div>
          </div>
        </section>

        {/* Who is this for */}
        <section className="py-24">
          <div className="mx-auto max-w-5xl px-6">
            <div className="mb-12">
              <p className="mb-3 text-xs font-semibold uppercase tracking-[0.2em] text-zinc-400">
                Who it&apos;s for
              </p>
              <h2 className="text-3xl font-medium tracking-tight text-zinc-900">
                Built for developers who ship with AI
              </h2>
              <p className="mt-4 max-w-xl text-lg text-zinc-600">
                Whether you&apos;re a solo developer or part of a larger team,
                Velane fits the way you already work.
              </p>
            </div>
            <div className="grid gap-6 md:grid-cols-2">
              {/* Solo */}
              <div className="rounded-2xl border border-black/5 bg-white p-8 shadow-sm">
                <div className="mb-5 inline-flex h-9 w-9 items-center justify-center rounded-xl bg-zinc-100">
                  <svg
                    viewBox="0 0 24 24"
                    width="18"
                    height="18"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="1.75"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    className="text-zinc-700"
                  >
                    <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2" />
                    <circle cx="12" cy="7" r="4" />
                  </svg>
                </div>
                <h3 className="text-lg font-medium text-zinc-900">
                  Solo developers
                </h3>
                <p className="mt-2 text-sm leading-relaxed text-zinc-500">
                  You&apos;re building an AI agent and don&apos;t want to
                  maintain a backend just to call Stripe or Slack. Velane
                  handles the runtime, OAuth, and deployment pipeline so you
                  can stay in your editor.
                </p>
                <ul className="mt-6 space-y-3">
                  {[
                    "Ship integrations in one agent conversation",
                    "No OAuth plumbing or credential management",
                    "Open source — self-host or use the hosted version",
                    "Sandboxed execution, no infra to manage",
                  ].map((item) => (
                    <li
                      key={item}
                      className="flex items-start gap-2.5 text-sm text-zinc-600"
                    >
                      <span className="mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-zinc-100">
                        <svg
                          viewBox="0 0 24 24"
                          width="9"
                          height="9"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth="3"
                          strokeLinecap="round"
                          strokeLinejoin="round"
                        >
                          <path d="M20 6 9 17l-5-5" />
                        </svg>
                      </span>
                      {item}
                    </li>
                  ))}
                </ul>
              </div>

              {/* Teams */}
              <div className="rounded-2xl border border-black/5 bg-white p-8 shadow-sm">
                <div className="mb-5 inline-flex h-9 w-9 items-center justify-center rounded-xl bg-zinc-100">
                  <svg
                    viewBox="0 0 24 24"
                    width="18"
                    height="18"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="1.75"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    className="text-zinc-700"
                  >
                    <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2" />
                    <circle cx="9" cy="7" r="4" />
                    <path d="M23 21v-2a4 4 0 0 0-3-3.87" />
                    <path d="M16 3.13a4 4 0 0 1 0 7.75" />
                  </svg>
                </div>
                <h3 className="text-lg font-medium text-zinc-900">Teams</h3>
                <p className="mt-2 text-sm leading-relaxed text-zinc-500">
                  Your agents need shared credentials, audit trails, and
                  controlled rollouts. Velane gives every team member&apos;s
                  agent the same integrations without anyone touching a secret.
                </p>
                <ul className="mt-6 space-y-3">
                  {[
                    "Shared credential store — one OAuth connection per provider",
                    "Environment promotion: dev → staging → prod",
                    "Invocation logs and audit trail per tenant",
                    "Role-based access: invoke, manage, admin scopes",
                  ].map((item) => (
                    <li
                      key={item}
                      className="flex items-start gap-2.5 text-sm text-zinc-600"
                    >
                      <span className="mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-zinc-100">
                        <svg
                          viewBox="0 0 24 24"
                          width="9"
                          height="9"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth="3"
                          strokeLinecap="round"
                          strokeLinejoin="round"
                        >
                          <path d="M20 6 9 17l-5-5" />
                        </svg>
                      </span>
                      {item}
                    </li>
                  ))}
                </ul>
              </div>
            </div>
          </div>
        </section>

        {/* How it works */}
        <section className="relative border-y border-black/5 bg-zinc-50 py-24 overflow-hidden">
          <div
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 bg-[linear-gradient(to_right,rgb(0_0_0/0.04)_1px,transparent_1px),linear-gradient(to_bottom,rgb(0_0_0/0.04)_1px,transparent_1px)] [background-size:28px_28px]"
          />
          <div className="relative mx-auto w-full max-w-5xl px-6">
            <div className="mb-12 max-w-2xl">
              <p className="mb-3 text-xs font-semibold uppercase tracking-[0.2em] text-zinc-400">
                How it works
              </p>
              <h2 className="text-3xl font-medium tracking-tight text-zinc-900">
                Your IDE is now your deployment pipeline.
              </h2>
              <p className="mt-4 text-lg text-zinc-600">
                One MCP connection. Your agent handles the rest — writing,
                testing, and shipping production workflows autonomously.
              </p>
            </div>

            {/* Pipeline flow */}
            <div className="mb-16 flex flex-col items-center gap-3 sm:flex-row sm:justify-center">
              {pipelineSteps.map((step, i) => (
                <React.Fragment key={step.label}>
                  <div className="min-w-[120px] rounded-xl border border-black/8 bg-white px-5 py-4 text-center shadow-sm">
                    <div className="text-sm font-medium text-zinc-900">
                      {step.label}
                    </div>
                    <div className="mt-0.5 text-xs text-zinc-400">{step.sub}</div>
                  </div>
                  {i < pipelineSteps.length - 1 && (
                    <svg
                      className="shrink-0 rotate-90 text-zinc-300 sm:rotate-0"
                      viewBox="0 0 24 24"
                      width="20"
                      height="20"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="1.5"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    >
                      <path d="M5 12h14M12 5l7 7-7 7" />
                    </svg>
                  )}
                </React.Fragment>
              ))}
            </div>

            {/* Steps */}
            <div className="grid gap-8 md:grid-cols-3">
              <div className="group">
                <div className="mb-4 inline-flex h-10 w-10 items-center justify-center rounded-full bg-zinc-100 text-zinc-900 transition-colors group-hover:bg-zinc-900 group-hover:text-white">
                  1
                </div>
                <h3 className="text-xl font-medium text-zinc-900">
                  Create & Connect
                </h3>
                <p className="mt-3 leading-relaxed text-zinc-600">
                  Your agent calls <code className="rounded bg-zinc-100 px-1 py-0.5 text-xs font-mono text-zinc-700">list_connections</code> to discover your OAuth accounts, fetches live API docs for any provider, then writes Bun or Python code and saves it as a versioned workflow.
                </p>
              </div>
              <div className="group">
                <div className="mb-4 inline-flex h-10 w-10 items-center justify-center rounded-full bg-zinc-100 text-zinc-900 transition-colors group-hover:bg-zinc-900 group-hover:text-white">
                  2
                </div>
                <h3 className="text-xl font-medium text-zinc-900">
                  Run & Debug
                </h3>
                <p className="mt-3 leading-relaxed text-zinc-600">
                  The agent invokes the workflow on dev, reads execution logs with <code className="rounded bg-zinc-100 px-1 py-0.5 text-xs font-mono text-zinc-700">get_logs</code>, and autonomously fixes errors — all without leaving the chat window. Each run is isolated in an ephemeral sandbox.
                </p>
              </div>
              <div className="group">
                <div className="mb-4 inline-flex h-10 w-10 items-center justify-center rounded-full bg-zinc-100 text-zinc-900 transition-colors group-hover:bg-zinc-900 group-hover:text-white">
                  3
                </div>
                <h3 className="text-xl font-medium text-zinc-900">Promote</h3>
                <p className="mt-3 leading-relaxed text-zinc-600">
                  Once tests pass, the agent calls <code className="rounded bg-zinc-100 px-1 py-0.5 text-xs font-mono text-zinc-700">publish_snippet</code> with <code className="rounded bg-zinc-100 px-1 py-0.5 text-xs font-mono text-zinc-700">env: &quot;prod&quot;</code>. Your workflow goes live at a stable HTTP endpoint — versioned, audited, and rollback-ready.
                </p>
              </div>
            </div>
          </div>
        </section>

        {/* Technical proof — dark */}
        <section className="relative overflow-hidden bg-zinc-900 py-24 text-white selection:bg-zinc-700">
          <div
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 bg-[linear-gradient(to_right,rgb(255_255_255/0.03)_1px,transparent_1px),linear-gradient(to_bottom,rgb(255_255_255/0.03)_1px,transparent_1px)] [background-size:28px_28px]"
          />
          <div className="relative mx-auto w-full max-w-5xl px-6">
            <div className="mb-12">
              <p className="mb-3 text-xs font-semibold uppercase tracking-[0.2em] text-zinc-500">
                Setup
              </p>
              <h2 className="text-3xl font-medium tracking-tight">
                Two lines to connect. One handler to ship.
              </h2>
              <p className="mt-4 max-w-xl leading-relaxed text-zinc-400">
                Add Velane to your MCP config, then your agent writes real
                Bun or Python code. No credentials in code — the proxy
                injects auth automatically.
              </p>
            </div>

            <div className="grid gap-6 md:grid-cols-2">
              {/* MCP config */}
              <div className="overflow-hidden rounded-2xl border border-white/10 bg-black/50 shadow-2xl">
                <div className="flex items-center gap-2 border-b border-white/10 bg-white/5 px-4 py-3">
                  <div className="h-3 w-3 rounded-full bg-red-500/80" />
                  <div className="h-3 w-3 rounded-full bg-yellow-500/80" />
                  <div className="h-3 w-3 rounded-full bg-green-500/80" />
                  <span className="ml-2 font-mono text-xs text-zinc-500">
                    mcp.json
                  </span>
                </div>
                <pre className="overflow-x-auto p-6 font-mono text-sm leading-relaxed text-zinc-300">
                  {"{"}{"\n"}
                  {"  "}
                  <span className="text-pink-400">&quot;mcpServers&quot;</span>
                  {": {"}{"\n"}
                  {"    "}
                  <span className="text-blue-400">&quot;velane&quot;</span>
                  {": {"}{"\n"}
                  {"      "}
                  <span className="text-pink-400">&quot;url&quot;</span>
                  {": "}
                  <span className="text-emerald-300">
                    &quot;https://mcp.velane.sh/mcp&quot;
                  </span>
                  {",\n"}
                  {"      "}
                  <span className="text-pink-400">&quot;headers&quot;</span>
                  {": {"}{"\n"}
                  {"        "}
                  <span className="text-blue-400">
                    &quot;Authorization&quot;
                  </span>
                  {": "}
                  <span className="text-emerald-300">
                    &quot;Bearer vl_xxxx&quot;
                  </span>
                  {"\n"}
                  {"      "}
                  {"}"}{"\n"}
                  {"    "}
                  {"}"}{"\n"}
                  {"  "}
                  {"}"}{"\n"}
                  {"}"}
                </pre>
              </div>

              {/* Agent-generated snippet */}
              <div className="overflow-hidden rounded-2xl border border-white/10 bg-black/50 shadow-2xl">
                <div className="flex items-center gap-2 border-b border-white/10 bg-white/5 px-4 py-3">
                  <div className="h-3 w-3 rounded-full bg-red-500/80" />
                  <div className="h-3 w-3 rounded-full bg-yellow-500/80" />
                  <div className="h-3 w-3 rounded-full bg-green-500/80" />
                  <span className="ml-2 font-mono text-xs text-zinc-500">
                    stripe-to-hubspot.ts{" "}
                    <span className="text-zinc-600">· agent-generated</span>
                  </span>
                </div>
                <pre className="overflow-x-auto p-6 font-mono text-sm leading-relaxed text-zinc-300">
                  <span className="text-pink-400">import</span>
                  {" { integration } "}
                  <span className="text-pink-400">from</span>
                  {" "}
                  <span className="text-emerald-300">&apos;@velane/integrations&apos;</span>
                  {"\n\n"}
                  <span className="text-pink-400">export default async</span>
                  {" "}
                  <span className="text-yellow-300">function</span>
                  {" "}
                  <span className="text-blue-300">handler</span>
                  {"({ customerId }) {\n"}
                  {"  "}
                  <span className="text-pink-400">const</span>
                  {" stripe  = integration("}
                  <span className="text-emerald-300">&apos;stripe&apos;</span>
                  {")\n"}
                  {"  "}
                  <span className="text-pink-400">const</span>
                  {" hubspot = integration("}
                  <span className="text-emerald-300">&apos;hubspot&apos;</span>
                  {")\n\n"}
                  {"  "}
                  <span className="text-pink-400">const</span>
                  {" customer = "}
                  <span className="text-pink-400">await</span>
                  {" stripe."}
                  <span className="text-blue-300">get</span>
                  {"(`"}
                  <span className="text-emerald-300">
                    {"/v1/customers/${customerId}"}
                  </span>
                  {"`)\n\n"}
                  {"  "}
                  <span className="text-pink-400">await</span>
                  {" hubspot."}
                  <span className="text-blue-300">post</span>
                  {"("}
                  <span className="text-emerald-300">
                    &apos;/crm/v3/objects/contacts&apos;
                  </span>
                  {", {\n"}
                  {"    properties: { email: customer.email },\n"}
                  {"  })\n\n"}
                  {"  "}
                  <span className="text-pink-400">return</span>
                  {" { synced: "}
                  <span className="text-blue-400">true</span>
                  {" }\n}"}
                </pre>
              </div>
            </div>

            {/* Stats */}
            <div className="mt-12 grid grid-cols-2 gap-6 border-t border-white/10 pt-12 md:grid-cols-4">
              <div>
                <div className="text-2xl font-medium">800+</div>
                <div className="mt-1 text-sm text-zinc-400">
                  Integrations via Nango
                </div>
              </div>
              <div>
                <div className="text-2xl font-medium">2</div>
                <div className="mt-1 text-sm text-zinc-400">
                  Runtimes: Bun & Python
                </div>
              </div>
              <div>
                <div className="text-2xl font-medium">3</div>
                <div className="mt-1 text-sm text-zinc-400">
                  Environments: dev · staging · prod
                </div>
              </div>
              <div>
                <div className="text-2xl font-medium">AGPL</div>
                <div className="mt-1 text-sm text-zinc-400">
                  Open source licensed
                </div>
              </div>
            </div>
          </div>
        </section>

        {/* Comparison */}
        <section className="border-b border-black/5 bg-white py-24">
          <div className="mx-auto max-w-5xl px-6">
            <div className="mb-12">
              <p className="mb-3 text-xs font-semibold uppercase tracking-[0.2em] text-zinc-400">
                Comparison
              </p>
              <h2 className="text-3xl font-medium tracking-tight text-zinc-900">
                Why not just use...
              </h2>
              <p className="mt-4 max-w-xl text-lg text-zinc-600">
                Lambda handles execution. Pipedream handles workflows.
                Neither was designed for AI agents that write, test, and
                ship code autonomously.
              </p>
            </div>

            <div className="overflow-hidden rounded-2xl border border-black/5 shadow-sm">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-black/5 bg-zinc-50">
                    <th className="px-6 py-4 text-left font-medium text-zinc-500">
                      Feature
                    </th>
                    <th className="px-4 py-4 text-center font-medium text-zinc-500">
                      DIY / Lambda
                    </th>
                    <th className="px-4 py-4 text-center font-medium text-zinc-500">
                      Pipedream
                    </th>
                    <th className="bg-zinc-900 px-4 py-4 text-center font-medium text-white">
                      Velane
                    </th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-black/5">
                  {comparisonRows.map((row) => (
                    <tr key={row.feature} className="bg-white">
                      <td className="px-6 py-4 text-zinc-700">{row.feature}</td>
                      <td className="px-4 py-4">
                        <CellValue value={row.diy} />
                      </td>
                      <td className="px-4 py-4">
                        <CellValue value={row.pipedream} />
                      </td>
                      <td className="bg-zinc-50 px-4 py-4">
                        <Check />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </section>

        {/* Pricing */}
        <section className="py-24">
          <div className="mx-auto max-w-5xl px-6">
            <div className="mb-12">
              <p className="mb-3 text-xs font-semibold uppercase tracking-[0.2em] text-zinc-400">
                Pricing
              </p>
              <h2 className="text-3xl font-medium tracking-tight text-zinc-900">
                Simple, predictable pricing
              </h2>
              <p className="mt-4 max-w-xl text-lg text-zinc-600">
                Start free on cloud or self-host under AGPL. Upgrade when you
                need more.
              </p>
            </div>

            {/* Cloud plans */}
            <p className="mb-5 text-xs font-semibold uppercase tracking-[0.2em] text-zinc-400">
              Cloud
            </p>
            <div className="mb-16 grid gap-5 md:grid-cols-3">
              {/* Free */}
              <div className="flex flex-col rounded-2xl border border-black/5 bg-white p-8 shadow-sm">
                <div className="mb-6">
                  <h3 className="text-base font-semibold text-zinc-900">Free</h3>
                  <div className="mt-4 flex items-baseline gap-1">
                    <span className="text-4xl font-medium tracking-tight text-zinc-900">
                      $0
                    </span>
                    <span className="text-sm text-zinc-400">/ month</span>
                  </div>
                  <p className="mt-3 text-sm leading-relaxed text-zinc-500">
                    For personal projects and exploration.
                  </p>
                </div>
                <a
                  href="https://app.velane.sh"
                  className="block w-full rounded-lg border border-black/10 px-4 py-2.5 text-center text-sm font-medium text-zinc-700 transition-colors hover:bg-black/5"
                >
                  Get started free
                </a>
                <ul className="mt-8 flex-1 space-y-3">
                  {[
                    "500 invocations / month",
                    "Up to 5 workflows",
                    "Shared runtime",
                    "All 800+ integrations",
                    "Community support",
                  ].map((f) => (
                    <li
                      key={f}
                      className="flex items-start gap-2.5 text-sm text-zinc-600"
                    >
                      <span className="mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-zinc-100">
                        <svg viewBox="0 0 24 24" width="9" height="9" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
                          <path d="M20 6 9 17l-5-5" />
                        </svg>
                      </span>
                      {f}
                    </li>
                  ))}
                </ul>
              </div>

              {/* Hobby — highlighted */}
              <div className="relative flex flex-col rounded-2xl bg-zinc-900 p-8 shadow-xl">
                <div className="absolute -top-3.5 left-1/2 -translate-x-1/2 rounded-full border border-white/10 bg-zinc-800 px-3 py-1 text-xs font-medium text-zinc-300">
                  Most popular
                </div>
                <div className="mb-6">
                  <h3 className="text-base font-semibold text-white">Hobby</h3>
                  <div className="mt-4 flex items-baseline gap-1">
                    <span className="text-4xl font-medium tracking-tight text-white">
                      $20
                    </span>
                    <span className="text-sm text-zinc-400">/ month</span>
                  </div>
                  <p className="mt-3 text-sm leading-relaxed text-zinc-400">
                    For active builders and indie developers.
                  </p>
                </div>
                <a
                  href="https://app.velane.sh"
                  className="block w-full rounded-lg bg-white px-4 py-2.5 text-center text-sm font-medium text-zinc-900 transition-colors hover:bg-zinc-100"
                >
                  Start building
                </a>
                <ul className="mt-8 flex-1 space-y-3">
                  {[
                    "10,000 invocations / month",
                    "Unlimited workflows",
                    "Shared runtime",
                    "All 800+ integrations",
                    "Email support",
                    "30-day execution log history",
                  ].map((f) => (
                    <li
                      key={f}
                      className="flex items-start gap-2.5 text-sm text-zinc-300"
                    >
                      <span className="mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-white/10">
                        <svg viewBox="0 0 24 24" width="9" height="9" fill="none" stroke="white" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
                          <path d="M20 6 9 17l-5-5" />
                        </svg>
                      </span>
                      {f}
                    </li>
                  ))}
                </ul>
              </div>

              {/* Dedicated */}
              <div className="flex flex-col rounded-2xl border border-black/5 bg-white p-8 shadow-sm">
                <div className="mb-6">
                  <h3 className="text-base font-semibold text-zinc-900">
                    Dedicated
                  </h3>
                  <div className="mt-4 flex items-baseline gap-1">
                    <span className="text-4xl font-medium tracking-tight text-zinc-900">
                      $100
                    </span>
                    <span className="text-sm text-zinc-400">/ month</span>
                  </div>
                  <p className="mt-3 text-sm leading-relaxed text-zinc-500">
                    For production workloads that need isolation and guarantees.
                  </p>
                </div>
                <a
                  href="https://app.velane.sh"
                  className="block w-full rounded-lg bg-zinc-900 px-4 py-2.5 text-center text-sm font-medium text-white transition-colors hover:bg-zinc-800"
                >
                  Get started
                </a>
                <ul className="mt-8 flex-1 space-y-3">
                  {[
                    "Unlimited invocations",
                    "Unlimited workflows",
                    "Dedicated cluster",
                    "All 800+ integrations",
                    "Priority support",
                    "Uptime SLA",
                    "Custom domain",
                  ].map((f) => (
                    <li
                      key={f}
                      className="flex items-start gap-2.5 text-sm text-zinc-600"
                    >
                      <span className="mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-zinc-100">
                        <svg viewBox="0 0 24 24" width="9" height="9" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
                          <path d="M20 6 9 17l-5-5" />
                        </svg>
                      </span>
                      {f}
                    </li>
                  ))}
                </ul>
              </div>
            </div>

            {/* Self-host */}
            <p className="mb-5 text-xs font-semibold uppercase tracking-[0.2em] text-zinc-400">
              Self-host
            </p>
            <div className="grid gap-5 md:grid-cols-2">
              {/* AGPL — free */}
              <div className="flex flex-col rounded-2xl border border-black/5 bg-white p-8 shadow-sm">
                <div className="mb-6">
                  <h3 className="text-base font-semibold text-zinc-900">
                    Personal / Small Startup
                  </h3>
                  <div className="mt-4 flex items-baseline gap-1">
                    <span className="text-4xl font-medium tracking-tight text-zinc-900">
                      Free
                    </span>
                  </div>
                  <p className="mt-3 text-sm leading-relaxed text-zinc-500">
                    Self-host the full stack under AGPL-3.0. Free forever for
                    personal projects and startups building open-source
                    products.
                  </p>
                </div>
                <a
                  href="https://github.com/abskrj/velane"
                  target="_blank"
                  rel="noreferrer"
                  className="block w-full rounded-lg border border-black/10 px-4 py-2.5 text-center text-sm font-medium text-zinc-700 transition-colors hover:bg-black/5"
                >
                  View on GitHub
                </a>
                <ul className="mt-8 flex-1 space-y-3">
                  {[
                    "Full source code access",
                    "Deploy on your own infra",
                    "OpenTofu configs for EKS included",
                    "Community support via GitHub Issues",
                    "AGPL-3.0 — changes must stay open source",
                  ].map((f) => (
                    <li
                      key={f}
                      className="flex items-start gap-2.5 text-sm text-zinc-600"
                    >
                      <span className="mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-zinc-100">
                        <svg viewBox="0 0 24 24" width="9" height="9" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
                          <path d="M20 6 9 17l-5-5" />
                        </svg>
                      </span>
                      {f}
                    </li>
                  ))}
                </ul>
              </div>

              {/* Commercial license */}
              <div className="flex flex-col rounded-2xl border border-black/5 bg-white p-8 shadow-sm">
                <div className="mb-6">
                  <h3 className="text-base font-semibold text-zinc-900">
                    Commercial
                  </h3>
                  <div className="mt-4 flex items-baseline gap-1">
                    <span className="text-4xl font-medium tracking-tight text-zinc-900">
                      Contact us
                    </span>
                  </div>
                  <p className="mt-3 text-sm leading-relaxed text-zinc-500">
                    Self-host Velane in a proprietary product without the
                    AGPL-3.0 copyleft requirement. Includes a commercial
                    license for your entire organisation.
                  </p>
                </div>
                <a
                  href="mailto:abhi@velane.sh?subject=Velane%20Commercial%20License"
                  className="block w-full rounded-lg bg-zinc-900 px-4 py-2.5 text-center text-sm font-medium text-white transition-colors hover:bg-zinc-800"
                >
                  Contact for a license
                </a>
                <ul className="mt-8 flex-1 space-y-3">
                  {[
                    "Commercial license — no copyleft obligations",
                    "Self-host on your own infra",
                    "Full source code access",
                    "Priority email support",
                    "License covers your entire organisation",
                  ].map((f) => (
                    <li
                      key={f}
                      className="flex items-start gap-2.5 text-sm text-zinc-600"
                    >
                      <span className="mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-zinc-100">
                        <svg viewBox="0 0 24 24" width="9" height="9" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
                          <path d="M20 6 9 17l-5-5" />
                        </svg>
                      </span>
                      {f}
                    </li>
                  ))}
                </ul>
              </div>
            </div>
          </div>
        </section>

        {/* FAQ */}
        <FAQSection />

        {/* Final CTA */}
        <section className="relative overflow-hidden py-32 text-center">
          <div
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 bg-[linear-gradient(to_right,rgb(0_0_0/0.06)_1px,transparent_1px),linear-gradient(to_bottom,rgb(0_0_0/0.06)_1px,transparent_1px)] [background-size:28px_28px] [mask-image:radial-gradient(ellipse_90%_90%_at_50%_50%,black_20%,transparent_90%)]"
          />
          <div
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_55%_70%_at_50%_50%,rgba(250,250,250,0.98)_0%,transparent_68%)]"
          />
          <div className="relative mx-auto w-full max-w-5xl px-6">
            <h2 className="text-4xl font-medium tracking-tight text-zinc-900">
              Your agent is waiting for a runtime.
            </h2>
            <p className="mx-auto mt-6 max-w-xl text-lg text-zinc-600">
              Add two lines to your MCP config and let your agent ship its
              first workflow in minutes.
            </p>
            <div className="mt-10 flex justify-center gap-4">
              <a
                className="rounded-full bg-zinc-900 px-8 py-4 text-sm font-medium text-white shadow-lg transition-transform hover:scale-105 hover:bg-zinc-800"
                href="https://app.velane.sh"
              >
                Start building for free
              </a>
              <a
                className="rounded-full border border-black/10 px-8 py-4 text-sm font-medium text-zinc-600 transition-colors hover:bg-black/5"
                href="https://docs.velane.sh"
                target="_blank"
                rel="noreferrer"
              >
                Read the docs
              </a>
            </div>
          </div>
        </section>
      </main>

      {/* Footer */}
      <footer className="relative overflow-hidden border-t border-black/5 bg-white py-16">
        <div className="pointer-events-none absolute bottom-[-20px] left-0 right-0 -z-10 select-none text-center">
          <span className="block text-[120px] font-bold leading-[0.75] tracking-[-0.065em] text-zinc-900 opacity-[0.035] md:text-[180px] lg:text-[220px] xl:text-[260px]">
            Velane
          </span>
        </div>

        <div className="relative z-10 mx-auto grid max-w-5xl grid-cols-2 gap-8 px-6 md:grid-cols-3 lg:gap-12">
          <div className="col-span-2 flex flex-col items-start gap-6 lg:col-span-1">
            <div className="text-xl font-medium tracking-tight text-zinc-900">
              Velane
            </div>
            <p className="text-sm text-zinc-500">
              Integration infrastructure agents actually control.
            </p>
          </div>

          <div className="flex flex-col gap-4">
            <span className="text-sm font-semibold text-zinc-900">Product</span>
            <a
              href="https://app.velane.sh"
              className="text-sm text-zinc-500 transition-colors hover:text-zinc-900"
            >
              Platform
            </a>
            <a
              href="https://nango.dev/api-integrations/"
              className="text-sm text-zinc-500 transition-colors hover:text-zinc-900"
            >
              Integrations
            </a>
          </div>

          <div className="flex flex-col gap-4">
            <span className="text-sm font-semibold text-zinc-900">
              Resources
            </span>
            <a
              href="https://docs.velane.sh"
              className="text-sm text-zinc-500 transition-colors hover:text-zinc-900"
            >
              Documentation
            </a>
            <a
              href="https://github.com/abskrj/velane/issues"
              className="text-sm text-zinc-500 transition-colors hover:text-zinc-900"
            >
              Issue Tracker
            </a>
            <a
              href="https://github.com/abskrj/velane"
              className="text-sm text-zinc-500 transition-colors hover:text-zinc-900"
            >
              GitHub
            </a>
          </div>
        </div>

        <div className="relative z-10 mx-auto mt-16 flex max-w-5xl flex-col items-center justify-between gap-4 border-t border-black/5 px-6 pt-8 sm:flex-row">
          <div className="text-sm text-zinc-500">
            © {new Date().getFullYear()} Velane. All rights reserved.
          </div>
          <div className="flex gap-6">
            <Link
              href="/privacy"
              className="text-sm text-zinc-500 transition-colors hover:text-zinc-900"
            >
              Privacy Policy
            </Link>
            <Link
              href="/terms"
              className="text-sm text-zinc-500 transition-colors hover:text-zinc-900"
            >
              Terms of Service
            </Link>
          </div>
        </div>
      </footer>
    </div>
  );
}
