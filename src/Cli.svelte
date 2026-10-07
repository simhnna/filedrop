<script lang="ts">
  import { RELEASES_URL, releaseAsset } from './lib/links'
  import Header from './lib/Header.svelte'
  import Footer from './lib/Footer.svelte'

  let { navigate, theme }: {
    navigate: (to: string) => void
    theme: 'dark' | 'light'
  } = $props()

  let cliOs = $state<'unix' | 'windows'>(navigator.userAgent.includes('Windows') ? 'windows' : 'unix')
  const installCmd = `curl -fsSL ${location.origin}/install.sh | sh`
  let copied = $state(false)

  async function copyInstall() {
    await navigator.clipboard.writeText(installCmd)
    copied = true
    setTimeout(() => (copied = false), 1500)
  }
</script>

<div class="pm t-{theme} c-wrap">
  <div class="c-stack">

    <Header {navigate} current="/cli" />

    <div class="c-body">
    <div class="c-hero">
      <h1>Command-line <span class="c-em">tool</span></h1>
      <p>Send and receive from a terminal. It speaks the same protocol as the web app, so either side can use either.</p>
    </div>

    <div class="panel c-panel">
      <div class="row between" style="gap: 12px; flex-wrap: wrap">
        <div class="label">Install</div>
        <div class="c-tabs" role="tablist">
          <button role="tab" aria-selected={cliOs === 'unix'} class:on={cliOs === 'unix'} onclick={() => (cliOs = 'unix')}>macOS / Linux</button>
          <button role="tab" aria-selected={cliOs === 'windows'} class:on={cliOs === 'windows'} onclick={() => (cliOs = 'windows')}>Windows</button>
        </div>
      </div>

      {#if cliOs === 'unix'}
        <div class="c-cmd">
          <code>{installCmd}</code>
          <button class="btn btn-ghost btn-sm" style="flex: none" onclick={copyInstall}>{copied ? 'Copied' : 'Copy'}</button>
        </div>
        <div class="muted c-note">Installs to <code>~/.local/bin</code> and verifies the checksum.</div>
      {:else}
        <div class="row" style="gap: 10px; flex-wrap: wrap">
          <a class="btn btn-ghost btn-sm" href={releaseAsset('filedrop-windows-amd64.exe')}>Download for x64</a>
          <a class="btn btn-ghost btn-sm" href={releaseAsset('filedrop-windows-arm64.exe')}>Download for ARM64</a>
        </div>
        <div class="muted c-note">Rename to <code>filedrop.exe</code> and put it in a folder on your PATH.</div>
      {/if}

      <a class="c-releases" href={RELEASES_URL} target="_blank" rel="noopener">All releases on GitHub →</a>
    </div>

    <div class="panel c-panel">
      <div class="label">Usage</div>
      <div class="c-usage">
        <div><span class="faint">$</span> filedrop send report.pdf photos/*</div>
        <div><span class="faint">$</span> filedrop recv word-word-word-word</div>
        <div><span class="faint">$</span> filedrop recv</div>
      </div>
      <div class="muted c-note">
        <code>send</code> creates a session and prints the link and a QR code. <code>recv</code> joins a
        session from a code or link — or, without one, creates a session with a QR code to scan from your phone —
        and saves the files to the current folder (or <code>-o &lt;dir&gt;</code>).
      </div>
    </div>

    </div>

    <Footer />
  </div>
</div>

<style>
  .c-wrap { min-height: 100dvh; padding: 40px 64px 32px; display: flex; flex-direction: column; }
  .c-stack { flex: 1; width: 100%; max-width: 1080px; margin: 0 auto; display: flex; flex-direction: column; }
  .c-body { max-width: 720px; display: flex; flex-direction: column; gap: 28px; padding: 28px 0 56px; }
  .pm .btn { text-decoration: none; }

  .c-hero h1 {
    font-family: var(--display);
    font-weight: 800;
    letter-spacing: -.035em;
    font-size: clamp(44px, 7vw, 72px);
    line-height: .95;
    margin-top: 28px;
  }
  /* The left padding is a shadow so it doesn't push into the preceding word when both share a line */
  .c-em { background: var(--hl); color: #111110; padding-right: .1em; box-shadow: -.1em 0 0 var(--hl); box-decoration-break: clone; -webkit-box-decoration-break: clone; }
  .c-hero p { font-size: 19px; line-height: 1.5; color: var(--muted); margin-top: 20px; max-width: 520px; text-wrap: pretty; }

  .c-panel { display: flex; flex-direction: column; gap: 16px; }
  .c-tabs { display: inline-flex; border: 2px solid var(--line); border-radius: 10px; padding: 3px; gap: 3px; }
  .c-tabs button {
    border: 0; background: none; color: var(--muted); font: inherit; font-size: 14px; font-weight: 500;
    padding: 6px 12px; border-radius: 6px; cursor: pointer;
  }
  .c-tabs button.on { background: var(--ink); color: var(--bg); }

  .c-cmd {
    display: flex; align-items: center; gap: 10px;
    border: 2px solid var(--line); border-radius: 12px; padding: 8px 8px 8px 14px;
  }
  .c-cmd code { flex: 1; min-width: 0; overflow-x: auto; white-space: nowrap; font-size: 14px; }
  .c-wrap code { font-family: var(--mono); }
  .c-note { font-size: 14px; line-height: 1.5; }
  .c-usage { font-family: var(--mono); font-size: 14px; line-height: 1.9; overflow-x: auto; white-space: nowrap; }
  .c-releases { color: var(--ink); font-weight: 500; font-size: 15px; text-decoration: none; align-self: flex-start; }
  .c-releases:hover:hover { text-decoration: underline; text-underline-offset: 4px; }

  @media (max-width: 600px) { .c-wrap { padding: 24px 20px; } }
</style>
