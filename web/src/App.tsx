import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type DragEvent as ReactDragEvent,
  type PointerEvent as ReactPointerEvent,
} from 'react'
import './App.css'
import brandLogo from './assets/bdinfo-logo.png'

type BrowseEntry = {
  name: string
  path: string
  type: string
  selectable: boolean
  modifiedAt?: string
  sizeBytes?: number
}

type BrowseResponse = {
  path: string
  parent?: string
  entries: BrowseEntry[]
}

type SourceLocation = {
  id: string
  name: string
  path: string
}

type Favorite = {
  id: string
  name: string
  path: string
}

type DiscInfo = {
  Path: string
  Title: string
  Label: string
  SizeBytes: number
  IsBDPlus: boolean
  IsBDJava: boolean
  IsDBOX: boolean
  IsPSP: boolean
  Is3D: boolean
  Is50Hz: boolean
  IsUHD: boolean
}

type PlaylistInfo = {
  Name: string
  LengthSeconds: number
  SizeBytes: number
  TotalBitrateBps: number
  HasHiddenTracks: boolean
  IsValid: boolean
}

type DiscoverResponse = {
  disc: DiscInfo
  playlists: PlaylistInfo[]
  mainPlaylist?: PlaylistInfo
  validPlaylistCount: number
  filteredPlaylistCount: number
  durationMs: number
}

type ScanMode = 'main' | 'selected' | 'all'

type ReportView = 'standard' | 'summary' | 'forums'
type ReportExportFormat = 'txt' | 'nfo'

type ScanReportResponse = {
  id: string
  view: ReportView
  playlist?: string
  report: string
}

type ScanProgress = {
  stage: string
  currentPlaylist?: string
  currentProcessedBytes?: number
  currentTotalBytes?: number
  currentPercent?: number
  processedBytes: number
  totalBytes: number
  percent: number
}

type ScanResult = {
  disc: DiscInfo
  playlists: PlaylistInfo[]
  durationMs: number
}

type ScanJob = {
  id: string
  status:
    | 'queued'
    | 'running'
    | 'cancelling'
    | 'cancelled'
    | 'completed'
    | 'failed'
  request: {
    path: string
    mode: ScanMode
    playlists?: string[]
  }
  progress?: ScanProgress
  elapsedMs: number
  result?: ScanResult
  error?: string
}

type BrowserSortKey =
  | 'name'
  | 'modified'
  | 'kind'
  | 'size'

type PlaylistSortKey =
  | 'playlist'
  | 'duration'
  | 'size'
  | 'bitrate'
  | 'status'

type SortDirection = 'asc' | 'desc'

type BrowserColumnKey =
  | 'name'
  | 'modified'
  | 'kind'
  | 'size'

type BrowserColumnWidths =
  Record<BrowserColumnKey, number>

const defaultColumnWidths: BrowserColumnWidths = {
  name: 390,
  modified: 190,
  kind: 140,
  size: 110,
}

const minimumColumnWidths: BrowserColumnWidths = {
  name: 180,
  modified: 140,
  kind: 100,
  size: 80,
}

function formatDuration(seconds: number) {
  const total = Math.max(0, Math.floor(seconds))
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const secs = total % 60

  return `${hours}:${minutes
    .toString()
    .padStart(2, '0')}:${secs
    .toString()
    .padStart(2, '0')}`
}

function formatGiB(bytes: number) {
  return `${(bytes / 1024 ** 3).toFixed(2)} GiB`
}

function formatMbps(bits: number) {
  return `${(bits / 1_000_000).toFixed(2)} Mbps`
}

function formatElapsedMS(milliseconds: number) {
  if (milliseconds < 1000) {
    return '<1 s'
  }

  const totalSeconds = Math.round(milliseconds / 1000)

  if (totalSeconds < 60) {
    return `${totalSeconds} s`
  }

  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60

  return `${minutes}m ${seconds}s`
}

function formatProgressBytes(bytes: number) {
  if (!bytes) {
    return '0 GiB'
  }

  return `${(bytes / 1024 ** 3).toFixed(2)} GiB`
}

function averageReadSpeed(
  processedBytes: number,
  elapsedMs: number,
) {
  if (processedBytes <= 0 || elapsedMs <= 0) {
    return 0
  }

  return processedBytes / (elapsedMs / 1000)
}

function formatReadSpeed(bytesPerSecond: number) {
  if (bytesPerSecond <= 0) {
    return '—'
  }

  const mib = bytesPerSecond / 1024 ** 2

  if (mib < 1024) {
    return `${mib.toFixed(1)} MiB/s`
  }

  return `${(mib / 1024).toFixed(2)} GiB/s`
}

function formatETA(
  processedBytes: number,
  totalBytes: number,
  elapsedMs: number,
) {
  if (
    processedBytes <= 0 ||
    totalBytes <= processedBytes ||
    elapsedMs <= 0
  ) {
    return ''
  }

  const speed = averageReadSpeed(
    processedBytes,
    elapsedMs,
  )

  if (speed <= 0) {
    return ''
  }

  const remainingSeconds =
    (totalBytes - processedBytes) / speed

  return formatElapsedMS(
    remainingSeconds * 1000,
  )
}

function formatScanStage(stage?: string) {
  if (!stage) {
    return 'Preparing'
  }

  return stage
    .replace(/[_-]+/g, ' ')
    .replace(/\b\w/g, (letter) => letter.toUpperCase())
}

function formatModified(value?: string) {
  if (!value) {
    return '—'
  }

  const date = new Date(value)

  if (Number.isNaN(date.getTime())) {
    return '—'
  }

  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  }).format(date)
}

function playlistNumber(name: string) {
  const match = name.match(/\d+/)
  return match ? Number(match[0]) : Number.MAX_SAFE_INTEGER
}

function basename(path: string) {
  const clean = path.replace(/\/+$/, '')
  return clean.split('/').pop() || path
}

function isISO(path: string) {
  return path.toLowerCase().endsWith('.iso')
}

function isBDMV(path: string) {
  return basename(path).toLowerCase() === 'bdmv'
}

function browserKind(entry: BrowseEntry) {
  if (entry.type.toLowerCase() === 'iso' || isISO(entry.path)) {
    return 'ISO image'
  }

  if (
    entry.type.toLowerCase() === 'bdmv' ||
    (entry.selectable && isBDMV(entry.path))
  ) {
    return 'BDMV folder'
  }

  return 'Folder'
}

function browserSize(entry: BrowseEntry) {
  if (
    entry.type.toLowerCase() !== 'iso' &&
    !isISO(entry.path)
  ) {
    return '—'
  }

  if (!entry.sizeBytes) {
    return '—'
  }

  return formatGiB(entry.sizeBytes)
}

async function readAPIError(response: Response) {
  try {
    const body = (await response.json()) as {
      error?: string
    }

    return body.error || `Request failed (${response.status})`
  } catch {
    return `Request failed (${response.status})`
  }
}

