import { Plus } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../lib/api'
import type { SandboxRecipe } from '../types'

export default function SandboxImageRecipesPage() {
  const [recipes, setRecipes] = useState<SandboxRecipe[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    api.listSandboxImageRecipes({ limit: 100 })
      .then(response => { if (!cancelled) setRecipes(response.items ?? []) })
      .catch(err => { if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load image recipes.') })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [])

  return (
    <div className="mx-auto max-w-5xl">
      <header className="mb-6 flex flex-wrap items-start justify-between gap-4"><div><p className="text-xs font-semibold uppercase tracking-[0.09em] text-gray-500">Sandboxes / Image recipes</p><h1 className="mt-2 text-2xl font-bold tracking-tight text-gray-900">Image recipes</h1><p className="mt-1 text-sm text-gray-500">Reproducible, versioned images for durable workspaces.</p></div><Link to="/dashboard/sandbox-images/new" className="flex h-9 items-center gap-2 rounded-lg bg-gray-900 px-3 text-sm font-medium text-white hover:bg-gray-800"><Plus size={15} />New recipe</Link></header>
      {error && <div role="alert" className="mb-5 rounded-lg bg-red-50 p-3 text-sm text-red-700">{error}</div>}
      <section className="overflow-hidden rounded-lg border border-gray-200 bg-white">
        {loading ? <p className="px-5 py-10 text-sm text-gray-500">Loading image recipes…</p> : recipes.length === 0 ? <div className="px-5 py-12 text-center"><p className="font-medium text-gray-900">No image recipes yet</p><p className="mt-1 text-sm text-gray-500">Create a recipe, then build an immutable version before creating a sandbox.</p></div> : <ul className="divide-y divide-gray-100">{recipes.map(recipe => <li key={recipe.id}><Link to={`/dashboard/sandbox-images/${recipe.id}`} className="block px-5 py-4 hover:bg-gray-50"><p className="font-medium text-gray-900">{recipe.name}</p><p className="mt-1 text-sm text-gray-500">{recipe.description || 'No description'}</p><code className="mt-2 block text-xs text-gray-400">{recipe.slug}</code></Link></li>)}</ul>}
      </section>
    </div>
  )
}
