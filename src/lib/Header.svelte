<script lang="ts">
  import { REPO_URL } from './links'

  let { navigate, current = '' }: {
    navigate: (to: string) => void
    /** Path of the page being shown, to mark its nav link */
    current?: '/about' | '/cli' | ''
  } = $props()

  function go(to: string) {
    return (e: MouseEvent) => {
      e.preventDefault()
      navigate(to)
    }
  }
</script>

<header class="hdr">
  <a href="/" onclick={go('/')} class="brand">
    <span class="mark" aria-hidden="true">
      <svg viewBox="2 2 23 23" fill="none" stroke-width="2.8" stroke-linecap="round" stroke-linejoin="round">
        <path d="M13.5 7v9M9 12.5l4.5 4.5 4.5-4.5M8.5 20.5h10" />
      </svg>
    </span>
    <span class="word">FileDrop</span>
  </a>
  <nav>
    <a href="/about" onclick={go('/about')} aria-current={current === '/about' ? 'page' : undefined}>FAQ</a>
    <a href="/cli" onclick={go('/cli')} aria-current={current === '/cli' ? 'page' : undefined}>
      CLI
    </a>
    <a href={REPO_URL} target="_blank" rel="noopener" class="gh" aria-label="Source code on GitHub" title="Source code on GitHub">
      <svg viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
        <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z" />
      </svg>
    </a>
  </nav>
</header>

<style>
  .hdr { display: flex; justify-content: space-between; align-items: center; gap: 16px; }
  nav { display: flex; align-items: center; gap: 28px; }
  .gh { display: flex; }
  .gh svg { width: 20px; height: 20px; }
  .gh:hover { opacity: 0.7; }
  nav a { color: var(--ink); font-weight: 500; font-size: 15px; text-decoration: none; white-space: nowrap; }
  nav a:hover, nav a[aria-current] { text-decoration: underline; text-underline-offset: 6px; text-decoration-thickness: 2px; }
  @media (max-width: 600px) { nav { gap: 16px; } nav a { font-size: 14px; } }
</style>
