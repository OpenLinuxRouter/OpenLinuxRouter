/**
 * Axis ticks a person can read.
 *
 * Recharts picks its own ticks from the data's domain, and a formatter then
 * decides what each one says. The obvious formatter for a GiB axis rounds to a
 * whole number, and neighbouring ticks round to the same string: a plot that
 * reaches 2.4 labels 0.6, 1.2 and 1.8 as "1" and "1", "2" and "2", so the axis
 * reads 0, 1, 1, 2, 2 — three different positions wearing two labels, and a
 * reader who trusts them is off by 40%.
 *
 * So the plots choose their own ticks: the smallest round step from one ladder
 * that covers the data, spaced evenly from zero, and however many decimals that
 * step needs. A tick is a position, and every position gets its own label.
 */

/** Round steps. 1.5 earns its place: it turns 41 ms into 0/15/30/45. */
const LADDER = [1, 1.5, 2, 2.5, 5, 10]

export interface AxisScale {
  /** Positions in the data's own units, ascending from zero. */
  ticks: number[]
  /** Upper bound of the axis, at or above the data's maximum. */
  top: number
  /** Decimal places the labels need to stay distinct. */
  decimals: number
}

/**
 * Ticks for data that runs from zero to `max`, aiming at `count` intervals.
 *
 * A series that is empty or flat still gets two ticks and a top of 1, because
 * an axis with no scale is a picture of nothing; `max` of zero is a real state
 * (a fresh router with nothing counted yet) and the plot should say so calmly
 * instead of collapsing to a hairline.
 */
export function axisScale(max: number, count = 5): AxisScale {
  if (!Number.isFinite(max) || max <= 0) return { ticks: [0, 1], top: 1, decimals: 0 }

  const raw = max / Math.max(1, count)
  const magnitude = 10 ** Math.floor(Math.log10(raw))
  const step = LADDER.map((share) => share * magnitude).find((candidate) => candidate >= raw) ?? 10 * magnitude

  // A step of 0.5 needs one decimal, 0.25 needs two, 5 needs none.
  const decimals = Number.isInteger(step) ? 0 : Number.isInteger(step * 10) ? 1 : 2
  const intervals = Math.ceil(max / step - 1e-9)
  const ticks = Array.from({ length: intervals + 1 }, (_, index) => Number((index * step).toFixed(decimals)))

  return { ticks, top: Number((intervals * step).toFixed(decimals)), decimals }
}

/** One tick label, in the precision the step it belongs to needs. */
export function formatTick(value: number, decimals: number): string {
  const fixed = value.toFixed(decimals)
  return decimals === 0 ? fixed : fixed.replace(/\.?0+$/, '')
}
