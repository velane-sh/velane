import React from 'react';
import Link from 'next/link';
import Image from 'next/image';

export default function PrivacyPolicy() {
  const lastUpdated = "June 9, 2026";

  return (
    <div className="min-h-screen bg-[#FAFAFA] text-zinc-900">
      {/* Header */}
      <header className="sticky top-0 z-50 border-b border-black/5 bg-[#FAFAFA]/80 backdrop-blur-md">
        <div className="mx-auto flex w-full max-w-5xl items-center justify-between px-6 py-4">
          <Link href="/" className="flex items-center gap-2">
            <Image src="/logo.png" alt="Velane Logo" width={24} height={24} className="rounded" />
            <span className="text-xl font-medium tracking-tight">Velane</span>
          </Link>
          <div className="flex items-center gap-6">
            <Link
              href="https://github.com/abskrj/velane"
              target="_blank"
              rel="noreferrer"
              className="text-sm text-zinc-500 transition-colors hover:text-zinc-900 hidden sm:inline-block"
            >
              Docs
            </Link>
            <Link
              href="https://app.velane.sh"
              className="rounded-full bg-zinc-900 px-4 py-2 text-sm font-medium text-white shadow-sm transition-transform hover:scale-105 hover:bg-zinc-800"
            >
              Start building
            </Link>
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-[680px] px-6 py-16">
        <div className="mb-14">
          <h1 className="text-4xl font-medium tracking-[-0.5px]">Privacy Policy</h1>
          <p className="mt-3 text-sm text-zinc-500">Last updated: {lastUpdated}</p>
        </div>

        <div className="space-y-10 text-[15px] leading-[1.75] text-zinc-700">
          <p className="text-base text-zinc-800">
            Velane ("we", "us", or "our") respects your privacy. This Privacy Policy explains how we collect, use, disclose, and safeguard your information when you use Velane&apos;s hosted cloud service, our website, and related Velane-operated infrastructure.
          </p>

          {/* Section 1 */}
          <section>
            <h2 className="mb-4 text-xl font-medium tracking-tight text-zinc-900">1. Scope: Cloud vs Self-Hosted</h2>
            <p className="mb-3">
              This Privacy Policy applies when you use Velane&apos;s <span className="font-medium text-zinc-900">hosted cloud service</span>, including app.velane.sh and related Velane-operated infrastructure.
            </p>
            <p>
              If you <span className="font-medium text-zinc-900">self-host</span> Velane on your own infrastructure, you control and own your tenant data, including snippets, logs, integration connections, and secrets. We do not collect or process that data unless you voluntarily share it with us (for example, through support requests). Your internal handling of self-hosted data is governed by your own policies and deployment choices. This website may still collect limited technical data when you visit it, as described below.
            </p>
          </section>

          {/* Section 2 */}
          <section>
            <h2 className="mb-4 text-xl font-medium tracking-tight text-zinc-900">2. Information We Collect</h2>
            <p className="mb-3">On Velane Cloud, we collect the following types of information:</p>
            <ul className="list-disc pl-6 space-y-2.5">
              <li><span className="font-medium text-zinc-900">Account Information:</span> When you create an account or tenant, we collect your email address and name (if provided). Passwords are stored as one-way hashes. We never store plaintext passwords. We also issue session tokens to keep you signed in.</li>
              <li><span className="font-medium text-zinc-900">Usage Data:</span> We collect information about how you and your AI agents interact with the Service, including API calls, snippet invocations, logs, and performance metrics.</li>
              <li><span className="font-medium text-zinc-900">Integration Data:</span> When you connect third-party services (via Nango or direct OAuth), we store connection metadata and tokens securely on your behalf. We never store or access the underlying data from those services unless explicitly invoked by your code.</li>
              <li><span className="font-medium text-zinc-900">Technical Data:</span> IP addresses, browser type, device information, and cookies for authentication and analytics.</li>
            </ul>
          </section>

          {/* Section 3 */}
          <section>
            <h2 className="mb-4 text-xl font-medium tracking-tight text-zinc-900">3. How We Use Your Information</h2>
            <p className="mb-3">We use the information we collect to:</p>
            <ul className="list-disc pl-6 space-y-2">
              <li>Provide, operate, and maintain the Service</li>
              <li>Process and fulfill your requests and transactions</li>
              <li>Improve and personalize the Service</li>
              <li>Detect and prevent fraud, abuse, and security issues</li>
              <li>Communicate with you about updates, security alerts, and support</li>
              <li>Comply with legal obligations</li>
            </ul>
          </section>

          {/* Section 4 */}
          <section>
            <h2 className="mb-4 text-xl font-medium tracking-tight text-zinc-900">4. Sharing of Information</h2>
            <p className="mb-3">We do not sell your personal information. We may share information in the following limited cases:</p>
            <ul className="list-disc pl-6 space-y-2.5">
              <li>With service providers who help us operate the Service (hosting, analytics, error monitoring) under strict confidentiality obligations.</li>
              <li>When required by law, regulation, or valid legal process.</li>
              <li>In connection with a merger, acquisition, or sale of assets (we will notify you if this happens).</li>
            </ul>
          </section>

          {/* Section 5 */}
          <section>
            <h2 className="mb-4 text-xl font-medium tracking-tight text-zinc-900">5. Data Security</h2>
            <p>
              We use industry-standard security practices, including encryption in transit (TLS), encryption at rest for secrets, and strict access controls. However, no method of transmission over the Internet is 100% secure.
            </p>
          </section>

          {/* Section 6 */}
          <section>
            <h2 className="mb-4 text-xl font-medium tracking-tight text-zinc-900">6. Your Rights</h2>
            <p className="mb-3">Depending on your location, you may have the right to:</p>
            <ul className="list-disc pl-6 space-y-2">
              <li>Access, correct, or delete your personal information</li>
              <li>Object to or restrict certain processing</li>
              <li>Request a copy of your data (data portability)</li>
              <li>Withdraw consent at any time (where processing is based on consent)</li>
            </ul>
            <p className="mt-3">
              To exercise these rights, please contact us at the email below.
            </p>
          </section>

          {/* Section 7 */}
          <section>
            <h2 className="mb-4 text-xl font-medium tracking-tight text-zinc-900">7. Data Retention</h2>
            <p>
              We retain your information for as long as your account is active or as needed to provide the Service. You may request deletion of your account and associated data at any time.
            </p>
          </section>

          {/* Section 8 */}
          <section>
            <h2 className="mb-4 text-xl font-medium tracking-tight text-zinc-900">8. Third-Party Services</h2>
            <p>
              The Service integrates with third-party providers (via Nango and direct OAuth). Those providers have their own privacy policies. We are not responsible for the practices of third parties you choose to connect.
            </p>
          </section>

          {/* Section 9 */}
          <section>
            <h2 className="mb-4 text-xl font-medium tracking-tight text-zinc-900">9. Changes to This Policy</h2>
            <p>
              We may update this Privacy Policy from time to time. We will notify you of material changes by posting the new policy on this page and updating the "Last updated" date.
            </p>
          </section>

          {/* Section 10 */}
          <section>
            <h2 className="mb-4 text-xl font-medium tracking-tight text-zinc-900">10. Contact Us</h2>
            <p>
              If you have questions or concerns about this Privacy Policy, please contact us at:
            </p>
            <p className="mt-2">
              <a href="mailto:abhi@velane.sh" className="font-medium text-zinc-900 underline hover:no-underline">abhi@velane.sh</a>
            </p>
          </section>
        </div>
      </main>

      {/* Minimal footer */}
      <footer className="border-t border-black/5 py-10">
        <div className="mx-auto max-w-5xl px-6 text-center text-sm text-zinc-500">
          © {new Date().getFullYear()} Velane. All rights reserved.
        </div>
      </footer>
    </div>
  );
}
