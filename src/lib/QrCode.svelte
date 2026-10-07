<script lang="ts">
  import QRCode from 'qrcode'

  let { url, size = 150 }: { url: string; size?: number } = $props()

  let dataUrl = $state('')

  $effect(() => {
    QRCode.toDataURL(url, {
      width: size,
      margin: 1,
      color: { dark: '#0a0e14', light: '#ffffff' },
    }).then(d => { dataUrl = d })
  })
</script>

{#if dataUrl}
  <img src={dataUrl} alt="QR code for {url}" width={size} height={size} class="qr" />
{:else}
  <div class="qr placeholder" style="width:{size}px;height:{size}px"></div>
{/if}

<style>
  .qr { display: block; border-radius: 6px; }
  .placeholder {
    background: #e8e8e8;
    border-radius: 6px;
    animation: pulse 1.5s ease-in-out infinite;
  }
  @keyframes pulse {
    0%, 100% { opacity: 1 }
    50% { opacity: .5 }
  }
</style>
