export interface ConversionFile {
  id: string
  file?: File
  name: string
  size: number
  sourceExt: string
  targetExt: string
  status: 'pending' | 'queued' | 'analyzing' | 'installing_tool' | 'converting' | 'ready' | 'error'
  progress: number
  message: string
  downloadUrl?: string
  error?: string
}

export const useConverter = () => {
  const config = useRuntimeConfig()
  const apiBase = config.public.apiBase
  const { sessionUUID } = useSession()

  const files = ref<ConversionFile[]>([])
  const globalTargetExt = ref<string>('pdf')
  const isUploading = ref<boolean>(false)
  let eventSource: EventSource | null = null

  // Restore last selected target format from localStorage
  onMounted(() => {
    const savedTarget = localStorage.getItem('last_target_format')
    if (savedTarget) {
      globalTargetExt.value = savedTarget
    }
    setupSSE()
  })

  onBeforeUnmount(() => {
    if (eventSource) {
      eventSource.close()
    }
  })

  const setGlobalTarget = (ext: string) => {
    globalTargetExt.value = ext
    localStorage.setItem('last_target_format', ext)
    files.value.forEach(f => {
      if (f.status === 'pending') {
        f.targetExt = ext
      }
    })
  }

  const setupSSE = () => {
    if (!import.meta.client || !sessionUUID.value) return
    if (eventSource) {
      eventSource.close()
    }

    const sseUrl = `${apiBase}/api/events/${sessionUUID.value}`
    eventSource = new EventSource(sseUrl)

    eventSource.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data)
        const targetFile = files.value.find(f => f.id === data.file_id)
        if (targetFile) {
          targetFile.status = data.status
          targetFile.progress = data.progress ?? targetFile.progress
          targetFile.message = data.message || ''
          if (data.download_url) {
            targetFile.downloadUrl = `${apiBase}${data.download_url}`
          }
          if (data.error) {
            targetFile.error = data.error
          }
        }
      } catch (e) {
        // Heartbeat or malformed data ignore
      }
    }

    eventSource.onerror = () => {
      // Reconnection handled automatically by browser EventSource
    }
  }

  const addFiles = (newFiles: FileList | File[]) => {
    for (const file of Array.from(newFiles)) {
      const ext = file.name.split('.').pop()?.toLowerCase() || ''
      files.value.push({
        id: crypto.randomUUID ? crypto.randomUUID() : Math.random().toString(36).substring(2, 9),
        file,
        name: file.name,
        size: file.size,
        sourceExt: ext,
        targetExt: globalTargetExt.value,
        status: 'pending',
        progress: 0,
        message: 'Ready to convert',
      })
    }
  }

  const removeFile = (id: string) => {
    files.value = files.value.filter(f => f.id !== id)
  }

  const clearAll = () => {
    files.value = []
  }

  const convertFile = async (item: ConversionFile) => {
    if (!item.file || item.status !== 'pending') return

    item.status = 'queued'
    item.progress = 5
    item.message = 'Uploading...'

    const formData = new FormData()
    formData.append('file', item.file)
    formData.append('session_uuid', sessionUUID.value)
    formData.append('target_ext', item.targetExt)

    try {
      const response = await fetch(`${apiBase}/api/upload`, {
        method: 'POST',
        body: formData,
      })

      if (!response.ok) {
        const errText = await response.text()
        throw new Error(errText || 'Upload failed')
      }

      const resData = await response.json()
      // Link server file_id to frontend file item
      item.id = resData.file_id
      item.status = 'queued'
      item.progress = 10
      item.message = 'Queued in worker...'
    } catch (err: any) {
      item.status = 'error'
      item.error = err.message || 'Failed to upload'
      item.message = 'Upload failed'
    }
  }

  const convertAll = async () => {
    isUploading.value = true
    const pending = files.value.filter(f => f.status === 'pending')
    for (const item of pending) {
      await convertFile(item)
    }
    isUploading.value = false
  }

  const downloadAllZip = () => {
    const zipUrl = `${apiBase}/api/download/zip?session=${sessionUUID.value}`
    window.open(zipUrl, '_blank')
  }

  const hasCompletedFiles = computed(() => {
    return files.value.some(f => f.status === 'ready')
  })

  return {
    files,
    globalTargetExt,
    isUploading,
    hasCompletedFiles,
    setGlobalTarget,
    addFiles,
    removeFile,
    clearAll,
    convertFile,
    convertAll,
    downloadAllZip,
  }
}
