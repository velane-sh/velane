import { useEffect } from 'react'
import { api } from '../lib/api'

const HEX_COLOR = /^#?([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/

function applyAccent(value: string | undefined) {
  if (!value || !HEX_COLOR.test(value) || /^#?0{3}(?:0{3})?$/.test(value)) return
  document.documentElement.style.setProperty('--color-accent', value.startsWith('#') ? value : `#${value}`)
}

export function useBrandingTheme() {
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const accent = params.get('accent')
    const theme = params.get('theme')

    if (accent !== null) {
      applyAccent(accent)
    } else {
      api.getBranding().then(branding => {
        applyAccent(branding.accent_color)
      }).catch(() => {
        // Branding is unavailable in some modes and roles.
      })
    }

    if (theme === 'dark') {
      document.documentElement.classList.add('dark')
    } else if (theme === 'light') {
      document.documentElement.classList.remove('dark')
    }
  }, [])
}
