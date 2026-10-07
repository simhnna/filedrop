<script lang="ts">
  import { RELEASES_URL } from './lib/links'
  import Header from './lib/Header.svelte'
  import Footer from './lib/Footer.svelte'

  let { navigate, theme }: {
    navigate: (to: string) => void
    theme: 'dark' | 'light'
  } = $props()

  function goHome(e: MouseEvent) {
    e.preventDefault()
    navigate('/')
  }
</script>

<div class="pm t-{theme} a-wrap">
  <div class="a-stack">

    <Header {navigate} current="/about" />

    <div class="a-body">

    <div class="a-left">
    <!-- about -->
    <div class="a-hero">
      <h1>About <span class="a-em">FileDrop</span></h1>
      <p>
        FileDrop sends files and text directly between devices. Open a session, share the link,
        and the two browsers connect to each other — nothing is uploaded, and there's no account to create.
      </p>
    </div>

    <div class="panel a-steps">
      {#each [
        ['Create a session', 'You get a link made of four random words.'],
        ['Share the link', 'Send it, or let the other person scan the QR code.'],
        ['Drop your files', 'They travel straight to the other device, encrypted.'],
      ] as [title, text], i}
        <div class="a-step">
          <span class="a-num">{i + 1}</span>
          <div>
            <div class="a-step-title">{title}</div>
            <div class="muted a-step-text">{text}</div>
          </div>
        </div>
      {/each}
    </div>

    <div class="a-foot">
      <a href="/" onclick={goHome} class="btn btn-primary">Start a session</a>
    </div>
    </div>

    <!-- FAQ -->
    <div class="a-right">
    <h2 class="a-h2">Frequently asked questions</h2>

    <div class="a-faq">
      <details>
        <summary>Do my files go through a server?</summary>
        <p>
          No. Files and text travel directly between the two devices over a WebRTC connection.
          Only the connection setup passes through a server: our signaling server introduces
          the two browsers to each other, and Cloudflare's public STUN servers help each one find
          its address on the internet. There is no relay server, so your data can't take a detour.
        </p>
      </details>

      <details>
        <summary>Is the transfer encrypted?</summary>
        <p>
          Yes. WebRTC always encrypts the connection. On top of that, FileDrop checks that the
          device on the other end really knows the session link before anything is exchanged.
          This uses a password-authenticated key exchange (PAKE), so not even the signaling
          server can secretly sit in the middle of your transfer.
        </p>
      </details>

      <details>
        <summary>What are the four words in the link?</summary>
        <p>
          The first two words name the session, so the two browsers can find each other.
          The last two words are a secret that never leaves your device. It is only used to
          prove that both sides have the same link. The words come after the <code>#</code>
          in the link, a part browsers never send to any server.
        </p>
        <p>
          Treat the link like a password: anyone who has it can join while the session is open.
        </p>
      </details>

      <details>
        <summary>What if someone tries to guess the link?</summary>
        <p>
          Each wrong guess has to be a real connection attempt, and FileDrop stops accepting new
          people after five failed attempts. With over a million possible secrets, a guess is
          very unlikely to succeed.
        </p>
      </details>

      <details>
        <summary>Do I need to keep the page open?</summary>
        <p>
          Yes. The person who created the session acts as the hub: everyone else connects to
          them. If they close the page, the session ends. Nobody stores your files in the
          meantime, so both sides need to be online at the same time.
        </p>
      </details>

      <details>
        <summary>Can more than two people join?</summary>
        <p>
          Yes. Everyone who opens the link connects to the person who created the session.
          Files the creator shares are offered to everyone, including people who join later.
          Files from anyone else go to the creator only.
        </p>
      </details>

      <details>
        <summary>Can both sides send?</summary>
        <p>Yes. Once connected, either side can send files and text.</p>
      </details>

      <details>
        <summary>What does "Ask before receiving" do?</summary>
        <p>
          When it's on (the default), you accept or reject each file before it starts
          downloading. Turn it off to receive everything automatically.
        </p>
      </details>

      <details>
        <summary>Is there a size limit?</summary>
        <p>
          Not a fixed one. Incoming files are written to your browser's private storage as they
          arrive, so the limit is the free space your browser is allowed to use on your device.
        </p>
      </details>

      <details>
        <summary>What happens if the connection drops?</summary>
        <p>
          Partly received files are kept. When the same file is offered again in the same
          session, the transfer continues where it stopped instead of starting over. Unfinished
          files are deleted from your browser after seven days.
        </p>
      </details>

      <details>
        <summary>Why won't my devices connect?</summary>
        <p>
          Some networks — many company networks, some mobile carriers and some public Wi-Fi —
          block direct connections between devices. Because FileDrop never relays your data
          through a server, it can't work around that. Try a different network, for example a
          phone hotspot.
        </p>
      </details>

      <details>
        <summary>Can I use it from the command line?</summary>
        <p>
          Yes. The <code>filedrop</code> command-line tool works with the web app, so you can
          send from a terminal to a browser and the other way round. It's handy for servers and
          other machines without a browser.
        </p>
        <p>
          On macOS or Linux, install it with
          <code class="a-cmd">curl -fsSL {location.origin}/install.sh | sh</code>
          The script puts it in <code>~/.local/bin</code> and verifies the checksum. On Windows,
          download the <code>.exe</code> for your processor from the
          <a href={RELEASES_URL} target="_blank" rel="noopener">releases page</a>, rename it to
          <code>filedrop.exe</code> and put it in a folder on your PATH. The releases page has
          every build if you'd rather install manually.
        </p>
        <p>
          <code>filedrop send report.pdf photos/*</code> creates a session and prints the link
          and a QR code. <code>filedrop recv &lt;code or link&gt;</code> joins a session and
          saves the files to the current folder (or <code>-o &lt;dir&gt;</code>).
        </p>
      </details>

      <details>
        <summary>What do you store about me?</summary>
        <p>
          There are no accounts, cookies, analytics or ads. What FileDrop keeps lives in your own
          browser: files you've received but not yet downloaded, so a transfer can resume (anything
          left over is removed after seven days), and a note that you created a session, which is
          cleared when you close the tab.
        </p>
      </details>

      <details>
        <summary>Which other companies are involved?</summary>
        <p>
          Only Cloudflare. FileDrop is hosted there, Cloudflare carries the connection setup
          between browsers, and its STUN servers help your device find its public address.
          Like any server you connect to, it sees your IP address. It never sees your files,
          your messages or the secret part of the session link.
        </p>
      </details>
    </div>

    </div>

    </div>

    <Footer />

  </div>
</div>

<style>
  .a-wrap {
    min-height: 100dvh;
    padding: 40px 64px 32px;
    display: flex;
    flex-direction: column;
  }

  .a-stack {
    flex: 1;
    width: 100%;
    max-width: 1080px;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
  }

  .a-body {
    display: grid;
    grid-template-columns: minmax(0, 5fr) minmax(0, 6fr);
    gap: 64px;
    align-items: start;
    padding: 28px 0 56px;
  }
  .a-left, .a-right { min-width: 0; }
  @media (max-width: 900px) { .a-body { grid-template-columns: minmax(0, 1fr); gap: 32px; } }
  .pm .btn { text-decoration: none; }

  .a-hero h1 {
    font-family: var(--display);
    font-weight: 800;
    letter-spacing: -.035em;
    font-size: clamp(44px, 6vw, 72px);
    line-height: .95;
    margin: 28px 0 0;
    color: var(--ink);
  }
  /* The left padding is a shadow so it doesn't push into the preceding word when both share a line */
  .a-em { background: var(--hl); color: #111110; padding-right: .1em; box-shadow: -.1em 0 0 var(--hl); box-decoration-break: clone; -webkit-box-decoration-break: clone; }
  .a-hero p {
    font-size: 17px;
    line-height: 1.55;
    color: var(--muted);
    margin: 16px 0 0;
    text-wrap: pretty;
  }

  .a-steps {
    margin-top: 26px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .a-step { display: flex; gap: 14px; align-items: flex-start; }
  .a-num {
    flex: none;
    width: 28px;
    height: 28px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    font-family: var(--mono);
    font-size: 13px;
    font-weight: 700;
    color: var(--accent);
    background: var(--accent-soft);
    border: 1px solid var(--accent-line);
  }
  .a-step-title { font-weight: 600; color: var(--ink); font-size: 15px; }
  .a-step-text { font-size: 14px; margin-top: 2px; line-height: 1.5; }

  .a-h2 {
    font-family: var(--display);
    font-size: 24px;
    letter-spacing: -.02em;
    color: var(--ink);
    margin: 28px 0 14px;
  }

  .a-faq details {
    border-bottom: 1px solid var(--line);
  }
  .a-faq details:first-child { border-top: 1px solid var(--line); }
  .a-faq summary {
    cursor: pointer;
    list-style: none;
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 16px;
    padding: 16px 2px;
    font-weight: 600;
    font-size: 15.5px;
    color: var(--ink);
  }
  .a-faq summary::-webkit-details-marker { display: none; }
  .a-faq summary::after {
    content: '+';
    flex: none;
    font-family: var(--mono);
    font-size: 18px;
    color: var(--accent);
    transition: transform .2s;
  }
  .a-faq details[open] summary::after { transform: rotate(45deg); }
  .a-faq summary:focus-visible { outline: 2px solid var(--accent-line); outline-offset: 2px; border-radius: 6px; }
  .a-faq p {
    font-size: 14.5px;
    line-height: 1.6;
    color: var(--muted);
    margin: 0 2px 16px;
    text-wrap: pretty;
  }
  .a-faq code {
    font-family: var(--mono);
    font-size: 13px;
    color: var(--ink);
    white-space: nowrap;
  }

  .a-faq a { color: var(--accent); }
  .a-faq .a-cmd {
    display: block;
    margin: 8px 0;
    padding: 10px 12px;
    border-radius: 10px;
    background: var(--inset);
    border: 1px solid var(--line);
    overflow-x: auto;
    white-space: nowrap;
  }

  .a-foot {
    margin: 28px 0 0;
    display: flex;
  }

  @media (max-width: 600px) {
    .a-wrap { padding: 28px 20px; }
    .a-hero h1 { font-size: 32px; }
  }
</style>
