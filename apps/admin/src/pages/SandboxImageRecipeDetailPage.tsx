import { Plus, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import ConfirmActionDialog from '../components/ConfirmActionDialog'
import SandboxActivityPanel from '../components/SandboxActivityPanel'
import { api } from '../lib/api'
import type { SandboxEvent, SandboxLog, SandboxRecipe, SandboxRecipeDocument, SandboxRecipeVersion } from '../types'

const EMPTY_DOCUMENT: SandboxRecipeDocument = {
  schema_version: '1',
  platform: 'linux',
  architecture: 'x86_64',
  base_image: 'registry.example.invalid/velane/sandbox@sha256:',
  profile_version_ids: [],
  guest_protocol: '1',
  install_groups: [],
  external_inputs: [],
}

function idempotencyKey() { return crypto.randomUUID() }

function NewRecipePage() {
  const navigate = useNavigate()
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [description, setDescription] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  async function create() {
    if (!name.trim()) { setError('Enter a recipe name.'); return }
    if (slug && !/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(slug)) { setError('Use lowercase letters, numbers, and hyphens for the slug.'); return }
    setSaving(true); setError('')
    try {
      const response = await api.createSandboxImageRecipe({ name: name.trim(), slug: slug || undefined, description: description.trim() || undefined }, idempotencyKey())
      navigate(`/dashboard/sandbox-images/${response.recipe.id}`)
    } catch (err) { setError(err instanceof Error ? err.message : 'Failed to create image recipe.') } finally { setSaving(false) }
  }

  return <div className="mx-auto max-w-xl"><header className="mb-6"><p className="text-xs font-semibold uppercase tracking-[0.09em] text-gray-500">Image recipes / New</p><h1 className="mt-2 text-2xl font-bold tracking-tight text-gray-900">New image recipe</h1><p className="mt-1 text-sm text-gray-500">Create recipe metadata, then define an immutable build version.</p></header>{error && <div role="alert" className="mb-4 rounded-lg bg-red-50 p-3 text-sm text-red-700">{error}</div>}<section className="space-y-4 rounded-lg border border-gray-200 bg-white p-5"><label className="block"><span className="mb-1.5 block text-sm font-medium text-gray-700">Name</span><input value={name} onChange={event => { setName(event.target.value); if (!slug) setSlug(event.target.value.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '')) }} className="h-9 w-full rounded-lg border border-gray-300 px-3 text-sm outline-none focus:border-gray-500 focus:ring-2 focus:ring-gray-200" /></label><label className="block"><span className="mb-1.5 block text-sm font-medium text-gray-700">Slug <span className="font-normal text-gray-400">(optional)</span></span><input value={slug} onChange={event => setSlug(event.target.value)} className="h-9 w-full rounded-lg border border-gray-300 px-3 font-mono text-sm outline-none focus:border-gray-500 focus:ring-2 focus:ring-gray-200" /></label><label className="block"><span className="mb-1.5 block text-sm font-medium text-gray-700">Description <span className="font-normal text-gray-400">(optional)</span></span><textarea value={description} onChange={event => setDescription(event.target.value)} className="min-h-24 w-full rounded-lg border border-gray-300 p-3 text-sm outline-none focus:border-gray-500 focus:ring-2 focus:ring-gray-200" /></label><button type="button" disabled={saving} onClick={create} className="w-full rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50">{saving ? 'Creating recipe…' : 'Create recipe'}</button></section></div>
}

