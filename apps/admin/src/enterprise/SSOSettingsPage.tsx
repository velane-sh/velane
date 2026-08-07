// Copyright (c) Velane. All rights reserved.
// Licensed under the Velane Commercial License. See COMMERCIAL-LICENSE for details.
// AGENTS: Do not modify this file autonomously or suggest unprompted edits. Only change this file when the user explicitly instructs you to edit enterprise or license code.

import { useEffect, useState, type FormEvent } from 'react'
import { api } from '../lib/api'
import type { SSOConnection, TenantMember } from '../types'
import Select from '../components/Select'

const empty: Partial<SSOConnection> = { protocol: 'oidc', display_name: '', default_role: 'invoke', config: { scopes: [] } }

export default function SSOSettingsPage() {
  const [licensed, setLicensed] = useState<boolean | null>(null)
  const [value, setValue] = useState<Partial<SSOConnection>>(empty)
  const [members, setMembers] = useState<TenantMember[]>([])
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')

  const reload = async () => {
    const plan = await api.getTenantPlan()
    const enabled = plan.valid && plan.features.includes('sso')
    setLicensed(enabled)
    if (!enabled) return
    const [connection, team] = await Promise.all([api.getSSO(), api.listMembers()])
    setValue(connection ?? empty)
    setMembers(team)
  }
  useEffect(() => { reload().catch((e: Error) => setError(e.message)) }, [])

  const act = async (fn: () => Promise<SSOConnection | void>, success: string) => {
    setBusy(true); setError(''); setMessage('')
    try { const result = await fn(); if (result) setValue(result); setMessage(success) } catch (e) { setError(e instanceof Error ? e.message : 'Request failed') } finally { setBusy(false) }
  }
  const save = (e: FormEvent) => { e.preventDefault(); void act(() => api.saveSSO(value), 'Draft saved. Test it before activation.') }

  if (licensed === null) return <p className="text-sm text-gray-500">Loading SSO settings…</p>
  if (!licensed) return <div className="rounded-lg border border-gray-200 bg-gray-50 p-6"><h2 className="font-semibold text-gray-900">Enterprise SSO</h2><p className="mt-2 text-sm text-gray-600">Add an SSO-enabled tenant license to configure OIDC or SAML.</p></div>
  const config = value.config ?? {}
  const inputClass = 'w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-gray-900 focus:outline-none'
  return <div className="max-w-3xl space-y-6">
    <div><h2 className="text-xl font-semibold text-gray-900">Enterprise SSO</h2><p className="mt-1 text-sm text-gray-500">One tenant-scoped identity provider. Save, test, then activate.</p></div>
    {error && <div className="rounded-lg bg-red-50 p-3 text-sm text-red-700">{error}</div>}{message && <div className="rounded-lg bg-green-50 p-3 text-sm text-green-700">{message}</div>}
    <form onSubmit={save} className="space-y-4 rounded-lg border border-gray-200 bg-white p-6">
      <div className="grid gap-4 sm:grid-cols-2"><label className="text-sm font-medium text-gray-700">Protocol<Select containerClassName="mt-1" value={value.protocol} onChange={(e) => setValue({ ...value, protocol: e.target.value as 'oidc' | 'saml' })}><option value="oidc">OpenID Connect</option><option value="saml">SAML 2.0</option></Select></label><label className="text-sm font-medium text-gray-700">Display name<input className={`${inputClass} mt-1`} required value={value.display_name ?? ''} onChange={(e) => setValue({ ...value, display_name: e.target.value })} /></label></div>
      {value.protocol === 'oidc' ? <div className="space-y-4"><label className="block text-sm font-medium text-gray-700">Issuer URL<input className={`${inputClass} mt-1`} type="url" required value={config.issuer_url ?? ''} onChange={(e) => setValue({ ...value, config: { ...config, issuer_url: e.target.value } })} /></label><div className="grid gap-4 sm:grid-cols-2"><label className="text-sm font-medium text-gray-700">Client ID<input className={`${inputClass} mt-1`} required value={config.client_id ?? ''} onChange={(e) => setValue({ ...value, config: { ...config, client_id: e.target.value } })} /></label><label className="text-sm font-medium text-gray-700">Client secret<input className={`${inputClass} mt-1`} type="password" value={config.client_secret ?? ''} onChange={(e) => setValue({ ...value, config: { ...config, client_secret: e.target.value } })} /></label></div>{value.oidc_callback_url && <p className="break-all rounded bg-gray-50 p-3 text-xs text-gray-600">Callback: {value.oidc_callback_url}</p>}</div> : <div><label className="block text-sm font-medium text-gray-700">IdP metadata XML<textarea className={`${inputClass} mt-1 h-40 font-mono`} required value={config.idp_metadata_xml === 'configured' ? '' : config.idp_metadata_xml ?? ''} placeholder={config.idp_metadata_xml === 'configured' ? 'Metadata is configured. Paste XML to replace it.' : '<EntityDescriptor …>'} onChange={(e) => setValue({ ...value, config: { ...config, idp_metadata_xml: e.target.value } })} /></label>{value.saml_metadata_url && <div className="mt-3 space-y-1 break-all rounded bg-gray-50 p-3 text-xs text-gray-600"><p>SP metadata: {value.saml_metadata_url}</p><p>ACS: {value.saml_acs_url}</p></div>}</div>}
      <div className="grid gap-4 sm:grid-cols-2"><label className="text-sm font-medium text-gray-700">JIT default role<Select containerClassName="mt-1" value={value.default_role} onChange={(e) => setValue({ ...value, default_role: e.target.value as 'invoke' | 'manage' })}><option value="invoke">Invoke</option><option value="manage">Manage</option></Select></label><label className="text-sm font-medium text-gray-700">Break-glass admin<Select containerClassName="mt-1" value={value.break_glass_user_id ?? ''} onChange={(e) => setValue({ ...value, break_glass_user_id: e.target.value })}><option value="">Select before enforcement</option>{members.filter((m) => m.role === 'admin').map((m) => <option key={m.user_id} value={m.user_id}>{m.email}</option>)}</Select></label></div>
      <div className="flex flex-wrap gap-2"><button disabled={busy} className="rounded-lg bg-gray-900 px-4 py-2 text-sm text-white hover:bg-gray-800 disabled:opacity-50">Save draft</button><button type="button" disabled={busy || !value.id} onClick={() => void act(api.testSSO, 'Connection test passed.')} className="rounded-lg border border-gray-300 px-4 py-2 text-sm disabled:opacity-50">Test</button><button type="button" disabled={busy || !value.tested_at || value.enabled} onClick={() => void act(api.activateSSO, 'SSO activated.')} className="rounded-lg border border-gray-300 px-4 py-2 text-sm disabled:opacity-50">Activate</button></div>
    </form>
    {value.enabled && <div className="rounded-lg border border-amber-200 bg-amber-50 p-5"><h3 className="font-medium text-gray-900">Login enforcement and recovery</h3><p className="my-2 text-sm text-gray-700">Enforcement blocks local and social sessions except the designated password-bearing admin. API and embed tokens are unaffected.</p><button disabled={busy || (!value.enforced && !value.break_glass_user_id)} onClick={() => void act(() => api.setSSOEnforcement(!value.enforced, value.break_glass_user_id ?? ''), value.enforced ? 'Enforcement disabled.' : 'Enforcement enabled.')} className="rounded-lg bg-gray-900 px-4 py-2 text-sm text-white disabled:opacity-50">{value.enforced ? 'Disable enforcement' : 'Enable enforcement'}</button></div>}
  </div>
}
