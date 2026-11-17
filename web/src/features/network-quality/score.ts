/**
 * A 0–100 score as it is read: one decimal, and no trailing `.0`.
 *
 * Snapshots keep their scores unrounded so the arithmetic stays exact
 * (docs/network-quality.md §8.3), which leaves rounding to the screens that
 * show one — and to a single helper, so the Overview's readout and the quality
 * ring cannot disagree about the same number.
 */
export const scoreText = (value: number) => Number(value.toFixed(1)).toString()
