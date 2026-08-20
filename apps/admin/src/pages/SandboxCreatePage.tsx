import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import Select from '../components/Select'
import { APIError, api } from '../lib/api'
import type { SandboxProfile, SandboxRecipe, SandboxRecipeVersion } from '../types'

function idempotencyKey() { return crypto.randomUUID() }

export default function SandboxCreatePage() {
  const navigate = useNavigate()
  const [name, setName] = useState('')
  const [recipes, setRecipes] = useState<SandboxRecipe[]>([])
  const [versions, setVersions] = useState<SandboxRecipeVersion[]>([])
  const [profiles, setProfiles] = useState<SandboxProfile[]>([])
  const [recipeID, setRecipeID] = useState('')
  const [versionID, setVersionID] = useState('')
  const [profileID, setProfileID] = useState('')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    Promise.all([api.listSandboxImageRecipes({ limit: 100 }), api.listSandboxProfiles()])
      .then(([recipesResponse, profilesResponse]) => {
        if (cancelled) return
        setRecipes(recipesResponse.items ?? [])
        setProfiles((profilesResponse.items ?? []).filter(profile => profile.status === 'active'))
      })
      .catch(err => { if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load create options.') })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [])

  useEffect(() => {
    if (!recipeID) {
      setVersions([])
      setVersionID('')
      return
    }
    let cancelled = false
    api.listSandboxImageRecipeVersions(recipeID, { limit: 100 })
      .then(response => { if (!cancelled) { setVersions((response.items ?? []).filter(version => version.status === 'ready')); setVersionID('') } })
      .catch(err => { if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load recipe versions.') })
    return () => { cancelled = true }
  }, [recipeID])

  const selectedRecipe = recipes.find(recipe => recipe.id === recipeID)
  const selectedVersion = versions.find(version => version.id === versionID)
  const selectedProfile = profiles.find(profile => profile.id === profileID)
  const compatible = Boolean(selectedVersion && selectedProfile)
  const profileSummary = useMemo(() => selectedProfile ? `${selectedProfile.vcpu} vCPU · ${Math.max(1, Math.round(selectedProfile.memory_mb / 1024))} GB memory` : '—', [selectedProfile])

  async function create() {
    if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(name)) {
      setError('Use lowercase letters, numbers, and hyphens for the sandbox name.')
      return
    }
    if (!selectedVersion || !selectedProfile) {
      setError('Select a ready recipe version and an active immutable profile.')
      return
    }
    setSaving(true)
    setError('')
    try {
      const response = await api.createSandbox({ name, recipe_version_id: selectedVersion.id, profile_version_id: selectedProfile.id }, idempotencyKey())
      if (!response.sandbox) throw new Error('Sandbox creation response did not include a sandbox.')
      navigate(`/dashboard/sandboxes/${response.sandbox.id}`)
    } catch (err) {
      setError(err instanceof APIError ? err.message : err instanceof Error ? err.message : 'Failed to create sandbox.')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="mx-auto max-w-6xl">
      <header className="mb-6"><p className="text-xs font-semibold uppercase tracking-[0.09em] text-gray-500">Sandboxes / New</p><h1 className="mt-2 text-2xl font-bold tracking-tight text-gray-900">Create sandbox</h1><p className="mt-1 text-sm text-gray-500">Provision a durable workspace from a versioned image recipe.</p></header>
      {error && <div role="alert" className="mb-5 rounded-lg bg-red-50 p-3 text-sm text-red-700">{error}</div>}
      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_22rem]">
        <div className="space-y-5">
          {loading ? <section className="rounded-lg border border-gray-200 bg-white p-5 text-sm text-gray-500">Loading ready recipes and active profiles…</section> : <>
            <section className="rounded-lg border border-gray-200 bg-white p-5"><h2 className="text-sm font-semibold text-gray-900">Workspace</h2><label className="mt-4 block"><span className="mb-1.5 block text-sm font-medium text-gray-700">Name</span><input value={name} onChange={event => setName(event.target.value)} placeholder="research-linux" className="h-9 w-full rounded-lg border border-gray-300 px-3 text-sm outline-none focus:border-gray-500 focus:ring-2 focus:ring-gray-200" /></label><p className="mt-1.5 text-xs text-gray-500">Lowercase letters, numbers, and hyphens.</p></section>
            <section className="rounded-lg border border-gray-200 bg-white p-5"><div className="flex justify-between gap-3"><div><h2 className="text-sm font-semibold text-gray-900">Image recipe</h2><p className="mt-1 text-sm text-gray-500">Recipes are immutable after a version builds successfully.</p></div><Link to="/dashboard/sandbox-images" className="text-sm font-medium text-gray-700 hover:underline">Manage recipes</Link></div><div className="mt-5 grid gap-4 sm:grid-cols-2"><label><span className="mb-1.5 block text-sm font-medium text-gray-700">Recipe</span><Select value={recipeID} onChange={event => setRecipeID(event.target.value)}><option value="">Select a recipe</option>{recipes.map(recipe => <option key={recipe.id} value={recipe.id}>{recipe.name}</option>)}</Select></label><label><span className="mb-1.5 block text-sm font-medium text-gray-700">Ready version</span><Select value={versionID} onChange={event => setVersionID(event.target.value)} disabled={!recipeID}><option value="">Select a version</option>{versions.map(version => <option key={version.id} value={version.id}>v{version.version_number}</option>)}</Select></label></div></section>
            <section className="rounded-lg border border-gray-200 bg-white p-5"><h2 className="text-sm font-semibold text-gray-900">Sandbox profile</h2><label className="mt-4 block"><span className="mb-1.5 block text-sm font-medium text-gray-700">Immutable profile version</span><Select value={profileID} onChange={event => setProfileID(event.target.value)}><option value="">Select a profile</option>{profiles.map(profile => <option key={profile.id} value={profile.id}>{profile.name} · v{profile.version} · {profile.vcpu} vCPU / {profile.memory_mb} MB</option>)}</Select></label><p className="mt-2 text-xs leading-5 text-gray-500">The profile pins compute and VM layout for this sandbox. It cannot be changed after creation.</p></section>
          </>}
        </div>
        <aside className="h-fit rounded-lg border border-gray-200 bg-white p-5"><h2 className="text-sm font-semibold text-gray-900">Review</h2><dl className="mt-4 space-y-3 text-sm"><div className="flex justify-between gap-4"><dt className="text-gray-500">Name</dt><dd className="font-mono text-gray-800">{name || '—'}</dd></div><div className="flex justify-between gap-4"><dt className="text-gray-500">Recipe</dt><dd className="text-right text-gray-800">{selectedRecipe && selectedVersion ? `${selectedRecipe.name} v${selectedVersion.version_number}` : '—'}</dd></div><div className="flex justify-between gap-4"><dt className="text-gray-500">Profile</dt><dd className="text-right text-gray-800">{selectedProfile ? `${selectedProfile.name} v${selectedProfile.version}` : '—'}</dd></div><div className="flex justify-between gap-4"><dt className="text-gray-500">Capacity</dt><dd className="text-right text-gray-800">{profileSummary}</dd></div></dl><button type="button" disabled={loading || saving || !compatible} onClick={create} className="mt-6 w-full rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:cursor-not-allowed disabled:opacity-50">{saving ? 'Creating sandbox…' : 'Create sandbox'}</button><p className="mt-4 text-xs leading-5 text-gray-500">Creating starts the sandbox immediately. Stopping records a full recovery point before compute is released.</p></aside>
      </div>
    </div>
  )
}
