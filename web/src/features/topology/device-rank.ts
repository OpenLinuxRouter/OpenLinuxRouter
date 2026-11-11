export interface ClientRank {
  rate: number
  lastSeen: number
}

/** A moving device precedes quiet ones; quiet devices use last-heard time. */
export function compareClientRanks(a: ClientRank, b: ClientRank): number {
  return Number(b.rate > 0) - Number(a.rate > 0) ||
    (a.rate > 0 ? b.rate - a.rate : b.lastSeen - a.lastSeen)
}
