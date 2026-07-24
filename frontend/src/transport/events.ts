// Shared EventSource manager for the HTTP transport.
// Opens a single lazy connection to /api/events and fans events out to listeners,
// auto-reconnecting with backoff when the connection drops.

type Listener = (payload: any) => void

const listeners = new Map<string, Set<Listener>>()
const wrappers = new Map<string, EventListener>()

let source: EventSource | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let backoff = 1000
const maxBackoff = 30000

function attachListener(name: string) {
  if (!source || wrappers.has(name)) {
    return
  }
  const wrapper: EventListener = (event) => {
    const messageEvent = event as MessageEvent
    let payload: any
    try {
      payload = messageEvent.data ? JSON.parse(messageEvent.data) : undefined
    } catch {
      payload = messageEvent.data
    }
    const bucket = listeners.get(name)
    if (!bucket) {
      return
    }
    for (const listener of bucket) {
      listener(payload)
    }
  }
  wrappers.set(name, wrapper)
  source.addEventListener(name, wrapper)
}

function connect() {
  if (source || typeof EventSource === 'undefined') {
    return
  }
  const opened = new EventSource('/api/events')
  source = opened
  opened.onopen = () => {
    backoff = 1000
  }
  for (const name of listeners.keys()) {
    attachListener(name)
  }
  opened.onerror = () => {
    opened.close()
    if (source === opened) {
      source = null
    }
    wrappers.clear()
    scheduleReconnect()
  }
}

function scheduleReconnect() {
  if (reconnectTimer !== null || listeners.size === 0) {
    return
  }
  const delay = backoff
  backoff = Math.min(backoff * 2, maxBackoff)
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null
    if (listeners.size > 0) {
      connect()
    }
  }, delay)
}

function closeIfIdle() {
  if (listeners.size > 0) {
    return
  }
  if (reconnectTimer !== null) {
    clearTimeout(reconnectTimer)
    reconnectTimer = null
  }
  if (source) {
    source.close()
    source = null
  }
  wrappers.clear()
  backoff = 1000
}

export function subscribe(name: string, cb: Listener): () => void {
  let bucket = listeners.get(name)
  if (!bucket) {
    bucket = new Set()
    listeners.set(name, bucket)
  }
  bucket.add(cb)

  connect()
  attachListener(name)

  return () => {
    const current = listeners.get(name)
    if (current) {
      current.delete(cb)
      if (current.size === 0) {
        listeners.delete(name)
        const wrapper = wrappers.get(name)
        if (wrapper && source) {
          source.removeEventListener(name, wrapper)
        }
        wrappers.delete(name)
      }
    }
    closeIfIdle()
  }
}
