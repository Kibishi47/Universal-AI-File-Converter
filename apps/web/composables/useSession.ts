
export const useSession = () => {
  const config = useRuntimeConfig()
  const apiBase = config.public.apiBase
  const sessionUUID = useState<string>('session_uuid', () => '')

  const initSession = async () => {
    if (!import.meta.client) return
    const stored = localStorage.getItem('converter_session_id')
    if (stored && /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(stored)) {
      sessionUUID.value = stored
      return
    }

    try {
      const res = await fetch(`${apiBase}/api/session`, { method: 'POST' })
      if (res.ok) {
        const data = await res.json()
        if (data.session_uuid) {
          sessionUUID.value = data.session_uuid
          localStorage.setItem('converter_session_id', data.session_uuid)
          return
        }
      }
    } catch {
      // Fallback if offline
    }

    // Fallback client generation if server endpoint unreachable
    const fallbackId = generateUUIDv4()
    sessionUUID.value = fallbackId
    localStorage.setItem('converter_session_id', fallbackId)
  }

  onMounted(() => {
    initSession()
  })

  return {
    sessionUUID,
    initSession
  }
}

function generateUUIDv4(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  // RFC4122 v4 compliant fallback using crypto.getRandomValues if randomUUID is unavailable
  if (typeof crypto !== 'undefined' && typeof crypto.getRandomValues === 'function') {
    const bytes = new Uint8Array(16)
    crypto.getRandomValues(bytes)
    bytes[6] = (bytes[6] & 0x0f) | 0x40 // version 4
    bytes[8] = (bytes[8] & 0x3f) | 0x80 // variant RFC4122
    const hex = Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('')
    return `${hex.substring(0, 8)}-${hex.substring(8, 12)}-${hex.substring(12, 16)}-${hex.substring(16, 20)}-${hex.substring(20)}`
  }
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0
    const v = c === 'x' ? r : (r & 0x3) | 0x8
    return v.toString(16)
  })
}
