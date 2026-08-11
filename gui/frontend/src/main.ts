import './style.css'
import { mount } from 'svelte'
import App from './App.svelte'

const target = document.getElementById('app')
if (!target) {
  throw new Error('App mount target not found')
}

const app = mount(App, {
  target,
})

export default app