function reportLineClass(line: string) {
  const trimmed = line.trim()

  if (!trimmed) {
    return ''
  }

  if (/^\*{4,}/.test(trimmed)) {
    return 'reportSeparator'
  }

  if (
    /^(QUICK SUMMARY|PLAYLIST(?: REPORT)?|DISC INFO|VIDEO|AUDIO|SUBTITLES?|FILES|CHAPTERS|STREAM DIAGNOSTICS):/.test(
      trimmed,
    )
  ) {
    return 'reportSectionLine'
  }

  const colon = trimmed.indexOf(':')

  if (colon > 0 && colon < 24) {
    return 'reportFieldLine'
  }

  return ''
}

function App() {
  const [sourcePath, setSourcePath] = useState('')
  const [discovery, setDiscovery] =
    useState<DiscoverResponse | null>(null)
  const [discovering, setDiscovering] = useState(false)
  const [discoverError, setDiscoverError] = useState('')

  const [scanMode, setScanMode] =
    useState<ScanMode>('main')
  const [selectedPlaylistNames, setSelectedPlaylistNames] =
    useState<string[]>([])
  const [scanJob, setScanJob] =
    useState<ScanJob | null>(null)
  const [scanError, setScanError] = useState('')

  const [reportView, setReportView] =
    useState<ReportView>('standard')
  const [reportPlaylist, setReportPlaylist] = useState('')
  const [scanReport, setScanReport] = useState('')
  const [reportLoading, setReportLoading] = useState(false)
  const [reportError, setReportError] = useState('')
  const [reportCopied, setReportCopied] = useState(false)
  const [reportExportFormat, setReportExportFormat] =
    useState<ReportExportFormat>('txt')

  const [scanNotifications, setScanNotifications] =
    useState(() => {
      return (
        localStorage.getItem(
          'bdinfo-scan-notifications-v1',
        ) === 'true'
      )
    })

  const previousScanStatus = useRef<{
    id: string
    status: string
  } | null>(null)

  const notificationsSupported =
    typeof window !== 'undefined' &&
    'Notification' in window

  const [browserOpen, setBrowserOpen] = useState(false)
  const [browsePath, setBrowsePath] = useState('')
  const [browseParent, setBrowseParent] = useState('')
  const [browseEntries, setBrowseEntries] =
    useState<BrowseEntry[]>([])
  const [browseLoading, setBrowseLoading] = useState(false)
  const [browseError, setBrowseError] = useState('')
  const [search, setSearch] = useState('')

  const [locations, setLocations] =
    useState<SourceLocation[]>([])
  const [favorites, setFavorites] =
    useState<Favorite[]>([])
  const [favoritesError, setFavoritesError] = useState('')
  const [favoriteDropActive, setFavoriteDropActive] =
    useState(false)

  const [browserSortKey, setBrowserSortKey] =
    useState<BrowserSortKey>('name')
  const [browserSortDirection, setBrowserSortDirection] =
    useState<SortDirection>('asc')

  const [playlistSortKey, setPlaylistSortKey] =
    useState<PlaylistSortKey>('playlist')
  const [playlistSortDirection, setPlaylistSortDirection] =
    useState<SortDirection>('asc')

  const [columnWidths, setColumnWidths] =
    useState<BrowserColumnWidths>(() => {
      try {
        const saved = localStorage.getItem(
          'bdinfo-browser-column-widths-v1',
        )

        if (!saved) {
          return defaultColumnWidths
        }

        return {
          ...defaultColumnWidths,
          ...JSON.parse(saved),
        }
      } catch {
        return defaultColumnWidths
      }
    })

  useEffect(() => {
    localStorage.setItem(
      'bdinfo-browser-column-widths-v1',
      JSON.stringify(columnWidths),
    )
  }, [columnWidths])

  useEffect(() => {
    localStorage.setItem(
      'bdinfo-scan-notifications-v1',
      String(scanNotifications),
    )
  }, [scanNotifications])

  useEffect(() => {
    if (!scanJob) {
      previousScanStatus.current = null
      return
    }

    const previous = previousScanStatus.current

    const justCompleted =
      previous?.id === scanJob.id &&
      previous.status !== 'completed' &&
      scanJob.status === 'completed'

    previousScanStatus.current = {
      id: scanJob.id,
      status: scanJob.status,
    }

    if (
      !justCompleted ||
      !scanNotifications ||
      !notificationsSupported ||
      Notification.permission !== 'granted'
    ) {
      return
    }

    try {
      new Notification('BDInfo scan complete', {
        body: `Bitrate scan completed in ${formatElapsedMS(
          scanJob.elapsedMs,
        )}`,
      })
    } catch {
      // Ignore notification display errors.
    }
  }, [
    scanJob?.id,
    scanJob?.status,
    scanNotifications,
    notificationsSupported,
  ])

  useEffect(() => {
    if (
      !scanJob?.id ||
      !['queued', 'running', 'cancelling'].includes(
        scanJob.status,
      )
    ) {
      return
    }

    let requestRunning = false

    const interval = window.setInterval(async () => {
      if (requestRunning) {
        return
      }

      requestRunning = true

      try {
        const response = await fetch(
          `/api/scans/${encodeURIComponent(scanJob.id)}`,
        )

        if (!response.ok) {
          throw new Error(await readAPIError(response))
        }

        const job = (await response.json()) as ScanJob
        setScanJob(job)

        if (job.status === 'failed') {
          setScanError(job.error || 'Scan failed')
        }

        if (job.status === 'cancelled') {
          setScanError('')
        }
      } catch (error) {
        setScanError(
          error instanceof Error
            ? error.message
            : 'Unable to read scan status',
        )
      } finally {
        requestRunning = false
      }
    }, 650)

    return () => {
      window.clearInterval(interval)
    }
  }, [scanJob?.id, scanJob?.status])

  useEffect(() => {
    if (
      !scanJob?.id ||
      scanJob.status !== 'completed'
    ) {
      return
    }

    void loadScanReport(
      scanJob.id,
      reportView,
      reportPlaylist,
    )
  }, [
    scanJob?.id,
    scanJob?.status,
    reportView,
    reportPlaylist,
  ])

  const loadNavigation = async () => {
    setFavoritesError('')

    try {
      const [locationResponse, favoriteResponse] =
        await Promise.all([
          fetch('/api/locations'),
          fetch('/api/favorites'),
        ])

      if (!locationResponse.ok) {
        throw new Error(
          await readAPIError(locationResponse),
        )
      }

      if (!favoriteResponse.ok) {
        throw new Error(
          await readAPIError(favoriteResponse),
        )
      }

      const locationJSON =
        (await locationResponse.json()) as {
          locations: SourceLocation[]
        }

      const favoriteJSON =
        (await favoriteResponse.json()) as {
          favorites: Favorite[]
        }

      setLocations(locationJSON.locations ?? [])
      setFavorites(favoriteJSON.favorites ?? [])
    } catch (error) {
      setFavoritesError(
        error instanceof Error
          ? error.message
          : 'Unable to load locations and favorites',
      )
    }
  }

  const loadBrowser = async (path?: string) => {
    setBrowseLoading(true)
    setBrowseError('')
    setSearch('')

    try {
      const query = path
        ? `?path=${encodeURIComponent(path)}`
        : ''

      const response = await fetch(`/api/browse${query}`)

      if (!response.ok) {
        throw new Error(await readAPIError(response))
      }

      const data =
        (await response.json()) as BrowseResponse

      setBrowsePath(data.path)
      setBrowseParent(data.parent ?? '')
      setBrowseEntries(data.entries ?? [])
    } catch (error) {
      setBrowseError(
        error instanceof Error
          ? error.message
          : 'Unable to browse this location',
      )
    } finally {
      setBrowseLoading(false)
    }
  }

  const showBrowser = async () => {
    setBrowserOpen(true)

    await Promise.all([
      loadNavigation(),
      loadBrowser(browsePath || undefined),
    ])
  }

  const discoverSource = async (path: string) => {
    setDiscovering(true)
    setDiscoverError('')
    setDiscovery(null)
    setScanJob(null)
    setScanError('')
    setScanReport('')
    setReportError('')
    setReportPlaylist('')
    setReportView('standard')
    setReportCopied(false)
    setScanMode('main')
    setSelectedPlaylistNames([])

    try {
      const response = await fetch('/api/discover', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          path,
        }),
      })

      if (!response.ok) {
        throw new Error(await readAPIError(response))
      }

      const data =
        (await response.json()) as DiscoverResponse

      setDiscovery(data)

      const initialPlaylist =
        data.mainPlaylist?.Name ||
        data.playlists.find((playlist) => playlist.IsValid)?.Name ||
        data.playlists[0]?.Name ||
        ''

      setSelectedPlaylistNames(
        initialPlaylist ? [initialPlaylist] : [],
      )
    } catch (error) {
      setDiscoverError(
        error instanceof Error
          ? error.message
          : 'Discovery failed',
      )
    } finally {
      setDiscovering(false)
    }
  }

  const selectSource = async (path: string) => {
    setSourcePath(path)
    setBrowserOpen(false)
    await discoverSource(path)
  }

  const toggleScanNotifications = async () => {
    if (scanNotifications) {
      setScanNotifications(false)
      return
    }

    if (!notificationsSupported) {
      setScanError(
        'Browser notifications are not available for this site',
      )
      return
    }

    let permission = Notification.permission

    if (permission === 'default') {
      permission = await Notification.requestPermission()
    }

    if (permission !== 'granted') {
      setScanNotifications(false)
      setScanError(
        'Browser notification permission was not granted',
      )
      return
    }

    setScanError('')
    setScanNotifications(true)
  }

  const loadScanReport = async (
    jobID: string,
    view: ReportView,
    playlist: string,
  ) => {
    setReportLoading(true)
    setReportError('')
    setReportCopied(false)

    try {
      const params = new URLSearchParams()
      params.set('view', view)

      if (playlist) {
        params.set('playlist', playlist)
      }

      const response = await fetch(
        `/api/scans/${encodeURIComponent(
          jobID,
        )}/report?${params.toString()}`,
      )

      if (!response.ok) {
        throw new Error(await readAPIError(response))
      }

      const data =
        (await response.json()) as ScanReportResponse

      setScanReport(data.report)
    } catch (error) {
      setScanReport('')
      setReportError(
        error instanceof Error
          ? error.message
          : 'Unable to load scan report',
      )
    } finally {
      setReportLoading(false)
    }
  }

  const copyScanReport = async () => {
    if (!scanReport) {
      return
    }

    let copied = false

    if (
      window.isSecureContext &&
      navigator.clipboard?.writeText
    ) {
      try {
        await navigator.clipboard.writeText(scanReport)
        copied = true
      } catch {
        copied = false
      }
    }

    if (!copied) {
      const textarea = document.createElement('textarea')

      textarea.value = scanReport
      textarea.setAttribute('readonly', '')
      textarea.style.position = 'fixed'
      textarea.style.left = '-9999px'
      textarea.style.top = '0'
      textarea.style.opacity = '0'

      document.body.appendChild(textarea)

      textarea.focus()
      textarea.select()
      textarea.setSelectionRange(
        0,
        textarea.value.length,
      )

      try {
        copied = document.execCommand('copy')
      } catch {
        copied = false
      } finally {
        document.body.removeChild(textarea)
      }
    }

    if (copied) {
      setReportError('')
      setReportCopied(true)

      window.setTimeout(() => {
        setReportCopied(false)
      }, 1800)

      return
    }

    setReportCopied(false)
    setReportError('Unable to copy report to clipboard')
  }

  const exportScanReport = () => {
    if (!scanReport) {
      return
    }

    const label =
      discovery?.disc.Label?.trim() || 'BDInfo'

    const safeLabel = label
      .replace(/[<>:"/\\|?*\u0000-\u001F]/g, '_')
      .replace(/\s+/g, ' ')
      .trim()

    const scope = reportPlaylist
      ? reportPlaylist.replace(/\.MPLS$/i, '')
      : 'complete'

    const filename =
      `${safeLabel}.${scope}.${reportView}.${reportExportFormat}`

    const blob = new Blob([scanReport], {
      type: 'text/plain;charset=utf-8',
    })

    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')

    anchor.href = url
    anchor.download = filename

    document.body.appendChild(anchor)
    anchor.click()
    document.body.removeChild(anchor)

    window.setTimeout(() => {
      URL.revokeObjectURL(url)
    }, 0)
  }

  const startScan = async () => {
    if (!sourcePath || !discovery) {
      return
    }

    setScanReport('')
    setReportError('')
    setReportPlaylist('')
    setReportView('standard')
    setReportCopied(false)

    if (
      scanMode === 'selected' &&
      selectedPlaylistNames.length === 0
    ) {
      setScanError('Select at least one playlist to scan')
      return
    }

    setScanError('')

    try {
      const request: {
        path: string
        mode: ScanMode
        playlists?: string[]
      } = {
        path: sourcePath,
        mode: scanMode,
      }

      if (scanMode === 'selected') {
        request.playlists = selectedPlaylistNames
      }

      const response = await fetch('/api/scans', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(request),
      })

      if (!response.ok) {
        throw new Error(await readAPIError(response))
      }

      const job = (await response.json()) as ScanJob
      setScanJob(job)
    } catch (error) {
      setScanError(
        error instanceof Error
          ? error.message
          : 'Unable to start scan',
      )
    }
  }

  const cancelScan = async () => {
    if (!scanJob?.id) {
      return
    }

    setScanError('')

    try {
      const response = await fetch(
        `/api/scans/${encodeURIComponent(scanJob.id)}`,
        {
          method: 'DELETE',
        },
      )

      if (!response.ok) {
        throw new Error(await readAPIError(response))
      }

      const job = (await response.json()) as ScanJob
      setScanJob(job)
    } catch (error) {
      setScanError(
        error instanceof Error
          ? error.message
          : 'Unable to cancel scan',
      )
    }
  }

  const favoriteForPath = (path: string) =>
    favorites.find((favorite) => favorite.path === path)

  const addFavorite = async (
    path: string,
    name?: string,
  ) => {
    if (favoriteForPath(path)) {
      return
    }

    setFavoritesError('')

    const response = await fetch('/api/favorites', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        name: name || basename(path),
        path,
      }),
    })

    if (!response.ok) {
      setFavoritesError(await readAPIError(response))
      return
    }

    await loadNavigation()
  }

  const removeFavorite = async (favorite: Favorite) => {
    setFavoritesError('')

    const response = await fetch(
      `/api/favorites/${encodeURIComponent(
        favorite.id,
      )}`,
      {
        method: 'DELETE',
      },
    )

    if (!response.ok) {
      setFavoritesError(await readAPIError(response))
      return
    }

    await loadNavigation()
  }

  const toggleFavorite = async (entry: BrowseEntry) => {
    const existing = favoriteForPath(entry.path)

    if (existing) {
      await removeFavorite(existing)
      return
    }

    await addFavorite(entry.path, entry.name)
  }

  const openFavorite = async (favorite: Favorite) => {
    if (isISO(favorite.path) || isBDMV(favorite.path)) {
      await selectSource(favorite.path)
      return
    }

    await loadBrowser(favorite.path)
  }

  const handleFavoriteDrop = async (
    event: ReactDragEvent<HTMLElement>,
  ) => {
    event.preventDefault()
    setFavoriteDropActive(false)

    const payload =
      event.dataTransfer.getData(
        'application/x-bdinfo-entry',
      )

    if (!payload) {
      return
    }

    try {
      const entry = JSON.parse(payload) as {
        path: string
        name: string
      }

      await addFavorite(entry.path, entry.name)
    } catch {
      setFavoritesError('Unable to add dropped favorite')
    }
  }

  const startColumnResize = (
    key: BrowserColumnKey,
    event: ReactPointerEvent<HTMLSpanElement>,
  ) => {
    event.preventDefault()
    event.stopPropagation()

    const startX = event.clientX
    const startWidth = columnWidths[key]

    const move = (moveEvent: PointerEvent) => {
      const delta = moveEvent.clientX - startX

      setColumnWidths((current) => ({
        ...current,
        [key]: Math.max(
          minimumColumnWidths[key],
          startWidth + delta,
        ),
      }))
    }

    const stop = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', stop)
      document.body.classList.remove('resizingColumns')
    }

    document.body.classList.add('resizingColumns')
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', stop)
  }

  const changeBrowserSort = (key: BrowserSortKey) => {
    if (browserSortKey === key) {
      setBrowserSortDirection((current) =>
        current === 'asc' ? 'desc' : 'asc',
      )
      return
    }

    setBrowserSortKey(key)
    setBrowserSortDirection('asc')
  }

  const changePlaylistSort = (key: PlaylistSortKey) => {
    if (playlistSortKey === key) {
      setPlaylistSortDirection((current) =>
        current === 'asc' ? 'desc' : 'asc',
      )
      return
    }

    setPlaylistSortKey(key)
    setPlaylistSortDirection('asc')
  }

  const browserSortSymbol = (key: BrowserSortKey) => {
    if (browserSortKey !== key) {
      return '↕'
    }

    return browserSortDirection === 'asc' ? '↑' : '↓'
  }

  const playlistSortSymbol = (key: PlaylistSortKey) => {
    if (playlistSortKey !== key) {
      return '↕'
    }

    return playlistSortDirection === 'asc'
      ? '↑'
      : '↓'
  }

  const filteredEntries = useMemo(() => {
    const query = search.trim().toLowerCase()

    if (!query) {
      return browseEntries
    }

    return browseEntries.filter((entry) =>
      entry.name.toLowerCase().includes(query),
    )
  }, [browseEntries, search])

  const sortedBrowserEntries = useMemo(() => {
    const entries = [...filteredEntries]

    entries.sort((a, b) => {
      let result = 0

      switch (browserSortKey) {
        case 'name':
          result = a.name.localeCompare(
            b.name,
            undefined,
            {
              numeric: true,
              sensitivity: 'base',
            },
          )
          break

        case 'modified':
          result =
            new Date(a.modifiedAt ?? 0).getTime() -
            new Date(b.modifiedAt ?? 0).getTime()
          break

        case 'kind':
          result = browserKind(a).localeCompare(
            browserKind(b),
          )
          break

        case 'size':
          result =
            (a.sizeBytes ?? 0) - (b.sizeBytes ?? 0)
          break
      }

      return browserSortDirection === 'asc'
        ? result
        : -result
    })

    return entries
  }, [
    filteredEntries,
    browserSortKey,
    browserSortDirection,
  ])

  const sortedPlaylists = useMemo(() => {
    const playlists = [...(discovery?.playlists ?? [])]

    playlists.sort((a, b) => {
      let result = 0

      switch (playlistSortKey) {
        case 'playlist':
          result =
            playlistNumber(a.Name) -
            playlistNumber(b.Name)
          break

        case 'duration':
          result =
            a.LengthSeconds - b.LengthSeconds
          break

        case 'size':
          result = a.SizeBytes - b.SizeBytes
          break

        case 'bitrate':
          result =
            a.TotalBitrateBps -
            b.TotalBitrateBps
          break

        case 'status':
          result =
            Number(a.IsValid) -
            Number(b.IsValid)
          break
      }

      return playlistSortDirection === 'asc'
        ? result
        : -result
    })

    return playlists
  }, [
    discovery,
    playlistSortKey,
    playlistSortDirection,
  ])

  const browserTableWidth =
    44 +
    columnWidths.name +
    columnWidths.modified +
    columnWidths.kind +
    columnWidths.size +
    96

  const scanActive =
    scanJob !== null &&
    ['queued', 'running', 'cancelling'].includes(scanJob.status)

  const scanCompleted =
    scanJob?.status === 'completed'

  const scanPercent = Math.min(
    100,
    Math.max(0, scanJob?.progress?.percent ?? 0),
  )

  const currentPlaylist =
    scanJob?.progress?.currentPlaylist ?? ''

  const currentPlaylistPercent = Math.min(
    100,
    Math.max(
      0,
      scanJob?.progress?.currentPercent ?? scanPercent,
    ),
  )

  const selectedPlaylistCount =
    scanJob?.request.playlists?.length ?? 0

  const showOverallProgress =
    scanJob?.request.mode === 'all' ||
    (scanJob?.request.mode === 'selected' &&
      selectedPlaylistCount > 1)

  const reportPlaylists = Array.from(
    new Set(
      (scanJob?.result?.playlists ?? []).map(
        (playlist) => playlist.Name,
      ),
    ),
  ).sort((a, b) =>
    a.localeCompare(b, undefined, {
      numeric: true,
      sensitivity: 'base',
    }),
  )

  const features = discovery
    ? [
        ['UHD', discovery.disc.IsUHD],
        ['3D', discovery.disc.Is3D],
        ['50 Hz', discovery.disc.Is50Hz],
        ['BD-Java', discovery.disc.IsBDJava],
        ['BD+', discovery.disc.IsBDPlus],
        ['D-BOX', discovery.disc.IsDBOX],
        ['PSP', discovery.disc.IsPSP],
      ]
    : []

  const [theme, setTheme] = useState<'light' | 'dark'>(() => {
    const saved = localStorage.getItem('bdinfo-theme')

    if (saved === 'light' || saved === 'dark') {
      return saved
    }

    return window.matchMedia('(prefers-color-scheme: dark)').matches
      ? 'dark'
      : 'light'
  })

  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)')

    const handleSystemThemeChange = (event: MediaQueryListEvent) => {
      if (localStorage.getItem('bdinfo-theme')) {
        return
      }

      setTheme(event.matches ? 'dark' : 'light')
    }

    media.addEventListener('change', handleSystemThemeChange)

    return () => {
      media.removeEventListener('change', handleSystemThemeChange)
    }
  }, [])

  useEffect(() => {
    document.documentElement.dataset.theme = theme
  }, [theme])

  const toggleTheme = () => {
    const nextTheme = theme === 'dark' ? 'light' : 'dark'

    localStorage.setItem('bdinfo-theme', nextTheme)
    setTheme(nextTheme)
  }

  return (
    <main className="appShell">
      <header className="appHeader">
        <div>
          <div className="brandLogoWrap">
            <img
              className="brandLogo"
              src={brandLogo}
              alt="BDInfo"
            />
          </div>
          <p>Blu-ray structure and bitrate analysis</p>
        </div>

        <div className="headerActions">
          <button
            type="button"
            className="themeToggle"
            onClick={toggleTheme}
            aria-label={
              theme === 'dark'
                ? 'Switch to light theme'
                : 'Switch to dark theme'
            }
            title={
              theme === 'dark'
                ? 'Switch to light theme'
                : 'Switch to dark theme'
            }
          >
            {theme === 'dark' ? '☀' : '☾'}
          </button>

          <div
            className={[
              'readyBadge',
              discovering
                ? 'discovering'
                : scanActive
                  ? 'scanning'
                  : discoverError || scanError
                    ? 'error'
                    : 'ready',
            ].join(' ')}
          >
            {discovering
              ? 'Discovering'
              : scanActive
                ? 'Scanning'
                : discoverError || scanError
                  ? 'Error'
                  : 'Ready'}
          </div>
        </div>
      </header>

      <section className="panel sourcePanel">
        <div className="panelHeaderRow">
          <div className="sectionTitleRow">
            <h2>Select Source</h2>
            <p>Choose a BDMV folder or ISO file</p>
          </div>

          <button
            type="button"
            className="secondaryButton"
            onClick={() => void showBrowser()}
          >
            Browse
          </button>
        </div>

        <div className="sourcePath">
          {sourcePath || 'No source selected'}
        </div>
      </section>

      {!sourcePath && (
        <button
          type="button"
          className="panel sourceEmptyPicker"
          onClick={() => void showBrowser()}
        >
          <strong>Select a Blu-ray source</strong>
          <span>
            Disc details and playlists will appear here immediately after discovery.
          </span>
        </button>
      )}

      {discovering && (
        <section className="panel discoveryMessage">
          Discovering Blu-ray structure…
        </section>
      )}

      {discoverError && (
        <section className="panel errorPanel">
          {discoverError}
        </section>
      )}

      {discovery && (
        <>
          <section className="panel">
            <div className="sectionHeading discSectionHeading">
              <h2>Disc Details</h2>

              <p className="discAnalysisStatus">
                Discovery completed in {formatElapsedMS(discovery.durationMs)}
              </p>
            </div>

            <div className="discDetailsFrame">
              <div className="discIdentity">
                <div className="discIdentityRow">
                  <span className="discIdentityLabel">
                    Disc Title
                  </span>
                  <span className="discIdentityValue">
                    {discovery.disc.Title || '—'}
                  </span>
                </div>

                <div className="discIdentityRow">
                  <span className="discIdentityLabel">
                    Disc Label
                  </span>
                  <span className="discIdentityValue discLabelValue">
                    {discovery.disc.Label || '—'}
                  </span>
                </div>
              </div>

              <div className="discMetaStrip">
                <span className="discMetaBadge active">
                  {formatGiB(
                    discovery.disc.SizeBytes,
                  )}
                </span>

                <span className="discMetaBadge active">
                  {isISO(sourcePath)
                    ? 'ISO'
                    : 'BDMV'}
                </span>

                {features.map(([label, value]) => (
                  <span
                    key={String(label)}
                    className={
                      value
                        ? 'discMetaBadge feature active'
                        : 'discMetaBadge feature inactive'
                    }
                  >
                    {label}
                  </span>
                ))}
              </div>
            </div>
          </section>

          <section className="panel">
            <div className="sectionHeading sectionTitleRow">
              <h2>Playlists</h2>
              <p>
                {discovery.validPlaylistCount}{' '}
                available ·{' '}
                {discovery.filteredPlaylistCount}{' '}
                filtered
              </p>
            </div>

            <div className="playlistTableWrap">
              <table className="playlistTable">
                <thead>
                  <tr>
                    <th>
                      <button
                        type="button"
                        className="sortHeader"
                        onClick={() =>
                          changePlaylistSort(
                            'playlist',
                          )
                        }
                      >
                        Playlist{' '}
                        <span>
                          {playlistSortSymbol(
                            'playlist',
                          )}
                        </span>
                      </button>
                    </th>

                    <th>
                      <button
                        type="button"
                        className="sortHeader"
                        onClick={() =>
                          changePlaylistSort(
                            'duration',
                          )
                        }
                      >
                        Duration{' '}
                        <span>
                          {playlistSortSymbol(
                            'duration',
                          )}
                        </span>
                      </button>
                    </th>

                    <th>
                      <button
                        type="button"
                        className="sortHeader"
                        onClick={() =>
                          changePlaylistSort('size')
                        }
                      >
                        Size{' '}
                        <span>
                          {playlistSortSymbol('size')}
                        </span>
                      </button>
                    </th>

                    <th>
                      <button
                        type="button"
                        className="sortHeader"
                        onClick={() =>
                          changePlaylistSort(
                            'bitrate',
                          )
                        }
                      >
                        Total Bitrate{' '}
                        <span>
                          {playlistSortSymbol(
                            'bitrate',
                          )}
                        </span>
                      </button>
                    </th>

                    <th>
                      <button
                        type="button"
                        className="sortHeader"
                        onClick={() =>
                          changePlaylistSort(
                            'status',
                          )
                        }
                      >
                        Status{' '}
                        <span>
                          {playlistSortSymbol(
                            'status',
                          )}
                        </span>
                      </button>
                    </th>
                  </tr>
                </thead>

                <tbody>
                  {sortedPlaylists.map(
                    (playlist) => {
                      const main =
                        discovery.mainPlaylist?.Name ===
                        playlist.Name

                      return (
                        <tr
                          key={playlist.Name}
                          className={[
                            playlist.IsValid
                              ? ''
                              : 'filteredPlaylist',
                            scanMode === 'selected'
                              ? 'selectablePlaylist'
                              : '',
                            main ? 'mainPlaylist' : '',
                            (
                              (scanMode === 'main' && main) ||
                              (scanMode === 'all') ||
                              (scanMode === 'selected' &&
                                selectedPlaylistNames.includes(playlist.Name))
                            )
                              ? 'selectedPlaylist'
                              : '',
                          ]
                            .filter(Boolean)
                            .join(' ')}
                          onClick={() => {
                            if (
                              scanMode === 'selected' &&
                              !scanActive
                            ) {
                              setSelectedPlaylistNames((current) =>
                                current.includes(playlist.Name)
                                  ? current.filter(
                                      (name) => name !== playlist.Name,
                                    )
                                  : [...current, playlist.Name],
                              )
                            }
                          }}
                        >
                          <td className="playlistName">
                            {playlist.Name}

                            {main && (
                              <span className="mainBadge">
                                Main
                              </span>
                            )}

                            {(
                              (scanMode === 'main' && main) ||
                              scanMode === 'all' ||
                              (scanMode === 'selected' &&
                                selectedPlaylistNames.includes(playlist.Name))
                            ) && (
                              <span className="selectedBadge">
                                Selected
                              </span>
                            )}
                          </td>

                          <td>
                            {formatDuration(
                              playlist.LengthSeconds,
                            )}
                          </td>

                          <td>
                            {formatGiB(
                              playlist.SizeBytes,
                            )}
                          </td>

                          <td>
                            {formatMbps(
                              playlist.TotalBitrateBps,
                            )}
                          </td>

                          <td>
                            <span
                              className={
                                playlist.IsValid
                                  ? 'playlistStatus available'
                                  : 'playlistStatus filtered'
                              }
                            >
                              {playlist.IsValid
                                ? 'Available'
                                : 'Filtered'}
                            </span>
                          </td>
                        </tr>
                      )
                    },
                  )}
                </tbody>
              </table>
            </div>
          </section>

          <section className="panel scanPanel">
            <div className="scanPanelHeader">
              <div className="scanPrimary">
                <div className="scanPrimaryTopRow">
                  <button
                    type="button"
                    className={
                      scanActive
                        ? 'scanButton scanPrimaryButton cancelling'
                        : 'scanButton scanPrimaryButton'
                    }
                    disabled={
                      scanActive
                        ? scanJob?.status === 'cancelling'
                        : scanMode === 'selected' &&
                          selectedPlaylistNames.length === 0
                    }
                    onClick={() =>
                      scanActive
                        ? void cancelScan()
                        : void startScan()
                    }
                  >
                    {scanActive
                      ? scanJob?.status === 'cancelling'
                        ? 'Cancelling…'
                        : 'Cancel'
                      : scanCompleted
                        ? 'Scan Again'
                        : 'Scan Bitrates'}
                  </button>

                  <span className="scanPrimaryHelper">
                    Measure stream bitrates and generate the detailed BDInfo report
                  </span>
                </div>

                <div className="scanPrimaryActions">
                  <label
                    className={
                      notificationsSupported
                        ? 'scanNotifyOption'
                        : 'scanNotifyOption disabled'
                    }
                    title={
                      notificationsSupported
                        ? 'Show a browser notification when the scan finishes'
                        : 'Browser notifications require HTTPS'
                    }
                  >
                    <input
                      type="checkbox"
                      checked={scanNotifications}
                      disabled={!notificationsSupported}
                      onChange={() =>
                        void toggleScanNotifications()
                      }
                    />

                    <span>Notify when done</span>
                  </label>


                </div>
              </div>

              <div className="scanScopeWithHelp">
                <div className="scanScope">
                  <button
                    type="button"
                    className={
                      scanMode === 'main'
                        ? 'scanScopeButton active'
                        : 'scanScopeButton'
                    }
                    disabled={scanActive}
                    onClick={() => setScanMode('main')}
                  >
                    Main
                  </button>

                  <button
                    type="button"
                    className={
                      scanMode === 'selected'
                        ? 'scanScopeButton active'
                        : 'scanScopeButton'
                    }
                    disabled={scanActive}
                    onClick={() => setScanMode('selected')}
                  >
                    Select
                  </button>

                  <button
                    type="button"
                    className={
                      scanMode === 'all'
                        ? 'scanScopeButton active'
                        : 'scanScopeButton'
                    }
                    disabled={scanActive}
                    onClick={() => setScanMode('all')}
                  >
                    All
                  </button>
                </div>

                <button
                  type="button"
                  className="scanHelp"
                  aria-label="About scan modes"
                >
                  ?

                  <span
                    className="scanHelpTooltip"
                    role="tooltip"
                  >
                    <span>
                      <strong>Main</strong>
                      Scan only the detected main playlist.
                    </span>

                    <span>
                      <strong>Select</strong>
                      Choose specific playlists to scan.
                    </span>

                    <span>
                      <strong>All</strong>
                      Scan every playlist, including filtered playlists.
                    </span>
                  </span>
                </button>

                
              </div>
            </div>

            {scanMode === 'selected' && (
              <div className="selectedScanPlaylist">
                <span>
                  {selectedPlaylistNames.length} selected
                </span>

                <strong>
                  {selectedPlaylistNames.length > 0
                    ? selectedPlaylistNames.join(', ')
                    : 'None'}
                </strong>

                <span className="selectedScanHint">
                  Click playlist rows above to toggle selection
                </span>
              </div>
            )}

            {scanJob && (
              <div className="scanProgressArea">
                <div className="scanProgressTop">
                  <div>
                    <span className="scanProgressLabel">
                      {scanJob.status === 'completed' ? (
                        `Scan completed in ${formatElapsedMS(
                          scanJob.elapsedMs,
                        )}`
                      ) : scanJob.status === 'cancelled' ? (
                        'Scan cancelled'
                      ) : scanJob.status === 'cancelling' ? (
                        'Cancelling…'
                      ) : currentPlaylist ? (
                        <>
                          <span>
                            {currentPlaylist} ·{' '}
                            {currentPlaylistPercent.toFixed(0)}%
                          </span>

                          {showOverallProgress && (
                            <>
                              <span className="progressSeparator">
                                |
                              </span>

                              <span>
                                Overall {scanPercent.toFixed(0)}%
                              </span>
                            </>
                          )}
                        </>
                      ) : scanJob?.request.mode === 'all' ? (
                        `Overall ${scanPercent.toFixed(0)}%`
                      ) : (
                        <>
                          {formatScanStage(
                            scanJob.progress?.stage,
                          )}
                          {' · '}
                          {scanPercent.toFixed(0)}%
                        </>
                      )}
                    </span>
                  </div>

                  {scanJob.status !== 'completed' && (
                    <span className="scanElapsed">
                      {formatElapsedMS(scanJob.elapsedMs)}
                    </span>
                  )}
                </div>

                <div className="scanProgressTrack">
                  <div
                    className="scanProgressFill"
                    style={{
                      width: `${scanPercent}%`,
                    }}
                  />
                </div>

                {scanJob.progress &&
                  scanJob.progress.totalBytes > 0 && (
                    <div className="scanProgressStats">
                      <span>
                        {formatProgressBytes(
                          scanJob.progress.processedBytes,
                        )}
                        {' / '}
                        {formatProgressBytes(
                          scanJob.progress.totalBytes,
                        )}
                      </span>

                      <span>
                        {formatReadSpeed(
                          averageReadSpeed(
                            scanJob.progress.processedBytes,
                            scanJob.elapsedMs,
                          ),
                        )}
                      </span>

                      {formatETA(
                        scanJob.progress.processedBytes,
                        scanJob.progress.totalBytes,
                        scanJob.elapsedMs,
                      ) && (
                        <span>
                          ETA{' '}
                          {formatETA(
                            scanJob.progress.processedBytes,
                            scanJob.progress.totalBytes,
                            scanJob.elapsedMs,
                          )}
                        </span>
                      )}
                    </div>
                  )}
              </div>
            )}

            {scanError && (
              <div className="scanError">
                {scanError}
              </div>
            )}


          </section>

          {scanCompleted && scanJob && (
            <section className="panel reportPanel">
              <div className="reportHeader">
                <div className="reportTitleRow">
                  <h2>Scan Report</h2>
                  <p>
                    View the completed BDInfo report without rescanning
                  </p>
                </div>

                <div className="reportControls">
                  <div className="reportControlGroup">
                    <span className="reportControlLabel">Scope</span>

                    <select
                      className="reportScopeSelect"
                      value={reportPlaylist}
                      aria-label="Report scope"
                      onChange={(event) =>
                        setReportPlaylist(event.target.value)
                      }
                    >
                      <option value="">Complete</option>

                      {reportPlaylists.map((playlist) => (
                        <option
                          key={playlist}
                          value={playlist}
                        >
                          {playlist}
                        </option>
                      ))}
                    </select>
                  </div>

                  <div className="reportControlDivider" />

                  <div className="reportControlGroup">
                    <span className="reportControlLabel">View</span>

                    <div className="reportViewSelector">
                      <button
                        type="button"
                        className={
                          reportView === 'standard'
                            ? 'reportViewButton active'
                            : 'reportViewButton'
                        }
                        onClick={() =>
                          setReportView('standard')
                        }
                      >
                        Standard
                      </button>

                      <button
                        type="button"
                        className={
                          reportView === 'summary'
                            ? 'reportViewButton active'
                            : 'reportViewButton'
                        }
                        onClick={() =>
                          setReportView('summary')
                        }
                      >
                        Summary
                      </button>

                      <button
                        type="button"
                        className={
                          reportView === 'forums'
                            ? 'reportViewButton active'
                            : 'reportViewButton'
                        }
                        onClick={() =>
                          setReportView('forums')
                        }
                      >
                        Forums
                      </button>
                    </div>
                  </div>

                  <div className="reportControlDivider" />

                  <div className="reportControlGroup">
                    <span className="reportControlLabel">Actions</span>

                    <div className="reportActionGroup">
                      <button
                        type="button"
                        className="reportCopyButton"
                        disabled={
                          !scanReport || reportLoading
                        }
                        onClick={() =>
                          void copyScanReport()
                        }
                      >
                        {reportCopied
                          ? 'Copied'
                          : 'Copy'}
                      </button>

                      <select
                        className="reportFormatSelect"
                        value={reportExportFormat}
                        aria-label="Export format"
                        onChange={(event) =>
                          setReportExportFormat(
                            event.target.value as ReportExportFormat,
                          )
                        }
                      >
                        <option value="txt">TXT</option>
                        <option value="nfo">NFO</option>
                      </select>

                      <button
                        type="button"
                        className="reportExportButton"
                        disabled={
                          !scanReport || reportLoading
                        }
                        onClick={exportScanReport}
                      >
                        Export
                      </button>
                    </div>
                  </div>
                </div>
              </div>

              {reportError && (
                <div className="reportError">
                  {reportError}
                </div>
              )}

              <div className="reportContentWrap">
                {scanReport && (
                  <pre
                    className={
                      reportLoading
                        ? 'reportText loading'
                        : 'reportText'
                    }
                  >
                    {scanReport.split('\n').map((line, index) => (
                      <span
                        key={index}
                        className={reportLineClass(line)}
                      >
                        {line}
                        {'\n'}
                      </span>
                    ))}
                  </pre>
                )}

                {reportLoading && (
                  <div className="reportLoadingBadge">
                    Loading…
                  </div>
                )}
              </div>
            </section>
          )}
        </>
      )}

      {browserOpen && (
        <div
          className="modalBackdrop"
          onMouseDown={(event) => {
            if (event.target === event.currentTarget) {
              setBrowserOpen(false)
            }
          }}
        >
          <div className="browserModal">
            <header className="browserModalHeader">
              <div>
                <h2>Select source</h2>
                <p>{browsePath || 'Source locations'}</p>
              </div>

              <button
                type="button"
                className="secondaryButton"
                onClick={() => setBrowserOpen(false)}
              >
                Close
              </button>
            </header>

            <div className="browserLayout">
              <aside className="browserSidebar">
                <section className="sidebarSection">
                  <h3>Locations</h3>

                  <div className="sidebarItems">
                    {locations.map((location) => {
                      const root =
                        location.path.replace(
                          /\/+$/,
                          '',
                        )

                      const active =
                        browsePath ===
                          location.path ||
                        browsePath.startsWith(
                          `${root}/`,
                        )

                      return (
                        <button
                          key={location.id}
                          type="button"
                          className={
                            active
                              ? 'sidebarItem active'
                              : 'sidebarItem'
                          }
                          onClick={() =>
                            void loadBrowser(
                              location.path,
                            )
                          }
                        >
                          {location.name}
                        </button>
                      )
                    })}
                  </div>
                </section>

                <section
                  className={
                    favoriteDropActive
                      ? 'sidebarSection favoritesSection dropActive'
                      : 'sidebarSection favoritesSection'
                  }
                  onDragEnter={(event) => {
                    event.preventDefault()
                    setFavoriteDropActive(true)
                  }}
                  onDragOver={(event) => {
                    event.preventDefault()
                    setFavoriteDropActive(true)
                    event.dataTransfer.dropEffect =
                      'copy'
                  }}
                  onDragLeave={() =>
                    setFavoriteDropActive(false)
                  }
                  onDrop={(event) =>
                    void handleFavoriteDrop(event)
                  }
                >
                  <h3>Favorites</h3>

                  <div className="sidebarItems">
                    {favorites.map((favorite) => (
                      <div
                        key={favorite.id}
                        className="favoriteSidebarRow"
                      >
                        <button
                          type="button"
                          className="sidebarItem favoriteSidebarItem"
                          title={favorite.path}
                          onClick={() =>
                            void openFavorite(
                              favorite,
                            )
                          }
                        >
                          ★ {favorite.name}
                        </button>

                        <button
                          type="button"
                          className="favoriteRemove"
                          title="Remove favorite"
                          onClick={() =>
                            void removeFavorite(
                              favorite,
                            )
                          }
                        >
                          ×
                        </button>
                      </div>
                    ))}

                    {favorites.length === 0 && (
                      <div className="sidebarEmpty">
                        Drop folders here or use ☆
                      </div>
                    )}
                  </div>
                </section>

                {favoritesError && (
                  <div className="sidebarError">
                    {favoritesError}
                  </div>
                )}
              </aside>

              <div className="browserMain">
                <div className="browserToolbar">
                  <button
                    type="button"
                    className="secondaryButton parentButton"
                    disabled={!browseParent}
                    onClick={() =>
                      browseParent &&
                      void loadBrowser(
                        browseParent,
                      )
                    }
                  >
                    ↑
                  </button>

                  <input
                    type="search"
                    value={search}
                    placeholder="Search this folder…"
                    onChange={(event) =>
                      setSearch(
                        event.target.value,
                      )
                    }
                  />
                </div>

                {browseError && (
                  <div className="browserError">
                    {browseError}
                  </div>
                )}

                <div className="browserTableViewport">
                  <table
                    className="browserTable"
                    style={{
                      width: browserTableWidth,
                      minWidth: browserTableWidth,
                    }}
                  >
                    <colgroup>
                      <col style={{ width: 44 }} />
                      <col
                        style={{
                          width:
                            columnWidths.name,
                        }}
                      />
                      <col
                        style={{
                          width:
                            columnWidths.modified,
                        }}
                      />
                      <col
                        style={{
                          width:
                            columnWidths.kind,
                        }}
                      />
                      <col
                        style={{
                          width:
                            columnWidths.size,
                        }}
                      />
                      <col style={{ width: 96 }} />
                    </colgroup>

                    <thead>
                      <tr>
                        <th
                          className="favoriteColumnHeader"
                          title="Favorite"
                        >
                          ★
                        </th>

                        <th>
                          <button
                            type="button"
                            className="sortHeader"
                            onClick={() =>
                              changeBrowserSort(
                                'name',
                              )
                            }
                          >
                            Name{' '}
                            <span>
                              {browserSortSymbol(
                                'name',
                              )}
                            </span>
                          </button>

                          <span
                            className="columnResizeHandle"
                            onPointerDown={(
                              event,
                            ) =>
                              startColumnResize(
                                'name',
                                event,
                              )
                            }
                          />
                        </th>

                        <th>
                          <button
                            type="button"
                            className="sortHeader"
                            onClick={() =>
                              changeBrowserSort(
                                'modified',
                              )
                            }
                          >
                            Date Modified{' '}
                            <span>
                              {browserSortSymbol(
                                'modified',
                              )}
                            </span>
                          </button>

                          <span
                            className="columnResizeHandle"
                            onPointerDown={(
                              event,
                            ) =>
                              startColumnResize(
                                'modified',
                                event,
                              )
                            }
                          />
                        </th>

                        <th>
                          <button
                            type="button"
                            className="sortHeader"
                            onClick={() =>
                              changeBrowserSort(
                                'kind',
                              )
                            }
                          >
                            Kind{' '}
                            <span>
                              {browserSortSymbol(
                                'kind',
                              )}
                            </span>
                          </button>

                          <span
                            className="columnResizeHandle"
                            onPointerDown={(
                              event,
                            ) =>
                              startColumnResize(
                                'kind',
                                event,
                              )
                            }
                          />
                        </th>

                        <th>
                          <button
                            type="button"
                            className="sortHeader"
                            onClick={() =>
                              changeBrowserSort(
                                'size',
                              )
                            }
                          >
                            Size{' '}
                            <span>
                              {browserSortSymbol(
                                'size',
                              )}
                            </span>
                          </button>

                          <span
                            className="columnResizeHandle"
                            onPointerDown={(
                              event,
                            ) =>
                              startColumnResize(
                                'size',
                                event,
                              )
                            }
                          />
                        </th>

                        <th
                          className="browserSelectHeader"
                          aria-label="Select source"
                        />
                      </tr>
                    </thead>

                    <tbody>
                      {browseLoading && (
                        <tr>
                          <td
                            colSpan={6}
                            className="browserMessage"
                          >
                            Loading…
                          </td>
                        </tr>
                      )}

                      {!browseLoading &&
                        sortedBrowserEntries.map(
                          (entry) => {
                            const favorite =
                              favoriteForPath(
                                entry.path,
                              )

                            const directory =
                              !isISO(entry.path) &&
                              entry.type.toLowerCase() !==
                                'iso'

                            return (
                              <tr
                                key={entry.path}
                                className="browserRow"
                                draggable
                                onDragStart={(
                                  event,
                                ) => {
                                  event.dataTransfer.effectAllowed =
                                    'copy'

                                  event.dataTransfer.setData(
                                    'application/x-bdinfo-entry',
                                    JSON.stringify(
                                      {
                                        name:
                                          entry.name,
                                        path:
                                          entry.path,
                                      },
                                    ),
                                  )
                                }}
                              >
                                <td className="favoriteCell">
                                  <button
                                    type="button"
                                    className={
                                      favorite
                                        ? 'rowFavorite active'
                                        : 'rowFavorite'
                                    }
                                    title={
                                      favorite
                                        ? 'Remove from favorites'
                                        : 'Add to favorites'
                                    }
                                    onClick={() =>
                                      void toggleFavorite(
                                        entry,
                                      )
                                    }
                                  >
                                    {favorite
                                      ? '★'
                                      : '☆'}
                                  </button>
                                </td>

                                <td className="browserNameCell">
                                  {directory ? (
                                    <button
                                      type="button"
                                      className="browserNameButton"
                                      title={
                                        entry.name
                                      }
                                      onClick={() =>
                                        void loadBrowser(
                                          entry.path,
                                        )
                                      }
                                    >
                                      {
                                        entry.name
                                      }
                                    </button>
                                  ) : (
                                    <span
                                      className="browserFileName"
                                      title={
                                        entry.name
                                      }
                                    >
                                      {
                                        entry.name
                                      }
                                    </span>
                                  )}
                                </td>

                                <td>
                                  {formatModified(
                                    entry.modifiedAt,
                                  )}
                                </td>

                                <td>
                                  {browserKind(
                                    entry,
                                  )}
                                </td>

                                <td>
                                  {browserSize(
                                    entry,
                                  )}
                                </td>

                                <td className="browserSelectCell">
                                  {entry.selectable && (
                                    <button
                                      type="button"
                                      className="selectButton"
                                      onClick={() =>
                                        void selectSource(
                                          entry.path,
                                        )
                                      }
                                    >
                                      Select
                                    </button>
                                  )}
                                </td>
                              </tr>
                            )
                          },
                        )}

                      {!browseLoading &&
                        sortedBrowserEntries.length ===
                          0 && (
                          <tr>
                            <td
                              colSpan={6}
                              className="browserMessage"
                            >
                              No matching items
                            </td>
                          </tr>
                        )}
                    </tbody>
                  </table>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}
    </main>
  )
}

export default App
