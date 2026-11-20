import type { ActivitySeries } from '@/lib/api-types'

const GIB = 1024 ** 3

/** One local hour of counted traffic, in GiB, ready for the stacked bars. */
export interface HourlyTraffic {
  at: number
  key: string
  label: string
  downGiB: number
  upGiB: number
}

function hourKey(at: Date) {
  return `${at.getFullYear()}-${at.getMonth()}-${at.getDate()}-${at.getHours()}`
}

function dayKey(at: Date) {
  return `${at.getFullYear()}-${at.getMonth()}-${at.getDate()}`
}

function clock(hour: number) {
  return `${String(hour).padStart(2, '0')}:00`
}

/**
 * The last `hours` clock hours, summed from half-hour buckets.
 *
 * Activity is stored in half-hour steps, but a day is read in hours: one bar
 * per clock hour is the shape an operator asked for, and summing the pair is
 * what makes the bar's height the hour's total rather than a sample of it.
 * The window can start mid-hour, so grouping by the local hour (instead of
 * pairing adjacent buckets) is what keeps the first and last bars honest.
 */
export function hourlyTraffic(buckets: ActivitySeries['buckets'], hours = 24): HourlyTraffic[] {
  const byHour = new Map<string, HourlyTraffic>()
  for (const bucket of buckets) {
    const at = new Date(bucket.at)
    if (!Number.isFinite(at.getTime())) continue
    const start = new Date(at.getFullYear(), at.getMonth(), at.getDate(), at.getHours())
    const key = hourKey(start)
    const point = byHour.get(key) ?? {
      at: start.getTime(),
      key,
      label: clock(start.getHours()),
      downGiB: 0,
      upGiB: 0,
    }
    point.downGiB += bucket.bytes.down / GIB
    point.upGiB += bucket.bytes.up / GIB
    byHour.set(key, point)
  }
  return [...byHour.values()].sort((a, b) => a.at - b.at).slice(-hours)
}

export interface RhythmDay {
  date: Date
  key: string
  hours: (number | null)[]
  totalGiB: number
}

export interface TrafficRhythm {
  days: RhythmDay[]
  maxGiB: number
  totalGiB: number
  peakDayIndex: number
  peakHour: number
  nowDay: number
  nowHour: number
}

/**
 * Seven local days by twenty-four local hours, in GiB.
 *
 * The activity window is a rolling one, so the grid is anchored to local
 * midnight rather than to the window's start: the reader is looking for "when
 * in the week", and a grid rotated by the current clock would make every row
 * start at a different time. Cells after the current hour are `null` (not
 * zero): the day has not happened yet.
 */
export function trafficRhythm(buckets: ActivitySeries['buckets'], now = new Date(), days = 7): TrafficRhythm {
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const start = new Date(today)
  start.setDate(start.getDate() - (days - 1))

  const rows = new Map<string, RhythmDay>()
  const ordered: RhythmDay[] = []
  for (let index = 0; index < days; index += 1) {
    const date = new Date(start)
    date.setDate(start.getDate() + index)
    const row: RhythmDay = {
      date,
      key: dayKey(date),
      hours: Array.from({ length: 24 }, () => 0),
      totalGiB: 0,
    }
    rows.set(row.key, row)
    ordered.push(row)
  }

  for (const bucket of buckets) {
    const at = new Date(bucket.at)
    if (!Number.isFinite(at.getTime())) continue
    const row = rows.get(dayKey(at))
    if (!row) continue
    const value = (bucket.bytes.down + bucket.bytes.up) / GIB
    row.hours[at.getHours()]! += value
    row.totalGiB += value
  }

  // Today's future hours are a gap, not a quiet hour. Only today has them:
  // every earlier row is complete.
  const todayRow = ordered[days - 1]
  const nowHour = now.getHours()
  for (let hour = nowHour + 1; hour < 24; hour += 1) todayRow.hours[hour] = null

  let maxGiB = 0
  let peakDayIndex = days - 1
  let peakHour = 0
  for (let index = 0; index < ordered.length; index += 1) {
    const row = ordered[index]
    if (row.totalGiB > (ordered[peakDayIndex]?.totalGiB ?? -1)) peakDayIndex = index
    for (let hour = 0; hour < 24; hour += 1) {
      const value = row.hours[hour]
      if (value != null && value > maxGiB) {
        maxGiB = value
        peakHour = hour
      }
    }
  }

  return {
    days: ordered,
    maxGiB,
    totalGiB: ordered.reduce((sum, row) => sum + row.totalGiB, 0),
    peakDayIndex,
    peakHour,
    nowDay: days - 1,
    nowHour,
  }
}
