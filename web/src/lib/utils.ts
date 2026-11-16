import { clsx, type ClassValue } from "clsx"
import { extendTailwindMerge } from "tailwind-merge"

// `text-meta` is the 13px role from index.css. Without telling tailwind-merge it
// is a font size, `cn('text-meta', 'text-muted-foreground')` reads it as a
// colour and drops it.
const twMerge = extendTailwindMerge({
  extend: { theme: { text: ['meta'] } },
})

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * Powers of 1024 with the short suffixes, matching every other tool on the box.
 *
 * Shared rather than copied: two screens now show byte counts from the same
 * endpoint — the gateway's per-device list and the overview's total — and a
 * second convention would make two numbers about the same traffic disagree.
 */
export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  const units = ['KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  let value = n / 1024
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i++
  }
  return `${value.toFixed(1)} ${units[i]}`
}

/**
 * A rate, given in bytes per second, written the way a router is read: in
 * bits. Link speeds and every provider's plan are quoted in Mbps, so a byte
 * rate beside them would make the operator do the ×8 in their head.
 */
export function formatRate(bytesPerSecond: number): string {
  const bits = bytesPerSecond * 8
  if (bits < 1000) return `${Math.round(bits)} bps`
  const units = ['kbps', 'Mbps', 'Gbps', 'Tbps']
  let value = bits / 1000
  let i = 0
  while (value >= 1000 && i < units.length - 1) {
    value /= 1000
    i++
  }
  return `${value < 10 ? value.toFixed(1) : Math.round(value)} ${units[i]}`
}

/**
 * How long ago, in the fewest characters that still read: "now", "4m ago",
 * "2h ago", "3d ago". For a row with no room — a precise time belongs in a
 * tooltip beside it.
 */
export function formatAgo(iso: string, now = Date.now()): string {
  const minutes = Math.floor((now - Date.parse(iso)) / 60_000)
  if (!(minutes >= 1)) return 'now'
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 48) return `${hours}h ago`
  return `${Math.floor(hours / 24)}d ago`
}

/**
 * formatRate for a row with no room: two significant figures and a one-letter
 * unit, "8.8K" and "415M" rather than "8.8 kbps" and "415 Mbps". The row
 * already says these are rates by its arrows, and at a glance the digits that
 * matter are the first two and the letter — the rest was width taken from the
 * device's name. Below a kilobit, show the actual rounded bit rate with a B
 * suffix rather than claiming a slow connection is already moving at 1K.
 */
export function formatRateCompact(bytesPerSecond: number): string {
  const bits = bytesPerSecond * 8
  if (bits < 0.5) return '0B'
  if (bits < 1000) return `${Math.round(bits)}B`
  const units = ['K', 'M', 'G', 'T']
  let value = bits / 1000
  let i = 0
  while (value >= 999.5 && i < units.length - 1) {
    value /= 1000
    i++
  }
  return `${value < 9.95 ? value.toFixed(1) : Math.round(value)}${units[i]}`
}
