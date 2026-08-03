import type { Metadata } from "next";
import Image from "next/image";
import Link from "next/link";

export const metadata: Metadata = {
  title: "Contact Velane",
  description: "Get in touch with the Velane team.",
};

export default function ContactPage() {
  return (
    <div className="flex min-h-screen flex-col bg-[#FAFAFA] text-zinc-900">
      <header className="sticky top-0 z-50 border-b border-black/5 bg-[#FAFAFA]/80 backdrop-blur-md">
        <div className="mx-auto flex w-full max-w-5xl items-center justify-between px-6 py-4">
          <Link href="/" className="flex items-center gap-2">
            <Image
              src="/logo.png"
              alt="Velane Logo"
              width={24}
              height={24}
              className="rounded"
            />
            <span className="text-xl font-medium tracking-tight">Velane</span>
          </Link>
          <Link
            href="https://app.velane.sh"
            className="rounded-full bg-zinc-900 px-4 py-2 text-sm font-medium text-white shadow-sm transition-transform hover:scale-105 hover:bg-zinc-800"
          >
            Start building
          </Link>
        </div>
      </header>

      <main className="flex flex-1 items-center justify-center px-6 py-20">
        <section className="w-full max-w-2xl rounded-3xl border border-black/5 bg-white px-8 py-14 text-center shadow-sm sm:px-14">
          <p className="text-xs font-semibold uppercase tracking-[0.2em] text-zinc-400">
            Contact
          </p>
          <h1 className="mt-4 text-4xl font-medium tracking-tight text-zinc-900 sm:text-5xl">
            Let&apos;s talk
          </h1>
          <p className="mx-auto mt-5 max-w-lg text-base leading-relaxed text-zinc-600 sm:text-lg">
            Have a question about Velane, need help getting started, or want to
            discuss your deployment? Send us an email and we&apos;ll get back to
            you.
          </p>
          <a
            href="mailto:abhi@velane.sh"
            className="mt-8 inline-flex rounded-lg bg-zinc-900 px-6 py-3 text-sm font-medium text-white transition-colors hover:bg-zinc-800"
          >
            Email us
          </a>
          <p className="mt-4 text-sm text-zinc-500">abhi@velane.sh</p>
        </section>
      </main>

      <footer className="border-t border-black/5 py-10">
        <div className="mx-auto max-w-5xl px-6 text-center text-sm text-zinc-500">
          © {new Date().getFullYear()} Velane. All rights reserved.
        </div>
      </footer>
    </div>
  );
}
