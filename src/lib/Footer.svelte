<footer class="ftr">
  <div class="trust">
    {#each ['No tracking', 'No analytics', 'No ads', 'No cookies', 'No account'] as item}
      <span>✕ {item}</span>
    {/each}
  </div>
  <div class="main">
    <b>Your files never touch a server.</b>
    <span class="muted"> They travel directly between devices over an encrypted WebRTC channel — only the handshake passes through.</span>
  </div>
  <div class="faint note">
    FileDrop is hosted on Cloudflare, which also helps devices find each other. Like any
    server you connect to, it sees your IP address — never your files or the session link.
  </div>
</footer>

<style>
  .ftr {
    margin-top: auto;
    padding: 20px 0 0;
    border-top: 2px solid var(--line);
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .trust { display: flex; flex-wrap: wrap; gap: 8px 32px; font-size: 14px; font-weight: 500; }
  .main { font-size: 15px; line-height: 1.55; max-width: 620px; }
  .note { font-family: var(--mono); font-size: 12.5px; line-height: 1.55; max-width: 620px; text-wrap: pretty; }
</style>