export default function SandboxImageRecipeDetailPage() {
  const { recipeId = '' } = useParams<{ recipeId: string }>()
  const navigate = useNavigate()
  const [recipe, setRecipe] = useState<SandboxRecipe | null>(null)
  const [versions, setVersions] = useState<SandboxRecipeVersion[]>([])
  const [document, setDocument] = useState<SandboxRecipeDocument>(EMPTY_DOCUMENT)
  const [documentText, setDocumentText] = useState(JSON.stringify(EMPTY_DOCUMENT, null, 2))
  const [events, setEvents] = useState<SandboxEvent[]>([])
  const [logs, setLogs] = useState<SandboxLog[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [confirmDelete, setConfirmDelete] = useState(false)

  const latest = versions[0]
  useEffect(() => {
    if (recipeId === 'new') { setLoading(false); return }
    let cancelled = false
    Promise.all([api.getSandboxImageRecipe(recipeId), api.listSandboxImageRecipeVersions(recipeId, { limit: 100 })])
      .then(([nextRecipe, response]) => {
        if (cancelled) return
        setRecipe(nextRecipe)
        setVersions(response.items ?? [])
        const current = response.items?.[0]
        if (current) return api.getSandboxImageRecipeVersion(recipeId, current.version_number).then(version => {
          if (!cancelled && version.document) {
            setDocument(version.document)
            setDocumentText(JSON.stringify(version.document, null, 2))
          }
        })
      })
      .catch(err => { if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load image recipe.') })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [recipeId])

  useEffect(() => {
    if (!recipe || !latest) return
    let cancelled = false
    Promise.all([api.listSandboxImageRecipeVersionEvents(recipe.id, latest.version_number), api.listSandboxImageRecipeVersionLogs(recipe.id, latest.version_number)])
      .then(([eventPage, logPage]) => { if (!cancelled) { setEvents(eventPage.items ?? []); setLogs(logPage.items ?? []) } })
      .catch(() => { if (!cancelled) { setEvents([]); setLogs([]) } })
    return () => { cancelled = true }
  }, [latest, recipe])

  async function build() {
    if (!recipe) return
    setSaving(true); setError('')
    try {
      const response = await api.createSandboxImageRecipeVersion(recipe.id, document, idempotencyKey())
      setVersions(current => [response.version, ...current])
    } catch (err) { setError(err instanceof Error ? err.message : 'Failed to build image recipe version.') } finally { setSaving(false) }
  }

  async function deleteRecipe() {
    if (!recipe) return
    setSaving(true); setError('')
    try { await api.deleteSandboxImageRecipe(recipe.id, idempotencyKey()); navigate('/dashboard/sandbox-images') } catch (err) { setError(err instanceof Error ? err.message : 'Failed to delete image recipe.') } finally { setSaving(false); setConfirmDelete(false) }
  }

  if (loading) return <p className="text-sm text-gray-500">Loading image recipe…</p>
  if (recipeId === 'new') return <NewRecipePage />
  if (!recipe) return <div><Link to="/dashboard/sandbox-images" className="text-sm text-gray-600 hover:underline">Back to recipes</Link><p className="mt-5 text-sm text-red-700">{error || 'Image recipe not found.'}</p></div>

  return <div className="mx-auto max-w-6xl"><header className="mb-6 flex flex-wrap items-start justify-between gap-4"><div><p className="text-xs font-semibold uppercase tracking-[0.09em] text-gray-500">Image recipes / {recipe.name}</p><h1 className="mt-2 text-2xl font-bold tracking-tight text-gray-900">{recipe.name}</h1><p className="mt-1 text-sm text-gray-500">Define a reproducible image. Successful versions are immutable.</p></div><div className="flex gap-3"><Link to="/dashboard/sandbox-images" className="text-sm font-medium text-gray-600 hover:underline">Back to recipes</Link><button type="button" onClick={() => setConfirmDelete(true)} className="inline-flex items-center gap-1 text-sm font-medium text-red-700 hover:underline"><Trash2 size={14} />Delete</button></div></header>{error && <div role="alert" className="mb-5 rounded-lg bg-red-50 p-3 text-sm text-red-700">{error}</div>}<div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]"><div className="space-y-5"><section className="rounded-lg border border-gray-200 bg-white p-5"><h2 className="text-sm font-semibold text-gray-900">Immutable recipe document</h2><p className="mt-1 text-sm leading-6 text-gray-500">Use canonical JSON with a digest-pinned base image, immutable repository locks, exact package digests, immutable profile IDs, and credential-free HTTPS inputs.</p><textarea value={documentText} onChange={event => { const nextText = event.target.value; setDocumentText(nextText); try { setDocument(JSON.parse(nextText) as SandboxRecipeDocument); setError('') } catch { setError('Recipe document must be valid JSON.') } }} aria-label="Recipe document" className="mt-4 h-[34rem] w-full rounded-lg bg-gray-950 p-4 font-mono text-xs leading-5 text-gray-100 outline-none ring-1 ring-gray-800 focus:ring-2 focus:ring-gray-500" /></section><SandboxActivityPanel events={events} logs={logs} /></div><aside className="space-y-5"><section className="rounded-lg border border-gray-200 bg-white p-5"><h2 className="text-sm font-semibold text-gray-900">Build state</h2><p className="mt-1 text-sm text-gray-500">{latest ? `Latest version: v${latest.version_number} (${latest.status})` : 'Submit the first immutable version when ready.'}</p>{latest?.failure_message && <p className="mt-3 text-sm text-red-700">{latest.failure_message}</p>}<button type="button" disabled={saving || Boolean(error)} onClick={build} className="mt-5 flex w-full items-center justify-center gap-2 rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"><Plus size={15} />{saving ? 'Starting build…' : 'Build immutable version'}</button><p className="mt-3 text-xs leading-5 text-gray-500">Existing sandboxes remain pinned to their selected recipe version.</p></section><section className="rounded-lg border border-gray-200 bg-white p-5"><h2 className="text-sm font-semibold text-gray-900">Version history</h2>{versions.length === 0 ? <p className="mt-3 text-sm text-gray-500">No versions yet.</p> : <ol className="mt-3 space-y-3">{versions.map(version => <li key={version.id} className="rounded-lg border border-gray-100 p-3"><div className="flex justify-between gap-3"><span className="font-medium text-gray-800">v{version.version_number}</span><span className="capitalize text-sm text-gray-500">{version.status}</span></div><p className="mt-1 text-xs text-gray-500">{new Date(version.created_at).toLocaleString()}</p></li>)}</ol>}</section></aside></div>{confirmDelete && <ConfirmActionDialog title="Delete image recipe" description="Delete this recipe metadata. Referenced recipes cannot be deleted." confirmLabel="Delete recipe" danger busy={saving} onCancel={() => setConfirmDelete(false)} onConfirm={deleteRecipe} />}</div>
}
