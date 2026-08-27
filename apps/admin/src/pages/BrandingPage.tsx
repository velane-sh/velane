import { useEffect, useState, type FormEvent } from 'react'
import { api } from '../lib/api'
import type { Branding } from '../types'

const DEFAULT_BRANDING: Branding = {
  logo_url: '',
  accent_color: '#000',
  font_family: 'Inter, sans-serif',
  custom_domain: '',
  hide_branding: false,
}

export default function BrandingPage() {
  const [branding, setBranding] = useState<Branding>(DEFAULT_BRANDING)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    api
      .getBranding()
      .then((b) => setBranding({ ...DEFAULT_BRANDING, ...b }))
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  const handleSave = async (e: FormEvent) => {
    e.preventDefault()
    setSaving(true)
    setError('')
    try {
      await api.updateBranding(branding)
      setSaved(true)
      setTimeout(() => setSaved(false), 3000)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save branding')
    } finally {
      setSaving(false)
    }
  }

  if (loading) return <p className="text-sm text-gray-500">Loading...</p>

  return (
    <div className="flex gap-8">
      {/* Form */}
      <div className="flex-1">
        <h1 className="mb-6 text-2xl font-bold text-gray-900">Branding</h1>

        {error && (
          <div className="mb-4 rounded-md bg-red-50 p-3 text-sm text-red-700">{error}</div>
        )}
        {saved && (
          <div className="mb-4 rounded-md bg-green-50 p-3 text-sm text-green-700">Branding saved!</div>
        )}

        <form onSubmit={handleSave} className="space-y-6">
          <div className="rounded-lg border border-gray-200 bg-white p-6 shadow-sm">
            <div className="mb-4">
              <label className="mb-1 block text-sm font-medium text-gray-700">Logo URL</label>
              <input
                type="url"
                value={branding.logo_url ?? ''}
                onChange={(e) => setBranding((b) => ({ ...b, logo_url: e.target.value }))}
                className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-gray-400 focus:outline-none"
                placeholder="https://example.com/logo.png"
              />
              {branding.logo_url && (
                <img
                  src={branding.logo_url}
                  alt="Logo preview"
                  className="mt-2 h-10 rounded object-contain"
                  onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
                />
              )}
            </div>

            <div className="mb-4">
              <label className="mb-1 block text-sm font-medium text-gray-700">Accent Color</label>
              <div className="flex items-center gap-3">
                <input
                  type="color"
                  value={branding.accent_color ?? '#000'}
                  onChange={(e) => setBranding((b) => ({ ...b, accent_color: e.target.value }))}
                  className="h-9 w-16 cursor-pointer rounded border border-gray-300"
                />
                <input
                  type="text"
                  value={branding.accent_color ?? ''}
                  onChange={(e) => setBranding((b) => ({ ...b, accent_color: e.target.value }))}
                  className="w-32 rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-gray-400 focus:outline-none"
                  placeholder="#000"
                />
              </div>
            </div>

            <div className="mb-4">
              <label className="mb-1 block text-sm font-medium text-gray-700">Font Family</label>
              <input
                type="text"
                value={branding.font_family ?? ''}
                onChange={(e) => setBranding((b) => ({ ...b, font_family: e.target.value }))}
                className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-gray-400 focus:outline-none"
                placeholder="Inter, sans-serif"
              />
            </div>

            <div className="mb-4">
              <div className="mb-1 flex items-center gap-2">
                <label className="text-sm font-medium text-gray-700">Custom Domain</label>
                <span className="rounded-full bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-500">
                  Coming soon
                </span>
              </div>
              <input
                type="text"
                disabled
                value={branding.custom_domain ?? ''}
                className="w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm text-gray-400"
                placeholder="workflows.yourcompany.com"
              />
              <p className="mt-1 text-xs text-gray-400">
                Custom domains for the embed dashboard aren&apos;t available yet.
              </p>
            </div>

            <div className="flex items-center gap-3">
              <input
                id="hide-branding"
                type="checkbox"
                checked={branding.hide_branding ?? false}
                onChange={(e) => setBranding((b) => ({ ...b, hide_branding: e.target.checked }))}
                className="h-4 w-4 rounded border-gray-300 text-gray-900"
              />
              <label htmlFor="hide-branding" className="text-sm font-medium text-gray-700">
                Hide Velane branding
              </label>
            </div>
          </div>

          <button
            type="submit"
            disabled={saving}
            className="rounded-md bg-gray-900 px-6 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
          >
            {saving ? 'Saving...' : 'Save Branding'}
          </button>
        </form>
      </div>

      {/* Live preview */}
      <div className="w-72 flex-shrink-0">
        <h2 className="mb-3 text-sm font-semibold text-gray-700">Preview</h2>
        <div
          className="rounded-lg border border-gray-200 bg-white p-4 shadow-sm"
          style={{ fontFamily: branding.font_family || 'inherit' }}
        >
          {branding.logo_url ? (
            <img
              src={branding.logo_url}
              alt="Logo"
              className="mb-3 h-8 object-contain"
              onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
            />
          ) : (
            <div className="mb-3 h-8 w-24 rounded bg-gray-200" />
          )}
          <div
            className="mb-2 h-6 w-full rounded"
            style={{ backgroundColor: branding.accent_color || '#000' }}
          />
          <p className="text-xs text-gray-600">Sample embed content</p>
          {!branding.hide_branding && (
            <p className="mt-2 text-xs text-gray-400">Powered by Velane</p>
          )}
        </div>
      </div>
    </div>
  )
}
