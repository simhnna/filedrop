<script lang="ts">
  import Landing from './Landing.svelte'
  import About from './About.svelte'
  import Cli from './Cli.svelte'
  import Room from './Room.svelte'
  import { normalizeRoomCode } from './lib/roomName'

  // Rooms live at /r/#<roomId>. The code is in the fragment so it never reaches
  // a server: fragments aren't sent in requests, server logs or Referer headers.
  let path = $state(window.location.pathname)
  let hash = $state(window.location.hash)

  const mq = window.matchMedia('(prefers-color-scheme: dark)')
  let theme = $state<'dark' | 'light'>(mq.matches ? 'dark' : 'light')

  $effect(() => {
    const onChange = (e: MediaQueryListEvent) => { theme = e.matches ? 'dark' : 'light' }
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  })

  function navigate(to: string) {
    history.pushState(null, '', to)
    path = window.location.pathname
    hash = window.location.hash
    window.scrollTo(0, 0)
  }

  $effect(() => {
    // popstate covers back/forward; hashchange covers editing the address bar
    const onChange = () => { path = window.location.pathname; hash = window.location.hash }
    window.addEventListener('popstate', onChange)
    window.addEventListener('hashchange', onChange)
    return () => {
      window.removeEventListener('popstate', onChange)
      window.removeEventListener('hashchange', onChange)
    }
  })

  const rawCode = $derived(path === '/r/' ? decodeURIComponent(hash.replace(/^#/, '')) : '')
  const parsed = $derived(rawCode ? normalizeRoomCode(rawCode) : null)
  const roomId = $derived(parsed && 'roomId' in parsed ? parsed.roomId : null)

  // Canonicalise e.g. /r/#Acid%20Acorn… so the Room sees one ID per room.
  $effect(() => {
    if (roomId && roomId !== hash.slice(1)) {
      history.replaceState(null, '', `/r/#${roomId}`)
      hash = window.location.hash
    }
  })
</script>

{#if roomId}
  {#key roomId}
    <Room {roomId} {navigate} {theme} />
  {/key}
{:else if path === '/about'}
  <About {navigate} {theme} />
{:else if path === '/cli'}
  <Cli {navigate} {theme} />
{:else if rawCode}
  {#key rawCode}
    <Landing {navigate} {theme} invalidCode={rawCode} />
  {/key}
{:else}
  <Landing {navigate} {theme} />
{/if}
