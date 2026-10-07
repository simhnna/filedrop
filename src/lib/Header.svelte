<script lang="ts">
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
    <a href="/about" onclick={go('/about')} aria-current={current === '/about' ? 'page' : undefined}>How it works</a>
    <a href="/cli" onclick={go('/cli')} aria-current={current === '/cli' ? 'page' : undefined}>
      <span class="long">Command line</span><span class="short">CLI</span>
    </a>
  </nav>
</header>

<style>
  .hdr { display: flex; justify-content: space-between; align-items: center; gap: 16px; }
  nav { display: flex; gap: 28px; }
  nav a { color: var(--ink); font-weight: 500; font-size: 15px; text-decoration: none; white-space: nowrap; }
  .short { display: none; }
  nav a:hover, nav a[aria-current] { text-decoration: underline; text-underline-offset: 6px; text-decoration-thickness: 2px; }
  @media (max-width: 600px) { nav { gap: 16px; } nav a { font-size: 14px; } }
  @media (max-width: 400px) { .long { display: none; } .short { display: inline; } }
</style>
