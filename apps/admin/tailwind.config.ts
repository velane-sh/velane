import type { Config } from 'tailwindcss'

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        bg: 'var(--color-bg)',
        surface: {
          DEFAULT: 'var(--color-surface)',
          muted: 'var(--color-surface-muted)',
        },
        line: {
          DEFAULT: 'var(--color-border)',
          strong: 'var(--color-border-strong)',
        },
        content: {
          DEFAULT: 'var(--color-text)',
          muted: 'var(--color-text-muted)',
          subtle: 'var(--color-text-subtle)',
        },
        accent: {
          DEFAULT: 'var(--color-accent)',
          hover: 'var(--color-accent-hover)',
          fg: 'var(--color-accent-fg)',
          subtle: 'var(--color-accent-subtle)',
        },
        danger: {
          DEFAULT: 'var(--color-danger)',
          subtle: 'var(--color-danger-subtle)',
          text: 'var(--color-danger-text)',
        },
        success: {
          DEFAULT: 'var(--color-success)',
          subtle: 'var(--color-success-subtle)',
          text: 'var(--color-success-text)',
        },
      },
      borderRadius: {
        md: 'var(--radius-md)',
        lg: 'var(--radius-lg)',
        xl: 'var(--radius-xl)',
        '2xl': 'var(--radius-2xl)',
      },
      boxShadow: {
        sm: 'var(--shadow-sm)',
        card: 'var(--shadow-card)',
        overlay: 'var(--shadow-overlay)',
      },
      fontFamily: {
        sans: ['Geist', 'ui-sans-serif', 'system-ui', '-apple-system', 'Segoe UI', 'sans-serif'],
        mono: ['"Geist Mono"', 'ui-monospace', 'SFMono-Regular', 'Menlo', 'monospace'],
      },
    },
  },
  darkMode: 'class',
  plugins: [],
} satisfies Config
