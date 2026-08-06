import { useState, type FormEvent } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { AlertCircle, Eye, EyeOff, Loader2 } from 'lucide-react'
import { api } from '../lib/api'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import SocialLoginButtons from '../components/SocialLoginButtons'
import AuthBrandHeader from '../components/AuthBrandHeader'

const OAUTH_ERROR_MESSAGES: Record<string, string> = {
  access_denied: 'Sign-in was cancelled.',
  invalid_state: 'Sign-in session expired. Please try again.',
  missing_code: 'Sign-in failed. Please try again.',
  exchange_failed: 'Could not complete sign-in with the provider.',
  account_error: 'Could not sign you in. The provider may not have shared a verified email.',
  session_error: 'Could not start your session. Please try again.',
}

export default function LoginPage() {
  useDocumentTitle('Sign in')
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const oauthError = searchParams.get('auth_error')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [error, setError] = useState(oauthError ? OAUTH_ERROR_MESSAGES[oauthError] ?? 'Sign-in failed.' : '')
  const [loading, setLoading] = useState(false)
  const [org, setOrg] = useState('')
  const [ssoLoading, setSSOLoading] = useState(false)

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      await api.login(email, password)
      localStorage.removeItem('apiKey')
      navigate('/dashboard/overview')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed')
    } finally {
      setLoading(false)
    }
  }

  const handleSSO = async (e: FormEvent) => {
    e.preventDefault(); setError(''); setSSOLoading(true)
    try { const discovery = await api.discoverSSO(org.trim().toLowerCase()); window.location.assign(discovery.start_url) }
    catch (err) { setError(err instanceof Error ? err.message : 'SSO is not available for this organization'); setSSOLoading(false) }
  }

  return (
    <div className="flex min-h-screen bg-white">
      {/* Brand panel */}
      <div className="relative hidden w-1/2 flex-col justify-between overflow-hidden bg-gray-900 p-12 lg:flex">
        <div
          aria-hidden="true"
          className="absolute inset-0 bg-[radial-gradient(circle_at_1px_1px,rgba(255,255,255,0.08)_1px,transparent_0)] bg-[size:28px_28px]"
        />
        <div
          aria-hidden="true"
          className="absolute -bottom-40 -right-40 h-96 w-96 rounded-full bg-white/5 blur-3xl"
        />
        <AuthBrandHeader className="relative" />
        <div className="relative">
          <h2 className="max-w-md text-3xl font-semibold leading-tight tracking-tight text-white">
            Ship integrations without leaving your editor.
          </h2>
          <p className="mt-4 max-w-md text-sm leading-relaxed text-gray-400">
            Manage tenants, snippets, libraries, and usage from one admin portal —
            backed by a sandboxed runtime for every invocation.
          </p>
        </div>
        <p className="relative text-xs text-gray-500">
          © {new Date().getFullYear()} Velane
        </p>
      </div>

      {/* Form panel */}
      <div className="flex w-full items-center justify-center px-6 py-12 lg:w-1/2">
        <div className="w-full max-w-sm">
          <AuthBrandHeader variant="light" className="mb-8 lg:hidden" />

          <h1 className="text-2xl font-semibold tracking-tight text-gray-900">Welcome back</h1>
          <p className="mt-1.5 mb-8 text-sm text-gray-500">
            Sign in to your admin portal.
          </p>

          {error && (
            <div className="mb-5 flex items-start gap-2.5 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700">
              <AlertCircle size={16} className="mt-0.5 shrink-0" />
              <span>{error}</span>
            </div>
          )}

          <SocialLoginButtons />

          <form onSubmit={handleSSO} className="mb-6 space-y-3 rounded-lg border border-gray-200 bg-gray-50 p-4">
            <label htmlFor="sso-org" className="block text-sm font-medium text-gray-700">Sign in with SSO</label>
            <div className="flex gap-2"><input id="sso-org" required value={org} onChange={(e) => setOrg(e.target.value)} placeholder="organization-slug" className="min-w-0 flex-1 rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm focus:border-gray-900 focus:outline-none" /><button disabled={ssoLoading} className="rounded-lg bg-gray-900 px-4 py-2 text-sm text-white hover:bg-gray-800 disabled:opacity-50">{ssoLoading ? 'Starting…' : 'Continue'}</button></div>
          </form>

          <form onSubmit={handleSubmit} className="space-y-5">
            <div>
              <label htmlFor="email" className="mb-1.5 block text-sm font-medium text-gray-700">
                Email
              </label>
              <input
                id="email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
                autoComplete="email"
                className="w-full rounded-lg border border-gray-300 px-3 py-2.5 text-sm text-gray-900 placeholder:text-gray-400 transition-colors focus:border-gray-900 focus:outline-none focus:ring-2 focus:ring-gray-900/10"
                placeholder="you@example.com"
              />
            </div>
            <div>
              <label htmlFor="password" className="mb-1.5 block text-sm font-medium text-gray-700">
                Password
              </label>
              <div className="relative">
                <input
                  id="password"
                  type={showPassword ? 'text' : 'password'}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                  autoComplete="current-password"
                  className="w-full rounded-lg border border-gray-300 px-3 py-2.5 pr-10 text-sm text-gray-900 placeholder:text-gray-400 transition-colors focus:border-gray-900 focus:outline-none focus:ring-2 focus:ring-gray-900/10"
                  placeholder="••••••••"
                />
                <button
                  type="button"
                  onClick={() => setShowPassword((v) => !v)}
                  aria-label={showPassword ? 'Hide password' : 'Show password'}
                  className="absolute inset-y-0 right-0 flex items-center pr-3 text-gray-400 hover:text-gray-600"
                >
                  {showPassword ? <EyeOff size={16} /> : <Eye size={16} />}
                </button>
              </div>
            </div>
            <button
              type="submit"
              disabled={loading}
              className="flex w-full items-center justify-center gap-2 rounded-lg bg-gray-900 px-4 py-2.5 text-sm font-medium text-white transition-colors hover:bg-gray-800 active:scale-[0.99] disabled:opacity-50"
            >
              {loading && <Loader2 size={16} className="animate-spin" />}
              {loading ? 'Signing in...' : 'Sign in'}
            </button>
          </form>

          <p className="mt-6 text-center text-sm text-gray-500">
            No account?{' '}
            <Link to="/register" className="font-medium text-gray-900 underline-offset-4 hover:underline">
              Register
            </Link>
          </p>
        </div>
      </div>
    </div>
  )
}
