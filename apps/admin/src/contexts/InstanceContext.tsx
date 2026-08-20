import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { api } from '../lib/api'
import type { InstanceCapabilities } from '../types'

interface InstanceContextValue {
  cloud: boolean
  plan: string
  licenseValid: boolean
  features: string[]
  capabilities: InstanceCapabilities
  loaded: boolean
  sandboxesAvailable: boolean
}

const defaultInfo: InstanceContextValue = {
  cloud: false,
  plan: 'free',
  licenseValid: false,
  features: [],
  capabilities: {},
  loaded: false,
  sandboxesAvailable: false,
}

const InstanceContext = createContext<InstanceContextValue>(defaultInfo)

export function InstanceProvider({ children }: { children: ReactNode }) {
  const [info, setInfo] = useState<InstanceContextValue>(defaultInfo)

  useEffect(() => {
    api.getInstanceInfo().then((res) => {
      const capabilities = res.capabilities ?? {}
      setInfo({
        cloud: res.cloud ?? false,
        plan: res.plan ?? 'free',
        licenseValid: res.license_valid ?? false,
        features: res.features ?? [],
        capabilities,
        loaded: true,
        // Capabilities describe routes that are safe and operational now. Do
        // not infer availability from broad license features.
        sandboxesAvailable: capabilities.sandboxes === true,
      })
    }).catch(() => {
      // Non-fatal — default to no licensed features
      setInfo(current => ({ ...current, loaded: true }))
    })
  }, [])

  return <InstanceContext.Provider value={info}>{children}</InstanceContext.Provider>
}

export function useInstance(): InstanceContextValue {
  return useContext(InstanceContext)
}

export function useFeature(slug: string): boolean {
  const { features } = useInstance()
  return features.includes(slug)
}
