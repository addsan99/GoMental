import React from 'react'
import {createRoot} from 'react-dom/client'
// Self-hosted reading serif (bundled into the binary — no startup network round
// trip). Only Newsreader is embedded: the UI sans and mono fall back to the
// platform fonts (Segoe UI / Consolas on Windows), which are strong enough that
// custom faces there weren't worth the download. woff2 subsets load on demand.
import '@fontsource-variable/newsreader'
import '@fontsource-variable/newsreader/wght-italic.css'
import './style.css'
import App from './App'

const container = document.getElementById('root')

const root = createRoot(container!)

root.render(
    <React.StrictMode>
        <App/>
    </React.StrictMode>
)
