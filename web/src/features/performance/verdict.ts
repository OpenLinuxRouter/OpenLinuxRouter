/**
 * The sentence the evidence supports, decided from numbers only.
 *
 * The page exists to answer one question — is this the line or the router? —
 * and a verdict written per render is a verdict that drifts. The thresholds are
 * named here so the copy and the chart cannot disagree about what "near" means.
 *
 * These are judgement calls, so they are published with the answer; the page
 * labels every capacity-derived figure as inferred.
 */
export const NEAR_CAPACITY = 0.8
export const BUSY_CPU = 70
export const IDLE_CPU = 20

/**
 * Drops per second that mean the router is losing packets rather than merely
 * counting a few.
 *
 * Interface drop counters tick over on any busy link — a gigabit carrying
 * torrents drops on the order of one packet a second, which is noise and not a
 * bottleneck. A verdict that fires on the first drop would call every busy
 * network broken, so the threshold sits well above that floor (roughly a
 * twentieth of a percent of a gigabit's packet rate) and is published with the
 * other judgement calls.
 */
export const DROP_BUSY = 50

export type CapacityState = 'loading' | 'missing' | 'measured' | 'warming'

export type VerdictCode =
  | 'checking'
  | 'no-capacity'
  | 'no-throughput'
  | 'no-utilisation'
  | 'upload-near-capacity'
  | 'download-near-capacity'
  | 'router-dropping'
  | 'link-free-router-busy'
  | 'link-free-router-idle'
  | 'link-free'

export interface Verdict {
  code: VerdictCode
  /** The capacity baseline is still warming, so the answer may move. */
  provisional: boolean
}

export interface VerdictInput {
  capacity: CapacityState
  /** Current throughput over capacity, 0–1 (or above on an overshoot). */
  downloadUtil?: number
  uploadUtil?: number
  cpuPercent?: number
  dropsPerSecond?: number
  /** At least one direction has a current rate. */
  throughput: boolean
}

export function verdict(input: VerdictInput): Verdict {
  const provisional = input.capacity === 'warming'

  if (input.capacity === 'loading') return { code: 'checking', provisional: false }
  if (input.capacity === 'missing') return { code: 'no-capacity', provisional: false }
  if (!input.throughput) return { code: 'no-throughput', provisional }

  const download = input.downloadUtil
  const upload = input.uploadUtil
  const busiest = Math.max(download ?? -1, upload ?? -1)

  // No usable capacity for the direction that did report. Claiming "not
  // saturated" here would be a claim with no denominator.
  if (busiest < 0) return { code: 'no-utilisation', provisional }

  if (busiest >= NEAR_CAPACITY) {
    return {
      code: (upload ?? -1) >= (download ?? -1) ? 'upload-near-capacity' : 'download-near-capacity',
      provisional,
    }
  }

  // A dropping router answers the question even while the line has headroom.
  // Below the busy threshold the counters are ticking, not bottlenecking.
  if (input.dropsPerSecond != null && input.dropsPerSecond >= DROP_BUSY) {
    return { code: 'router-dropping', provisional }
  }

  if (input.cpuPercent != null && input.cpuPercent >= BUSY_CPU) {
    return { code: 'link-free-router-busy', provisional }
  }

  if (input.cpuPercent != null && input.cpuPercent < IDLE_CPU) {
    return { code: 'link-free-router-idle', provisional }
  }

  // CPU unknown or between the two bands: the line is still the one thing the
  // numbers support, so the verdict says only that.
  return { code: 'link-free', provisional }
}
