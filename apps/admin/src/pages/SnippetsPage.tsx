import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Plus, Trash2, FileCode2, AlertCircle } from 'lucide-react'
import { api } from '../lib/api'
import type { Snippet } from '../types'
import LanguageBadge from '../components/LanguageBadge'
import Button from '../components/Button'
import PageHeader from '../components/PageHeader'
import Modal from '../components/Modal'
import { TD, TBody, TH, THead, TR, Table } from '../components/Table'

export default function SnippetsPage() {
  const navigate = useNavigate()
  const [snippets, setSnippets] = useState<Snippet[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [showModal, setShowModal] = useState(false)
  const [newName, setNewName] = useState('')
  const [newLanguage, setNewLanguage] = useState<'bun' | 'python'>('bun')
  const [newDescription, setNewDescription] = useState('')
  const [creating, setCreating] = useState(false)

  useEffect(() => {
    load()
  }, [])

  async function load() {
    setLoading(true)
    try {
      const data = await api.listSnippets()
      setSnippets(data)
    } catch (err) {
      setError(String(err))
    } finally {
      setLoading(false)
    }
  }

  async function handleCreate() {
    if (!newName.trim()) return
    setCreating(true)
    try {
      const sn = await api.createSnippet({
        name: newName.trim(),
        language: newLanguage,
        description: newDescription.trim() || undefined,
      })
      setShowModal(false)
      setNewName('')
      setNewLanguage('bun')
      setNewDescription('')
      navigate(`/dashboard/snippets/${sn.id}`)
    } catch (err) {
      setError(String(err))
    } finally {
      setCreating(false)
    }
  }

  async function handleDelete(e: React.MouseEvent, id: string) {
    e.stopPropagation()
    if (!confirm('Delete this snippet? This cannot be undone.')) return
    try {
      await api.deleteSnippet(id)
      setSnippets((prev) => prev.filter((s) => s.id !== id))
    } catch (err) {
      setError(String(err))
    }
  }

  return (
    <div>
      <PageHeader
        title="Workflows"
        description="Manage your workflows and deployments."
        actions={(
          <Button onClick={() => setShowModal(true)}>
            <Plus size={15} />
            New Workflow
          </Button>
        )}
      />

      {error && (
        <div className="mb-6 flex items-center gap-2.5 rounded-lg border border-danger-border bg-danger-subtle px-4 py-3 text-sm text-danger-text">
          <AlertCircle size={16} className="shrink-0" />
          {error}
        </div>
      )}

      {loading && (
        <p className="text-sm text-content-subtle">Loading workflows...</p>
      )}

      {!loading && snippets.length === 0 && (
        <div className="flex flex-col items-center justify-center rounded-xl border border-dashed border-line-strong bg-surface py-24">
          <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-surface-muted">
            <FileCode2 size={22} className="text-content-subtle" />
          </div>
          <p className="text-base font-semibold text-content">No workflows yet</p>
          <p className="mt-1.5 max-w-xs text-center text-sm text-content-muted">
            You haven't created any workflows. Create one to get started with your deployments.
          </p>
          <Button
            variant="secondary"
            className="mt-6"
            onClick={() => setShowModal(true)}
          >
            Create your first workflow
          </Button>
        </div>
      )}

      {snippets.length > 0 && (
        <Table className="shadow-sm">
          <THead>
            <TR className="hover:bg-transparent">
              <TH>Name</TH>
              <TH>Language</TH>
              <TH>Slug</TH>
              <TH>Created</TH>
              <TH></TH>
            </TR>
          </THead>
          <TBody>
            {snippets.map((sn) => (
              <TR
                key={sn.id}
                interactive
                onClick={() => navigate(`/dashboard/snippets/${sn.id}`)}
              >
                <TD className="font-medium text-content">{sn.name}</TD>
                <TD>
                  <LanguageBadge language={sn.language} />
                </TD>
                <TD className="font-mono text-xs text-content-muted">{sn.slug}</TD>
                <TD className="text-content-muted">
                  {new Date(sn.created_at).toLocaleDateString()}
                </TD>
                <TD className="text-right">
                  <Button
                    variant="danger"
                    size="icon"
                    className="rounded p-1"
                    onClick={(e) => handleDelete(e, sn.id)}
                    title="Delete workflow"
                  >
                    <Trash2 size={14} />
                  </Button>
                </TD>
              </TR>
            ))}
          </TBody>
        </Table>
      )}

      <Modal
        open={showModal}
        onClose={() => setShowModal(false)}
        title="New Workflow"
        footer={(
          <>
            <Button variant="secondary" onClick={() => setShowModal(false)}>Cancel</Button>
            <Button onClick={handleCreate} disabled={creating || !newName.trim()}>
              {creating ? 'Creating...' : 'Create'}
            </Button>
          </>
        )}
      >
        <div className="mb-4">
          <label className="mb-1 block text-sm font-medium text-content">Name</label>
          <input
            className="w-full rounded-md border border-line-strong bg-surface px-3 py-2 text-sm text-content focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent-ring"
            placeholder="My Workflow"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            autoFocus
          />
        </div>

        <div className="mb-4">
          <label className="mb-1 block text-sm font-medium text-content">Language</label>
          <select
            className="w-full rounded-md border border-line-strong bg-surface px-3 py-2 text-sm text-content focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent-ring"
            value={newLanguage}
            onChange={(e) => setNewLanguage(e.target.value as 'bun' | 'python')}
          >
            <option value="bun">Bun (TypeScript)</option>
            <option value="python">Python</option>
          </select>
        </div>

        <div className="mb-6">
          <label className="mb-1 block text-sm font-medium text-content">
            Description <span className="font-normal text-content-subtle">(optional)</span>
          </label>
          <input
            className="w-full rounded-md border border-line-strong bg-surface px-3 py-2 text-sm text-content focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent-ring"
            placeholder="What does this workflow do?"
            value={newDescription}
            onChange={(e) => setNewDescription(e.target.value)}
          />
        </div>
      </Modal>
    </div>
  )
}
