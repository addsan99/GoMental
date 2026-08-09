import React from 'react'
import {createRoot} from 'react-dom/client'
import '@fontsource-variable/newsreader'
import '@fontsource-variable/newsreader/wght-italic.css'
import './mobile.css'
import {MobileApp} from './MobileApp'

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <MobileApp />
  </React.StrictMode>,
)
