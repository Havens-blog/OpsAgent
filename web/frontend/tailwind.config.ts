import type { Config } from 'tailwindcss'
import tailwindcssAnimate from 'tailwindcss-animate'

const config: Config = {
  darkMode: ['class'],
  content: [
    './src/pages/**/*.{js,ts,jsx,tsx,mdx}',
    './src/components/**/*.{js,ts,jsx,tsx,mdx}',
    './src/app/**/*.{js,ts,jsx,tsx,mdx}',
  ],
  theme: {
    container: {
      center: true,
      padding: '2rem',
      screens: {
        '2xl': '1400px',
      },
    },
    extend: {
      colors: {
        border: 'hsl(var(--border))',
        input: 'hsl(var(--input))',
        ring: 'hsl(var(--ring))',
        background: 'hsl(var(--background))',
        foreground: 'hsl(var(--foreground))',
        primary: {
          DEFAULT: 'hsl(var(--primary))',
          foreground: 'hsl(var(--primary-foreground))',
        },
        secondary: {
          DEFAULT: 'hsl(var(--secondary))',
          foreground: 'hsl(var(--secondary-foreground))',
        },
        destructive: {
          DEFAULT: 'hsl(var(--destructive))',
          foreground: 'hsl(var(--destructive-foreground))',
        },
        muted: {
          DEFAULT: 'hsl(var(--muted))',
          foreground: 'hsl(var(--muted-foreground))',
        },
        accent: {
          DEFAULT: 'hsl(var(--accent))',
          foreground: 'hsl(var(--accent-foreground))',
        },
        popover: {
          DEFAULT: 'hsl(var(--popover))',
          foreground: 'hsl(var(--popover-foreground))',
        },
        card: {
          DEFAULT: 'hsl(var(--card))',
          foreground: 'hsl(var(--card-foreground))',
        },
        // Linear multi-tier tokens aligned to haven-console (theme-variables.scss).
        surface: 'hsl(var(--bg-surface))',
        elevated: 'hsl(var(--bg-elevated))',
        'bg-hover': 'hsl(var(--bg-hover))',
        'text-regular': 'hsl(var(--text-regular))',
        'text-secondary': 'hsl(var(--text-secondary))',
        'text-tertiary': 'hsl(var(--text-tertiary))',
        'accent-blue': 'hsl(var(--accent-blue))',
        'accent-purple': 'hsl(var(--accent-purple))',
        // Design variables (globals.css): page/surface tiers + primary states.
        page: 'hsl(var(--bg-page))',
        subtle: 'hsl(var(--bg-subtle))',
        tertiary: 'hsl(var(--text-tertiary))',
        'primary-soft': 'hsl(var(--primary-soft))',
        'primary-border': 'hsl(var(--primary-border))',
        'primary-hover': 'hsl(var(--primary-hover))',
        // Status colors: state only, never type decoration.
        warning: {
          DEFAULT: 'hsl(var(--warning))',
          soft: 'hsl(var(--warning-soft))',
          border: 'hsl(var(--warning-border))',
        },
        success: {
          DEFAULT: 'hsl(var(--success))',
          soft: 'hsl(var(--success-soft))',
        },
        danger: {
          DEFAULT: 'hsl(var(--danger))',
          soft: 'hsl(var(--danger-soft))',
        },
        // Category colors: node type only, never state.
        cat: {
          application: 'hsl(var(--cat-application))',
          database: 'hsl(var(--cat-database))',
          cache: 'hsl(var(--cat-cache))',
          queue: 'hsl(var(--cat-queue))',
        },
        // Severity colors for incidents
        severity: {
          p0: 'hsl(0, 84%, 60%)',    // Red
          p1: 'hsl(25, 95%, 53%)',   // Orange
          p2: 'hsl(43, 96%, 56%)',   // Yellow
          p3: 'hsl(142, 71%, 45%)',  // Green
          p4: 'hsl(210, 40%, 40%)',  // Blue
        },
        // Status colors
        status: {
          healthy: 'hsl(142, 71%, 45%)',
          degraded: 'hsl(43, 96%, 56%)',
          unhealthy: 'hsl(0, 84%, 60%)',
          unknown: 'hsl(210, 40%, 40%)',
        },
      },
      borderRadius: {
        lg: 'var(--radius)',
        md: 'calc(var(--radius) - 2px)',
        sm: 'calc(var(--radius) - 4px)',
      },
      keyframes: {
        'accordion-down': {
          from: { height: '0' },
          to: { height: 'var(--radix-accordion-content-height)' },
        },
        'accordion-up': {
          from: { height: 'var(--radix-accordion-content-height)' },
          to: { height: '0' },
        },
        'progress': {
          '0%': { transform: 'translateX(-100%)' },
          '100%': { transform: 'translateX(100%)' },
        },
      },
      animation: {
        'accordion-down': 'accordion-down 0.2s ease-out',
        'accordion-up': 'accordion-up 0.2s ease-out',
        'progress': 'progress 2s linear infinite',
      },
    },
  },
  plugins: [tailwindcssAnimate],
}

export default config
