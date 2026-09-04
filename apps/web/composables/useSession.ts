import { v4 as uuidv4 } from 'uuid'

export const useSession = () => {
  const sessionUUID = useState<string>('session_uuid', () => {
    if (import.meta.client) {
      const stored = sessionStorage.getItem('converter_session_id')
      if (stored) return stored
      const newId = crypto.randomUUID ? crypto.randomUUID() : Math.random().toString(36).substring(2, 15)
      sessionStorage.setItem('converter_session_id', newId)
      return newId
    }
    return 'init-session'
  })

  return {
    sessionUUID
  }
}
