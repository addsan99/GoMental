import React from 'react'
import {createRoot} from 'react-dom/client'
// Self-hosted reading fonts (bundled into the binary — no startup network round
// trip). The platform stacks remain available as choices, but these faces render
// consistently on every supported desktop OS.
import '@fontsource-variable/newsreader'
import '@fontsource-variable/newsreader/wght-italic.css'
import '@fontsource-variable/open-sans'
import '@fontsource-variable/roboto'
import './style.css'
import App from './App'

const container = document.getElementById('root')

const root = createRoot(container!)

root.render(
    <React.StrictMode>
        <App/>
    </React.StrictMode>
)
