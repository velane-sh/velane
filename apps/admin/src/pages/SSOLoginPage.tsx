import { useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { AlertCircle, ArrowLeft, Loader2 } from 'lucide-react'
import AuthBrandHeader from '../components/AuthBrandHeader'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { api } from '../lib/api'

export default function SSOLoginPage() {
  useDocumentTitle('Enterprise SSO')
  const [organization, setOrganization] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault()
    setError('')
    setLoading(true)

    try {
      const discovery = await api.discoverSSO(organization.trim().toLowerCase())
      window.location.assign(discovery.start_url)
    } catch {
      setError('SSO is not available for this organization. Check the organization name or contact your administrator.')
      setLoading(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-gray-50 px-6 py-12">
      <div className="w-full max-w-sm">
        <AuthBrandHeader variant="light" className="mb-10" />

        <div className="rounded-xl border border-gray-200 bg-white p-6 shadow-sm">
          <h1 className="text-2xl font-semibold tracking-tight text-gray-900">Enterprise SSO</h1>
          <p className="mt-2 text-sm leading-relaxed text-gray-500">
            Enter the organization name provided by your administrator to continue to your identity provider.
          </p>

          {error && (
            <div className="mt-5 flex items-start gap-2.5 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700">
              <AlertCircle size={16} className="mt-0.5 shrink-0" />
              <span>{error}</span>
            </div>
          )}

          <form onSubmit={handleSubmit} className="mt-6 space-y-5">
            <div>
              <label htmlFor="sso-organization" className="mb-1.5 block text-sm font-medium text-gray-700">
                Organization name
              </label>
              <input
                id="sso-organization"
                value={organization}
                onChange={(event) => setOrganization(event.target.value)}
                required
                autoComplete="organization"
                placeholder="acme"
                className="w-full rounded-lg border border-gray-300 px-3 py-2.5 text-sm text-gray-900 placeholder:text-gray-400 focus:border-gray-900 focus:outline-none focus:ring-2 focus:ring-gray-900/10"
              />
            </div>
            <button
              type="submit"
              disabled={loading}
              className="flex w-full items-center justify-center gap-2 rounded-lg bg-gray-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
            >
              {loading && <Loader2 size={16} className="animate-spin" />}
              {loading ? 'Redirecting…' : 'Continue with SSO'}
            </button>
          </form>
        </div>

        <Link to="/login" className="mt-6 flex items-center justify-center gap-2 text-sm font-medium text-gray-600 hover:text-gray-900">
          <ArrowLeft size={15} />
          Back to sign in
        </Link>
      </div>
    </div>
  )
}
