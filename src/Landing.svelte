<script lang="ts">
  import { untrack } from 'svelte'
  import { generateRoomName, normalizeRoomCode, ROOM_WORDS } from './lib/roomName'
  import { WORDLIST } from './lib/wordlist'
  import { setPendingFiles } from './lib/pending'
  import Header from './lib/Header.svelte'
  import Footer from './lib/Footer.svelte'

  let { navigate, theme, invalidCode = '' }: {
    navigate: (to: string) => void
    theme: 'dark' | 'light'
    /** Code from a /r/#… link that failed validation, shown for correction. */
    invalidCode?: string
  } = $props()

  // Landing remounts when the route changes, so only the initial value matters.
  const initialCode = untrack(() => invalidCode)
  const initial = initialCode ? normalizeRoomCode(initialCode) : null
  let joinInput = $state(initialCode)
  let joinError = $state(initial && 'error' in initial ? initial.error : '')

  // The code shown is the one the session will use, so what you see is what you share.
  // Its first two words only pick the signaling topic; the last two are the PAKE secret.
  const sessionCode = generateRoomName()
  const codeWords = sessionCode.split('-')

  function startSession(files: File[] = []) {
    setPendingFiles(files)
    sessionStorage.setItem(`host:${sessionCode}`, '1')
    navigate(`/r/#${sessionCode}`)
  }

  let fileInput = $state<HTMLInputElement>()
  let dragging = $state(false)

  function onPick(e: Event) {
    const input = e.currentTarget as HTMLInputElement
    const files = Array.from(input.files ?? [])
    input.value = ''
    if (files.length) startSession(files)
  }

  function onDrop(e: DragEvent) {
    e.preventDefault()
    dragging = false
    const files = Array.from(e.dataTransfer?.files ?? [])
    if (files.length) startSession(files)
  }
  function onDragOver(e: DragEvent) {
    if (!e.dataTransfer?.types.includes('Files')) return
    e.preventDefault()
    dragging = true
  }

  function joinSession() {
    const r = normalizeRoomCode(joinInput)
    if ('error' in r) joinError = r.error
    else navigate(`/r/#${r.roomId}`)
  }

  // Word completion for the code being typed. The wordlist is public, so this
  // reveals nothing: the code's strength is the random choice of words.
  const MAX_SUGGESTIONS = 6
  const WORDS = new Set(WORDLIST)
  let suggestIdx = $state(0)
  let suggestOpen = $state(true)

  const currentWord = $derived.by(() => {
    if (/[/#.:]/.test(joinInput)) return '' // pasted link — leave it alone
    return joinInput.toLowerCase().split(/[\s-]/).at(-1) ?? ''
  })
  const suggestions = $derived.by(() => {
    if (!currentWord) return []
    const matches = WORDLIST.filter((w) => w.startsWith(currentWord)).slice(0, MAX_SUGGESTIONS)
    return matches.length === 1 && matches[0] === currentWord ? [] : matches
  })
  const showSuggestions = $derived(suggestOpen && suggestions.length > 0)

  function onJoinInput(e: Event & { currentTarget: HTMLInputElement }) {
    joinError = ''
    suggestIdx = 0
    suggestOpen = true
    // No wordlist word is a prefix of another, so a typed word that matches one
    // is finished: add the dash. Only when typing at the end, so deleting a
    // dash or editing mid-code doesn't fight the user.
    const input = e.currentTarget
    if (!(e instanceof InputEvent && e.inputType.startsWith('insert')) || input.selectionEnd !== joinInput.length) return
    if (/[/#.:]/.test(joinInput)) return
    const words = joinInput.toLowerCase().split(/[\s-]+/)
    if (words.length < ROOM_WORDS && WORDS.has(words[words.length - 1])) joinInput += '-'
  }

  // Typing the separator yourself right after an auto-added dash: keep just the one.
  function onJoinBeforeInput(e: InputEvent & { currentTarget: HTMLInputElement }) {
    const input = e.currentTarget
    const atEnd = input.selectionStart === input.value.length && input.selectionEnd === input.value.length
    if (atEnd && /^[\s-]$/.test(e.data ?? '') && input.value.endsWith('-')) e.preventDefault()
  }

  function completeWord(word: string) {
    const words = joinInput.toLowerCase().split(/[\s-]+/)
    words[words.length - 1] = word
    joinInput = words.join('-') + (words.length < ROOM_WORDS ? '-' : '')
    suggestIdx = 0
  }

  function onJoinKeydown(e: KeyboardEvent) {
    if (showSuggestions) {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault()
        const n = suggestions.length
        suggestIdx = (suggestIdx + (e.key === 'ArrowDown' ? 1 : n - 1)) % n
        return
      }
      if (e.key === 'Tab' || e.key === 'Enter') {
        e.preventDefault()
        completeWord(suggestions[suggestIdx])
        return
      }
      if (e.key === 'Escape') {
        suggestOpen = false
        return
      }
    }
    if (e.key === 'Enter') joinSession()
  }
</script>

<svelte:window ondrop={onDrop} ondragover={onDragOver} ondragleave={(e) => { if (!e.relatedTarget) dragging = false }} />

<div class="pm t-{theme} l-wrap" class:l-dragging={dragging}>
  <div class="l-stack">

    <Header {navigate} />

    <div class="l-main">
      <!-- hero -->
      <div class="l-hero">
        <h1>Send files.<br />Skip the<br /><span class="l-em">server.</span></h1>
        <p>Four words are the whole connection. Say them out loud, paste them, scan them. Files go straight from one device to the other.</p>
      </div>

      <div class="l-actions">
        <div class="panel l-card">
          <div class="label">Session code</div>
          <div class="l-code" aria-label="Session code {sessionCode}">
            <div>{codeWords[0]}-{codeWords[1]}-</div>
            <div><mark>{codeWords[2]}-{codeWords[3]}</mark></div>
          </div>
          <div class="muted l-code-note">The last two words are a secret that never leaves your devices.</div>

          <div class="l-btns">
            <button class="btn btn-primary l-send" onclick={() => fileInput?.click()}>
              Choose file to send
              <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 19V5M5 12l7-7 7 7"/></svg>
            </button>
            <input type="file" multiple hidden bind:this={fileInput} onchange={onPick} />
            <button class="btn btn-ghost l-recv" onclick={() => startSession()}>
              Create session to receive
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M5 12h14M12 5l7 7-7 7"/></svg>
            </button>
          </div>
          <div class="faint l-hint">Or drop files anywhere on this page.</div>
        </div>

        <div>
          <label class="l-join-label" for="join-code">Have a code?</label>
          <div class="field">
            <div class="l-combo">
              <input
                id="join-code"
                class="input"
                class:l-invalid={!!joinError}
                type="text"
                placeholder="word-word-word-word"
                autocapitalize="off"
                autocomplete="off"
                spellcheck="false"
                enterkeyhint="go"
                role="combobox"
                aria-expanded={showSuggestions}
                aria-controls="join-suggestions"
                aria-autocomplete="list"
                aria-invalid={!!joinError}
                aria-describedby={joinError ? 'join-error' : undefined}
                aria-activedescendant={showSuggestions ? `join-suggestion-${suggestIdx}` : undefined}
                bind:value={joinInput}
                onbeforeinput={onJoinBeforeInput}
                oninput={onJoinInput}
                onkeydown={onJoinKeydown}
                onblur={() => (suggestOpen = false)}
                onfocus={() => (suggestOpen = true)}
              />
              {#if showSuggestions}
                <ul class="l-suggest" id="join-suggestions" role="listbox">
                  {#each suggestions as word, i}
                    <li
                      id="join-suggestion-{i}"
                      role="option"
                      aria-selected={i === suggestIdx}
                      class:on={i === suggestIdx}
                      onmousedown={(e) => { e.preventDefault(); completeWord(word) }}
                      onmouseenter={() => (suggestIdx = i)}
                    ><b>{word.slice(0, currentWord.length)}</b>{word.slice(currentWord.length)}</li>
                  {/each}
                </ul>
              {/if}
            </div>
            <button class="btn btn-ghost" style="flex: none" onclick={joinSession} disabled={!joinInput.trim()}>
              Join
            </button>
          </div>
          {#if joinError}
            <div class="l-error" id="join-error" role="alert">{joinError}</div>
          {/if}
        </div>
      </div>
    </div>

    <Footer />

  </div>
</div>

<style>
  .l-wrap {
    min-height: 100dvh;
    padding: 40px 64px 32px;
    display: flex;
    flex-direction: column;
  }

  .l-stack {
    flex: 1;
    width: 100%;
    max-width: 1080px;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
  }

  .l-main {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 64px;
    align-items: center;
    padding: 56px 0 44px;
  }

  .l-hero h1 {
    font-family: var(--display);
    font-weight: 800;
    letter-spacing: -.035em;
    font-size: clamp(56px, 8.4vw, 104px);
    line-height: .92;
    color: var(--ink);
  }
  /* Highlight starts below the top of the line box so it doesn't cover the descender of "Skip" */
  .l-em {
    background: linear-gradient(to bottom, transparent 19%, var(--hl) 19%, var(--hl) 83%, transparent 83%);
    color: #111110;
    padding: 0 12px;
    margin-left: -12px;
    box-decoration-break: clone;
  }
  .l-hero p {
    font-size: 20px;
    line-height: 1.45;
    color: var(--muted);
    margin: 28px 0 0;
    max-width: 460px;
    text-wrap: pretty;
  }

  .l-actions { display: flex; flex-direction: column; gap: 26px; min-width: 0; }
  .l-card { padding: 28px 32px; display: flex; flex-direction: column; gap: 20px; }
  .l-code {
    font-family: var(--mono);
    font-weight: 500;
    font-size: clamp(26px, 3.4vw, 40px);
    line-height: 1.4;
    letter-spacing: -.02em;
    overflow-wrap: anywhere;
  }
  .l-code mark { background: var(--hl); color: #111110; padding: 0 .12em; margin-left: -.12em; }
  .l-code-note { font-size: 14px; line-height: 1.4; margin-top: -6px; }
  .l-btns { display: flex; flex-direction: column; gap: 12px; }
  .l-send, .l-recv { width: 100%; justify-content: space-between; text-align: left; }
  .l-send { font-size: 24px; padding: 24px 26px; border-radius: 14px; }
  .l-recv { font-size: 16px; padding: 14px 24px; }
  .l-hint { font-size: 13px; margin-top: -8px; }
  .l-join-label { display: block; font-size: 14px; font-weight: 500; color: var(--muted); margin-bottom: 8px; }
  .l-dragging .l-card { background: var(--hl); color: #111110; --ink: #111110; --muted: #3b3a36; --faint: #3b3a36; }


  .l-combo {
    position: relative;
    flex: 1;
    min-width: 0;
  }
  .l-suggest {
    position: absolute;
    top: calc(100% + 6px);
    left: 0;
    right: 0;
    z-index: 10;
    margin: 0;
    padding: 4px;
    list-style: none;
    background: var(--panel);
    border: 1px solid var(--line2);
    border-radius: 12px;
    box-shadow: var(--shadow);
    font-family: var(--mono);
    font-size: 14px;
  }
  .l-suggest li {
    padding: 8px 11px;
    border-radius: 8px;
    color: var(--muted);
    cursor: pointer;
  }
  .l-suggest li b { color: var(--ink); font-weight: 600; }
  .l-suggest li.on { background: var(--accent-soft); color: var(--ink); }

  .pm .input.l-invalid { border-color: var(--danger); }
  .l-error {
    margin-top: 8px;
    font-size: 13px;
    color: var(--danger);
  }


  @media (hover: none) and (pointer: coarse) { .l-hint { display: none; } }
  @media (max-width: 900px) {
    .l-main { grid-template-columns: minmax(0, 1fr); }
    .l-main { gap: 40px; padding-top: 36px; }
  }
  @media (max-width: 600px) {
    .l-wrap { padding: 24px 20px; }
    .l-hero h1 { font-size: 56px; }
    .l-card { padding: 22px 20px; }
    .l-send { font-size: 20px; padding: 20px; }
  }
</style>
