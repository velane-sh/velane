import { useEffect, useState, type FormEvent } from 'react'
import { Trash2, X } from 'lucide-react'
import { api } from '../lib/api'
import type { IntegrationConfig, TenantMember, UserGroup } from '../types'

export default function UserGroupsPage() {
  const [groups, setGroups] = useState<UserGroup[]>([])
  const [members, setMembers] = useState<TenantMember[]>([])
  const [integrations, setIntegrations] = useState<IntegrationConfig[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [creating, setCreating] = useState(false)

  const load = async () => {
    try {
      const [g, m, i] = await Promise.all([
        api.listUserGroups(),
        api.listMembers(),
        api.listConfigured(undefined, undefined, undefined, 'all'),
      ])
      setGroups(g)
      setMembers(m)
      setIntegrations(i)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load user groups')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  const run = async (fn: () => Promise<void>) => {
    setError('')
    try {
      await fn()
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Request failed')
    }
  }

  const handleCreate = async (e: FormEvent) => {
    e.preventDefault()
    setCreating(true)
    await run(async () => {
      await api.createUserGroup(name.trim(), description.trim())
      setName('')
      setDescription('')
    })
    setCreating(false)
  }

  const integrationLabel = (profileID: string) => {
    const cfg = integrations.find((i) => i.id === profileID)
    return cfg ? `${cfg.provider} · ${cfg.alias}` : profileID
  }

  return (
    <div>
      <h1 className="mb-2 text-2xl font-bold text-gray-900">User Groups</h1>
      <p className="mb-6 text-sm text-gray-500">
        Grant integrations to groups. Members of a group — and the API keys they create — are the
        only non-admin principals allowed to use those integrations, including from snippet code.
      </p>

      {error && <div className="mb-4 rounded-md bg-red-50 p-3 text-sm text-red-700">{error}</div>}

      <form onSubmit={handleCreate} className="mb-8 rounded-lg border border-gray-200 bg-white p-6 shadow-sm">
        <h2 className="mb-4 text-base font-semibold text-gray-900">Create Group</h2>
        <div className="flex flex-col gap-4 sm:flex-row sm:items-end">
          <div className="flex-1">
            <label className="mb-1 block text-sm font-medium text-gray-700">Name</label>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              placeholder="support-engineers"
              className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-gray-400 focus:outline-none"
            />
          </div>
          <div className="flex-1">
            <label className="mb-1 block text-sm font-medium text-gray-700">Description</label>
            <input
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Optional"
              className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-gray-400 focus:outline-none"
            />
          </div>
          <button
            type="submit"
            disabled={creating}
            className="rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
          >
            {creating ? 'Creating...' : 'Create Group'}
          </button>
        </div>
      </form>

      {loading ? (
        <p className="text-sm text-gray-500">Loading...</p>
      ) : groups.length === 0 ? (
        <p className="text-sm text-gray-500">No groups yet.</p>
      ) : (
        <div className="space-y-4">
          {groups.map((group) => (
            <div key={group.id} className="rounded-lg border border-gray-200 bg-white shadow-sm">
              <div className="flex items-start justify-between border-b border-gray-200 px-4 py-3">
                <div>
                  <h2 className="text-base font-semibold text-gray-900">{group.name}</h2>
                  {group.description && (
                    <p className="text-sm text-gray-500">{group.description}</p>
                  )}
                </div>
                <button
                  onClick={() => {
                    if (confirm(`Delete group "${group.name}"?`)) {
                      run(() => api.deleteUserGroup(group.id))
                    }
                  }}
                  className="text-red-500 hover:text-red-700"
                  title="Delete group"
                >
                  <Trash2 size={16} />
                </button>
              </div>

              <div className="grid gap-6 p-4 md:grid-cols-2">
                <div>
                  <h3 className="mb-2 text-sm font-medium text-gray-700">Members</h3>
                  <div className="mb-3 flex flex-wrap gap-2">
                    {(group.members ?? []).length === 0 ? (
                      <span className="text-sm text-gray-500">No members yet.</span>
                    ) : (
                      (group.members ?? []).map((m) => (
                        <span
                          key={m.user_id}
                          className="inline-flex items-center gap-1 rounded-full bg-gray-100 px-3 py-1 text-xs text-gray-700"
                        >
                          {m.email || m.user_id}
                          <button
                            onClick={() => run(() => api.removeUserGroupMember(group.id, m.user_id))}
                            className="text-gray-500 hover:text-red-600"
                            title="Remove member"
                          >
                            <X size={12} />
                          </button>
                        </span>
                      ))
                    )}
                  </div>
                  <select
                    value=""
                    onChange={(e) => {
                      const userID = e.target.value
                      if (userID) run(() => api.addUserGroupMember(group.id, userID))
                    }}
                    className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-gray-400 focus:outline-none"
                  >
                    <option value="">Add member…</option>
                    {members
                      .filter((m) => !(group.members ?? []).some((gm) => gm.user_id === m.user_id))
                      .map((m) => (
                        <option key={m.user_id} value={m.user_id}>{m.email}</option>
                      ))}
                  </select>
                </div>

                <div>
                  <h3 className="mb-2 text-sm font-medium text-gray-700">Integrations</h3>
                  <div className="mb-3 flex flex-wrap gap-2">
                    {(group.integration_grants ?? []).length === 0 ? (
                      <span className="text-sm text-gray-500">No integrations granted.</span>
                    ) : (
                      (group.integration_grants ?? []).map((g) => (
                        <span
                          key={g.credential_profile_id}
                          className="inline-flex items-center gap-1 rounded-full bg-gray-100 px-3 py-1 text-xs text-gray-700"
                        >
                          {integrationLabel(g.credential_profile_id)}
                          <button
                            onClick={() =>
                              run(() => api.revokeIntegrationFromGroup(group.id, g.credential_profile_id))
                            }
                            className="text-gray-500 hover:text-red-600"
                            title="Revoke integration"
                          >
                            <X size={12} />
                          </button>
                        </span>
                      ))
                    )}
                  </div>
                  <select
                    value=""
                    onChange={(e) => {
                      const profileID = e.target.value
                      if (profileID) run(() => api.grantIntegrationToGroup(group.id, profileID))
                    }}
                    className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-gray-400 focus:outline-none"
                  >
                    <option value="">Grant integration…</option>
                    {integrations
                      .filter(
                        (i) =>
                          !(group.integration_grants ?? []).some(
                            (g) => g.credential_profile_id === i.id,
                          ),
                      )
                      .map((i) => (
                        <option key={i.id} value={i.id}>{i.provider} · {i.alias}</option>
                      ))}
                  </select>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
