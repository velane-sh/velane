import { useEffect, useState, type FormEvent } from 'react'
import { UserMinus } from 'lucide-react'
import { api } from '../lib/api'
import type { TenantMember, InviteToken } from '../types'
import CopyBox from '../components/CopyBox'

const ROLES = ['owner', 'admin', 'integration_manager', 'manage', 'member', 'invoke', 'viewer']

export default function TeamPage() {
  const [members, setMembers] = useState<TenantMember[]>([])
  const [invites, setInvites] = useState<InviteToken[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  // Invite form
  const [inviteEmail, setInviteEmail] = useState('')
  const [inviteRole, setInviteRole] = useState('manage')
  const [inviting, setInviting] = useState(false)
  const [newInviteToken, setNewInviteToken] = useState<string | null>(null)

  const load = async () => {
    try {
      const [m, i] = await Promise.all([api.listMembers(), api.listInvites()])
      setMembers(m)
      setInvites(i)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load team')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  const handleInvite = async (e: FormEvent) => {
    e.preventDefault()
    setInviting(true)
    setNewInviteToken(null)
    try {
      const res = await api.inviteMember(inviteEmail, inviteRole)
      setNewInviteToken(res.invite_token)
      setInviteEmail('')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to invite member')
    } finally {
      setInviting(false)
    }
  }

  const handleRemove = async (userID: string) => {
    if (!confirm('Remove this member?')) return
    try {
      await api.removeMember(userID)
      setMembers((m) => m.filter((mb) => mb.user_id !== userID))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to remove member')
    }
  }

  const registerLink = newInviteToken
    ? `${window.location.origin}/register?invite=${newInviteToken}`
    : null

  return (
    <div>
      <h1 className="mb-6 text-2xl font-bold text-gray-900">Team</h1>

      {error && (
        <div className="mb-4 rounded-md bg-red-50 p-3 text-sm text-red-700">{error}</div>
      )}

      {registerLink && (
        <div className="mb-6 rounded-md border border-green-200 bg-green-50 p-4">
          <p className="mb-2 text-sm font-medium text-green-800">
            Invite created! Share this registration link with the invitee:
          </p>
          <CopyBox value={registerLink} />
        </div>
      )}

      {/* Invite form */}
      <form
        onSubmit={handleInvite}
        className="mb-8 rounded-lg border border-gray-200 bg-white p-6 shadow-sm"
      >
        <h2 className="mb-4 text-base font-semibold text-gray-900">Invite Team Member</h2>
        <div className="flex flex-col gap-4 sm:flex-row sm:items-end">
          <div className="flex-1">
            <label className="mb-1 block text-sm font-medium text-gray-700">Email</label>
            <input
              type="email"
              value={inviteEmail}
              onChange={(e) => setInviteEmail(e.target.value)}
              required
              className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-gray-400 focus:outline-none"
              placeholder="colleague@example.com"
            />
          </div>
          <div>
            <label className="mb-1 block text-sm font-medium text-gray-700">Role</label>
            <select
              value={inviteRole}
              onChange={(e) => setInviteRole(e.target.value)}
              className="rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-gray-400 focus:outline-none"
            >
              {ROLES.map((r) => (
                <option key={r} value={r}>{r}</option>
              ))}
            </select>
          </div>
          <button
            type="submit"
            disabled={inviting}
            className="rounded-md bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
          >
            {inviting ? 'Inviting...' : 'Send Invite'}
          </button>
        </div>
      </form>

      {/* Members table */}
      <div className="mb-8 rounded-lg border border-gray-200 bg-white shadow-sm">
        <h2 className="border-b border-gray-200 px-4 py-3 text-base font-semibold text-gray-900">
          Members
        </h2>
        {loading ? (
          <p className="p-4 text-sm text-gray-500">Loading...</p>
        ) : members.length === 0 ? (
          <p className="p-4 text-sm text-gray-500">No members yet.</p>
        ) : (
          <table className="w-full text-sm">
            <thead className="border-b border-gray-200 bg-gray-50 text-left">
              <tr>
                <th className="px-4 py-3 font-medium text-gray-600">Email</th>
                <th className="px-4 py-3 font-medium text-gray-600">Role</th>
                <th className="px-4 py-3 font-medium text-gray-600">Invited</th>
                <th className="px-4 py-3" />
              </tr>
            </thead>
            <tbody>
              {members.map((m) => (
                <tr key={m.user_id} className="border-b border-gray-100 last:border-0">
                  <td className="px-4 py-3 text-gray-900">{m.email}</td>
                  <td className="px-4 py-3 text-gray-600">{m.role}</td>
                  <td className="px-4 py-3 text-gray-500">
                    {new Date(m.invited_at).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-3">
                    <button
                      onClick={() => handleRemove(m.user_id)}
                      className="text-red-500 hover:text-red-700"
                      title="Remove member"
                    >
                      <UserMinus size={16} />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Pending invites */}
      {invites.length > 0 && (
        <div className="rounded-lg border border-gray-200 bg-white shadow-sm">
          <h2 className="border-b border-gray-200 px-4 py-3 text-base font-semibold text-gray-900">
            Pending Invites
          </h2>
          <table className="w-full text-sm">
            <thead className="border-b border-gray-200 bg-gray-50 text-left">
              <tr>
                <th className="px-4 py-3 font-medium text-gray-600">Email</th>
                <th className="px-4 py-3 font-medium text-gray-600">Role</th>
                <th className="px-4 py-3 font-medium text-gray-600">Expires</th>
              </tr>
            </thead>
            <tbody>
              {invites.map((inv) => (
                <tr key={inv.id} className="border-b border-gray-100 last:border-0">
                  <td className="px-4 py-3 text-gray-900">{inv.email}</td>
                  <td className="px-4 py-3 text-gray-600">{inv.role}</td>
                  <td className="px-4 py-3 text-gray-500">
                    {new Date(inv.expires_at).toLocaleString()}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
