import { mount } from 'svelte'
import '@fontsource-variable/bricolage-grotesque'
import '@fontsource/dm-mono/400.css'
import '@fontsource/dm-mono/500.css'
import './app.css'
import App from './App.svelte'

const app = mount(App, {
  target: document.getElementById('app')!,
})

export default app
