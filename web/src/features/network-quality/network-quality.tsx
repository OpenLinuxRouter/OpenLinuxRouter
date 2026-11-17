import { t, useLocale } from '@/lib/i18n'
import { useState } from 'react'
import { ChevronRight, CircleHelp, LoaderCircle, RotateCw } from 'lucide-react'

import { PageHeading } from '@/components/layout/page-heading'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Heading } from '@/components/ui/heading'
import { MetricValue } from '@/components/ui/metric-value'
import { PropertyRow, TechnicalValue } from '@/components/ui/property-row'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { StatusPill } from '@/components/ui/status'
import { cn } from '@/lib/utils'
import { filteringVerdict, mappingVerdict, natTypeLabel } from '@/features/tools/nat'
import { categoryEvidence, serviceEvidence, type CategoryEvidence, type ServiceEvidence } from './measurements'
import { useNetworkQuality, useQualityAction } from './queries'
import type { QualityAction } from './queries'
import { scoreText } from './score'
import type { QualityCategory, QualitySample, QualitySnapshot, QualityScope } from './types'

const categoryLabel = (label: string) => label === 'Wi-Fi' ? 'Wireless' : label

type GradeTone = 'success' | 'warning' | 'destructive'

/** The grade bands are a judgement, so they wear the status pill. */
const gradeStatusTone = { success: 'ok', warning: 'warn', destructive: 'fault' } as const

// Display bands only: they name the same 0–100 reference score and never change
// the calculation in docs/network-quality.md.
const grades: { min: number; label: string; range: string; variant: GradeTone }[] = [
  { min: 90, label: 'Excellent', range: '90–100', variant: 'success' },
  { min: 75, label: 'Good', range: '75–89', variant: 'success' },
  { min: 60, label: 'Fair', range: '60–74', variant: 'warning' },
  { min: 0, label: 'Poor', range: 'below 60', variant: 'destructive' },
]

function gradeFor(score: number) {
  return grades.find(grade => score >= grade.min) ?? grades[grades.length - 1]
}

// The ring and the bars carry the grade; the score itself stays foreground so a
// page of amber scores does not read as a page of warnings.
const toneStroke: Record<GradeTone, string> = {
  success: 'text-success',
  warning: 'text-warning',
  destructive: 'text-destructive',
}

const toneFill: Record<GradeTone, string> = {
  success: 'bg-success',
  warning: 'bg-warning',
  destructive: 'bg-destructive',
}

const phaseText: Record<string, string> = {
  responses: 'Checking service responses',
  speed: 'Measuring download and upload',
  nat: 'Checking NAT type',
  wifi: 'Reading wireless links',
  '': 'Starting…',
}

function categoryScope(id: string): Exclude<QualityScope, 'all' | 'target'> | null {
  switch (id) {
    case 'speed': return 'speed'
    case 'wifi': return 'wifi'
    case 'latency':
    case 'stability':
    case 'functionality': return 'responses'
    default: return null
  }
}

export function NetworkQualityPage() {
  const query = useNetworkQuality()
  // A failed read must not keep a cached healthy result looking current.
  const data = query.isError ? undefined : query.data

  return <div className="space-y-6">
    <QualityReport data={data} readError={query.isError} loading={query.isPending} retry={() => query.refetch()} />
  </div>
}

function formatWhen(value: string, locale: string) {
  return new Date(value).toLocaleString(locale, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })
}

function targetName(data: QualitySnapshot | undefined, id: string | undefined) {
  if (!id) return t('service')
  return data?.targets.find(target => target.id === id)?.name ?? id
}

function runningLabel(data: QualitySnapshot | undefined) {
  switch (data?.running_scope) {
    case 'responses': return t('Updating service tests')
    case 'speed': return t('Updating the transfer test')
    case 'nat': return t('Updating NAT observations')
    case 'wifi': return t('Updating wireless links')
    case 'target': return t('Updating {target} tests', { target: targetName(data, data.running_target) })
    default: return t('Testing network quality')
  }
}

function heroCopy(data: QualitySnapshot | undefined, loading: boolean | undefined) {
  if (!data && loading) return { title: t('Loading…'), detail: t('Reading the latest assessment.') }
  if (!data || data.state === 'idle') return { title: t('Not tested'), detail: t('Run all tests to score speed, latency, stability and functionality.') }
  if (data.state === 'running') return { title: t('Testing network quality'), detail: t(phaseText[data.phase] ?? 'Starting…') }
  if (data.state === 'cancelled') return { title: t('Assessment cancelled'), detail: t('The assessment did not finish, so no overall score is published. Partial measurements are kept.') }
  if (data.state === 'timed_out') return { title: t('Assessment timed out'), detail: t('The assessment did not finish, so no overall score is published. Partial measurements are kept.') }
  if (data.overall.state === 'stale') return { title: t('Results expired'), detail: t('Run all tests to score the current connection.') }
  if (data.overall.state === 'ready' && data.overall.score != null) return { title: t('Overall score'), detail: '' }
  if (data.overall.available_points > 0) return { title: t('Partial result'), detail: t('No overall score is published for this assessment.') }
  return { title: t('No scored results'), detail: t('No usable measurements. Run all tests to try again.') }
}

function QualityReport({ data, readError, loading, retry }: {
  data?: QualitySnapshot
  readError: boolean
  loading?: boolean
  retry: () => void
}) {
  const locale = useLocale()
  const action = useQualityAction()
  const fullRunning = data?.state === 'running'
  const runningScope: QualityScope | '' = data?.running_scope ?? (fullRunning ? 'all' : '')
  const scopedRunning = !fullRunning && Boolean(data?.running_scope)
  const running = fullRunning || scopedRunning
  const score = data && data.state === 'completed' && data.overall.state === 'ready' ? data.overall.score : null
  const grade = score == null ? null : gradeFor(score)
  const categories = data?.categories ?? []
  const services = serviceEvidence(data)
  const copy = heroCopy(data, loading)
  const status = scopedRunning && data ? runningLabel(data) : copy.detail
  const updated = data?.state === 'completed' ? data.completed_at : null
  const measured = categories.filter(category => category.score != null).length
  const canRerun = data?.state === 'completed'
  const hasRun = Boolean(data?.started_at) && data?.state !== 'idle'

  return <>
    <PageHeading title={t('Network quality')} description={<>
      {data?.wireless
        ? t("From this router's default route and its access points.")
        : t("From this router's default route.")}
      {updated && <> · {t('Updated {time}', { time: formatWhen(updated, locale) })}</>}
    </>} action={
      <div className="flex shrink-0 flex-col items-start gap-1.5 sm:items-end">
        {running
          ? <Button variant="secondary" size="lg" disabled={action.isPending} onClick={() => action.mutate({ cancel: true })}>{t('Cancel test')}</Button>
          : <Button size="lg" disabled={action.isPending || loading} onClick={() => action.mutate({ scope: 'all' })}>
              {action.isPending ? t('Starting…') : hasRun ? t('Run all tests') : t('Run assessment')}
            </Button>}
        <span className="text-xs text-muted-foreground">{t('Bandwidth test included')}</span>
      </div>
    } />
    <p className="sr-only" role="status" aria-live="polite">{[copy.title, status].filter(Boolean).join('. ')}</p>

    {readError && <Alert variant="destructive">
      <AlertTitle>{t('Could not read the assessment.')}</AlertTitle>
      <AlertDescription className="space-y-3">
        <p>{t('Check the router connection, then try again.')}</p>
        <Button variant="secondary" size="sm" onClick={retry}>{t('Retry')}</Button>
      </AlertDescription>
    </Alert>}

    {action.isError && <Alert variant="destructive">
      <AlertTitle>{t('Could not update the assessment')}</AlertTitle>
      <AlertDescription className="break-words">{action.error.message}</AlertDescription>
    </Alert>}

    {data?.speed_error && <Alert variant="destructive">
      <AlertTitle>{t('Bandwidth test failed')}</AlertTitle>
      <AlertDescription className="break-words">{data.speed_error}</AlertDescription>
    </Alert>}

    {/* One column: the score answers the page's question, then the categories
        explain it. A second column held nothing once the transfer moved under
        Speed, and a narrower list would squeeze the sample tables. */}
    <div className="space-y-4">
      <ScorePanel
        score={score}
        grade={grade}
        measured={measured}
        total={categories.length}
        coverage={data?.overall.coverage_percent ?? 0}
        copy={copy}
        busy={fullRunning || Boolean(loading && !data)}
      />
      {data || readError
        ? <CategoryList
            categories={categories}
            data={data}
            services={services}
            hasRun={hasRun}
            runningScope={runningScope}
            runningTarget={data?.running_target}
            anyRunning={running || action.isPending}
            canRerun={canRerun}
            onRerun={next => action.mutate(next)}
          />
        : <Skeleton className="h-72 w-full rounded-xl" />}
    </div>
  </>
}

/**
 * The one number on the report whose label does not say what it counts.
 *
 * The scoring rule itself is a document, not a panel: a page that prints the
 * weights also asks a reader to add the arithmetic up, and every run pays for
 * it. Coverage is different — "48%" beside a score of 69.6 invites the reading
 * that the score is a fraction of a test that did not finish, so the sentence
 * that it does not reduce the score belongs on the number.
 */
function Hint({ label, children }: { label: string; children: React.ReactNode }) {
  return <Tooltip>
    <TooltipTrigger
      aria-label={label}
      className="rounded-full text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
    >
      <CircleHelp className="size-3.5" aria-hidden="true" />
    </TooltipTrigger>
    <TooltipContent className="block max-w-64 leading-relaxed"><span>{children}</span></TooltipContent>
  </Tooltip>
}

function ScorePanel({ score, grade, measured, total, coverage, copy, busy }: {
  score: number | null
  grade: { label: string; range: string; variant: GradeTone } | null
  measured: number
  total: number
  coverage: number
  copy: { title: string; detail: string }
  busy: boolean
}) {
  useLocale()
  return <section aria-labelledby="quality-result" className="panel-surface p-4 sm:p-5">
    <div className="flex flex-wrap items-center gap-x-5 gap-y-4">
      <ScoreRing score={score} busy={busy} />
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex flex-wrap items-center gap-2">
          <Heading as="h2" size="sm" id="quality-result" className="text-muted-foreground">{t('Overall score')}</Heading>
          {grade && <StatusPill tone={gradeStatusTone[grade.variant]}>{t(grade.label)}</StatusPill>}
        </div>
        {score != null
          ? <>
              <p className="text-sm leading-5 text-foreground">{t('{count} of {total} categories scored', { count: measured, total })}</p>
              <p className="text-xs leading-5 text-muted-foreground">{t('{grade} covers {range} of 100', { grade: t(grade!.label), range: grade!.range })}</p>
            </>
          : <>
              <p className="text-sm leading-5 font-medium text-foreground">{copy.title}</p>
              {copy.detail && <p className="text-xs leading-5 text-muted-foreground">{copy.detail}</p>}
            </>}
      </div>
      {score != null && coverage > 0 && <div className="w-full max-w-xs sm:w-64">
        <div className="flex items-baseline justify-between gap-2 text-xs text-muted-foreground">
          <span className="flex items-center gap-1">
            {t('Measured coverage')}
            <Hint label={t('What measured coverage means')}>
              {t('Share of the full assessment this run measured. Unmeasured categories are left out, and the measured ones still share the full 100 points.')}
            </Hint>
          </span>
          <span className="font-medium text-foreground tabular-nums">{Math.round(coverage)}%</span>
        </div>
        <div role="progressbar" aria-label={t('Measured coverage')} aria-valuenow={Math.round(coverage)} aria-valuemin={0} aria-valuemax={100}
          className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-muted">
          <div className="h-full rounded-full bg-muted-foreground/45 transition-[width] duration-500" style={{ width: `${Math.min(100, Math.max(0, coverage))}%` }} />
        </div>
      </div>}
    </div>
  </section>
}

function ScoreRing({ score, busy }: { score: number | null; busy: boolean }) {
  useLocale()
  const radius = 52
  const circumference = 2 * Math.PI * radius
  const clamped = score == null ? 0 : Math.max(0, Math.min(100, score))
  const grade = score == null ? null : gradeFor(score)
  const label = busy
    ? t('Testing network quality')
    : score == null
      ? t('No overall score')
      : t('Overall score {score} out of 100, {grade}', { score: scoreText(score), grade: t(grade!.label) })

  return <div className="relative size-28 shrink-0" role="img" aria-label={label}>
    <svg viewBox="0 0 120 120" className="size-full -rotate-90" aria-hidden="true">
      <circle cx="60" cy="60" r={radius} className="fill-none stroke-border" strokeWidth="8" />
      {score != null && <circle
        cx="60" cy="60" r={radius}
        className={cn('fill-none stroke-current transition-[stroke-dashoffset] duration-700', grade && toneStroke[grade.variant])}
        strokeWidth="8" strokeLinecap="round"
        strokeDasharray={circumference}
        strokeDashoffset={circumference * (1 - clamped / 100)}
      />}
    </svg>
    <div className="absolute inset-0 flex flex-col items-center justify-center" aria-hidden="true">
      {busy
        ? <LoaderCircle className="size-7 animate-spin text-primary" aria-hidden="true" />
        : score == null
          ? <span className="text-3xl leading-none font-semibold text-muted-foreground">—</span>
          : <span className="text-4xl leading-none font-semibold tracking-tight text-foreground tabular-nums">{scoreText(score)}</span>}
      <span className="mt-1.5 text-xs font-medium tracking-wider text-muted-foreground uppercase">
        {busy ? t('Testing…') : score == null ? t('No score') : '/ 100'}
      </span>
    </div>
  </div>
}

function TransferRow({ arrow, label, value }: { arrow: '↓' | '↑'; label: string; value: number | null }) {
  return <div className="flex items-center justify-between gap-3 py-0.5">
    <span className="flex min-w-0 items-center gap-1.5 text-sm text-muted-foreground">
      <span aria-hidden="true" className="text-base">{arrow}</span>{label}
    </span>
    <MetricValue size="md" value={value == null ? null : value.toFixed(1)} unit="Mbps" />
  </div>
}

/**
 * The reference transfer's own numbers, kept under the row that scores them.
 *
 * Download and upload are not a second score and not a category of their own:
 * they are the evidence behind Speed, so they sit inside Speed's own detail
 * rather than in a card beside the report. The server line stays because one
 * edge to one provider is the whole caveat a reader needs here.
 */
function TransferDetail({ data, busy }: { data?: QualitySnapshot; busy: boolean }) {
  useLocale()
  const speed = data?.speed ?? null
  const where = speed ? [speed.location, speed.server].filter(Boolean).join(' · ') : ''
  return <div className="mt-1 mb-1.5 ml-5">
    <div className="rounded-lg bg-muted/30 px-3 py-2">
      <TransferRow arrow="↓" label={t('Download')} value={speed?.download_mbps ?? null} />
      <TransferRow arrow="↑" label={t('Upload')} value={speed?.upload_mbps ?? null} />
    </div>
    {where && <p className="mt-1.5 break-words text-xs leading-5 text-muted-foreground">{where}</p>}
    <p className="mt-1 text-xs leading-5 text-muted-foreground">
      {busy ? t('Measuring download and upload') : speed ? t('One test server, not a service-wide benchmark.') : t('Not measured')}
    </p>
  </div>
}

function categorySummary(evidence: CategoryEvidence, category: QualityCategory, hasRun: boolean): string | null {
  if (!hasRun) return t('Weight {weight} of 100', { weight: Math.round(category.budget) })
  if (category.state === 'unavailable') return t('Not measured')
  switch (evidence.kind) {
    case 'speed':
      // Throughput leads in the transfer panel beside the score. Repeating it
      // here would spend a row on a number the same screen already shows, so
      // this line carries the limit of what the category score covers.
      return evidence.speed ? t('Reference transfer only') : t('Not measured')
    case 'latency': {
      const parts: string[] = []
      if (evidence.latency.response != null) parts.push(t('{value} ms median', { value: evidence.latency.response.toFixed(1) }))
      if (evidence.latency.variation != null) parts.push(t('{value} ms variation', { value: evidence.latency.variation.toFixed(1) }))
      return parts.length > 0 ? parts.join(' · ') : t('Not measured')
    }
    case 'stability':
      return evidence.stability.attempted === 0
        ? t('Not measured')
        : t('{count} of {total} expected responses', { count: evidence.stability.responded, total: evidence.stability.attempted })
    case 'functionality':
      return evidence.functionality.measured === 0
        ? t('Not measured')
        : t('{count} of {total} services reachable', { count: evidence.functionality.reachable, total: evidence.functionality.measured })
    case 'wireless': {
      const wireless = evidence.wireless
      if (!wireless) return t('Not measured')
      if (wireless.links === 0) return t('No associated clients')
      const parts = [t('{links} links on {aps} access points', { links: wireless.links, aps: wireless.accessPoints })]
      if (wireless.weakest != null) parts.push(t('Weakest {value} dBm', { value: wireless.weakest }))
      if (wireless.answered < wireless.accessPoints) {
        parts.push(t('{count} of {total} access points answered', { count: wireless.answered, total: wireless.accessPoints }))
      }
      return parts.join(' · ')
    }
    default:
      return null
  }
}

function CategoryList({ categories, data, services, hasRun, runningScope, runningTarget, anyRunning, canRerun, onRerun }: {
  categories: QualityCategory[]
  data?: QualitySnapshot
  services: ServiceEvidence[]
  hasRun: boolean
  runningScope: QualityScope | ''
  runningTarget?: string
  anyRunning: boolean
  canRerun: boolean
  onRerun: (action: QualityAction) => void
}) {
  useLocale()
  if (categories.length === 0) return <div className="panel-surface px-5 py-8 text-center text-sm text-muted-foreground">{t('No measurements yet.')}</div>
  return <section aria-label={t('Categories')} className="panel-surface divide-y divide-border">
    {categories.map(category => <CategoryRow
      key={category.id}
      category={category}
      data={data}
      services={services}
      hasRun={hasRun}
      runningScope={runningScope}
      runningTarget={runningTarget}
      anyRunning={anyRunning}
      canRerun={canRerun}
      onRerun={onRerun}
    />)}
  </section>
}

function CategoryRow({ category, data, services, hasRun, runningScope, runningTarget, anyRunning, canRerun, onRerun }: {
  category: QualityCategory
  data?: QualitySnapshot
  services: ServiceEvidence[]
  hasRun: boolean
  runningScope: QualityScope | ''
  runningTarget?: string
  anyRunning: boolean
  canRerun: boolean
  onRerun: (action: QualityAction) => void
}) {
  useLocale()
  const [open, setOpen] = useState(false)
  const score = category.score
  const displayed = score == null ? null : Math.round(score)
  const grade = displayed == null ? null : gradeFor(displayed)
  const width = score == null ? 0 : Math.max(0, Math.min(100, score))
  const scope = categoryScope(category.id)
  const rerun: QualityAction | null = scope ? { scope } : null
  const tests = data ? categoryTests(category, services, data) : []
  const busy = scope != null && runningScope === scope
  const label = t(categoryLabel(category.label))
  const listID = `quality-${category.id}-tests`
  const summary = categorySummary(categoryEvidence(category, services, data), category, hasRun)
  // "Partial" would repeat on every row: the score is complete only when its
  // categories name the checks that did run. Only a state a reader can act on
  // earns a word here, and an unmeasured category says so in its own line.
  const stateWord = hasRun && category.state === 'stale' ? t('Outdated') : null

  return <div className="px-4 py-2.5 sm:px-5">
    <div className="flex items-center gap-2">
      <Heading as="h3" size="md" className="min-w-0 flex-1">
        <button
          type="button"
          aria-expanded={open}
          aria-controls={open ? listID : undefined}
          onClick={() => setOpen(value => !value)}
          className="flex min-h-11 w-full items-center gap-2 rounded-md text-left hover:text-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring sm:min-h-9"
        >
          <ChevronRight className={cn('size-4 shrink-0 text-muted-foreground transition-transform', open && 'rotate-90')} aria-hidden="true" />
          <span className="min-w-0 truncate">{label}</span>
          {stateWord && <span className="shrink-0 text-xs font-normal text-muted-foreground">{stateWord}</span>}
        </button>
      </Heading>
      <span className="w-24 shrink-0 text-right whitespace-nowrap tabular-nums">
        {displayed == null
          ? <span className="text-lg leading-6 font-normal text-muted-foreground">—</span>
          : <><span className="text-lg leading-6 font-semibold text-foreground">{displayed}</span><span className="text-xs font-normal text-muted-foreground"> / 100</span></>}
      </span>
      {/* The action slot is reserved even when a category has no rerun, so the
          scores stay in one column instead of stepping sideways. */}
      <div className="flex w-8 shrink-0 justify-end">
        {rerun && canRerun && <Button
          variant="quiet"
          size="icon-sm"
          className={cn('shrink-0', busy && 'disabled:opacity-100')}
          disabled={anyRunning}
          onClick={() => onRerun(rerun)}
          aria-label={t('Rerun {category} tests', { category: label })}
          title={t('Rerun {category} tests', { category: label })}
        >
          {busy ? <LoaderCircle className="animate-spin" aria-hidden="true" /> : <RotateCw aria-hidden="true" />}
        </Button>}
      </div>
    </div>
    {(score != null || summary) && <div className="mt-0.5 flex flex-col gap-1.5 pl-6 sm:flex-row sm:flex-wrap sm:items-center sm:gap-x-3">
      {summary && <span className="min-w-0 flex-1 text-xs text-muted-foreground tabular-nums">{summary}</span>}
      {score != null && <div
        role="progressbar"
        aria-label={label}
        aria-valuenow={displayed ?? undefined}
        aria-valuemin={0}
        aria-valuemax={100}
        // One track width for every category: bars that stretch to their own
        // text cannot be read against each other.
        className="h-1.5 w-full shrink-0 overflow-hidden rounded-full bg-muted sm:w-40"
      >
        <div className={cn('h-full rounded-full transition-[width] duration-500', grade && toneFill[grade.variant])} style={{ width: `${width}%` }} />
      </div>}
    </div>}
    {open && <ul id={listID} className="mt-2 divide-y divide-border/60 border-t border-border/60 pl-6">
      {tests.map(test => <QualityTestRow
        key={`${category.id}-${test.id}`}
        test={test}
        category={category.id}
        data={data}
        view={category.id === 'latency' ? 'timing' : category.id === 'stability' ? 'continuity' : 'access'}
        runningScope={runningScope}
        runningTarget={runningTarget}
        anyRunning={anyRunning}
        canRerun={canRerun}
        onRerun={onRerun}
      />)}
    </ul>}
  </div>
}

interface TestMetric {
  id: string
  label: string
  score: number | null
  weight: number
}

interface TestGroup {
  id: string
  label: string
  target?: string
  /** What opening the row shows, or `null` when the row is all there is. */
  evidence: 'samples' | 'transfer' | 'nat' | 'links' | null
  scope: 'target' | 'speed' | 'nat' | 'wifi' | null
  score: number | null
  metrics: TestMetric[]
  /** What the group measured, in its own units, so the row can be checked. */
  detail?: string
  /** The attempts behind a service row, and the resource that answered them. */
  samples?: QualitySample[]
  url?: string
  /** Which path the scored evidence came from; the two are never averaged. */
  family?: 'ipv4' | 'ipv6'
}

/**
 * Which part of a sample its category was scored from.
 *
 * The same six attempts feed three categories, so the evidence is shown three
 * times — once as timings where Latency reads it, once as a sequence where
 * Stability counts it, and once as an outcome where Functionality judges it.
 * Each listing is short because it leaves out the columns another category
 * already reads.
 */
type SampleView = 'timing' | 'continuity' | 'access'

function weightedScore(metrics: TestMetric[]) {
  let weighted = 0
  let weight = 0
  for (const metric of metrics) {
    if (metric.score == null) continue
    weighted += metric.score * metric.weight
    weight += metric.weight
  }
  return weight > 0 ? weighted / weight : null
}

const timingDetail = (label: string, value: number | null | undefined) => value == null ? undefined : `${label} ${value.toFixed(1)} ms`

function categoryTests(category: QualityCategory, services: ServiceEvidence[], data?: QualitySnapshot): TestGroup[] {
  const groups: TestGroup[] = []
  const targets = new Map<string, TestGroup>()
  const details = new Map<string, string[]>()
  const byId = new Map(services.map(service => [service.id, service]))
  const addDetail = (key: string, part: string | undefined) => {
    if (!part) return
    details.set(key, [...details.get(key) ?? [], part])
  }

  for (const component of category.components) {
    if (component.target) {
      let group = targets.get(component.target)
      if (!group) {
        const separator = component.label.indexOf(' · ')
        // The score picks one address family per service, so the evidence is
        // that family's own attempts rather than a blend of two paths.
        const measured = data?.targets.find(target => target.id === component.target && target.family === byId.get(component.target)?.family)
        group = {
          id: component.target,
          label: separator < 0 ? component.target : component.label.slice(0, separator),
          target: component.target,
          evidence: measured ? 'samples' : null,
          scope: 'target',
          score: null,
          metrics: [],
          samples: measured?.samples,
          url: measured?.url,
          family: measured?.family,
        }
        targets.set(component.target, group)
        groups.push(group)
      }
      const separator = component.label.indexOf(' · ')
      group.metrics.push({
        id: component.id,
        label: t(separator < 0 ? component.label : component.label.slice(separator + 3)),
        score: component.score,
        weight: component.weight,
      })
      const evidence = byId.get(component.target)
      switch (component.kind) {
        case 'tcp': addDetail(component.target, timingDetail(t('TCP'), evidence?.tcp)); break
        case 'dns': addDetail(component.target, timingDetail(t('DNS'), evidence?.dns)); break
        case 'response': addDetail(component.target, timingDetail(t('HTTPS'), evidence?.response)); break
        case 'continuity':
          if (evidence) addDetail(component.target, t('{count} of {total} connected', { count: evidence.responded, total: evidence.samples }))
          break
        case 'variation':
          addDetail(component.target, evidence?.variation == null ? undefined : `${evidence.variation.toFixed(1)} ms ${t('variation')}`)
          break
        case 'access':
          if (evidence) addDetail(component.target, t('{count} of {total} expected responses', { count: evidence.responded, total: evidence.samples }))
          break
      }
      continue
    }
    if (component.id === 'reference') {
      groups.push({
        id: 'reference',
        label: t('Cloudflare download and upload'),
        evidence: 'transfer',
        scope: 'speed',
        score: component.score,
        metrics: [],
      })
      continue
    }
    groups.push({ id: component.id, label: t(component.label), evidence: null, scope: null, score: component.score, metrics: [] })
  }

  // Wireless evidence is one list of links, not one service per access point:
  // a client belongs to exactly one of them, and the category score is the mean
  // over those links.
  if (category.id === 'wifi' && data) {
    const wireless = data.wireless
    // The row exists even when no access point answered: expanding the
    // category must explain the gap instead of showing an empty list.
    groups.push({
      id: 'links',
      label: t('Observed links'),
      evidence: wireless ? 'links' : null,
      scope: 'wifi',
      score: category.components[0]?.score ?? null,
      metrics: [],
      detail: wireless && wireless.links.length > 0
        ? t('{links} links on {aps} access points', { links: wireless.links.length, aps: wireless.aps.length })
        : data.wireless_error || t('No associated clients'),
    })
  }

  // NAT is Functionality evidence (docs/network-quality.md §6) that the score
  // deliberately does not reward, so it is listed where a reader looks for that
  // capability and says in its own line why it carries no number.
  if (category.id === 'functionality' && data && (data.nat || data.nat_error)) {
    groups.push({
      id: 'nat',
      label: t('NAT observations'),
      evidence: 'nat',
      scope: 'nat',
      score: null,
      metrics: [],
      detail: t('Diagnostic only, not scored'),
    })
  }

  for (const group of groups) {
    if (group.target) {
      group.score = weightedScore(group.metrics)
      const parts = details.get(group.target)
      if (parts && parts.length > 0) group.detail = parts.join(' · ')
    }
  }
  return groups
}

function QualityTestRow({ test, category, data, view, runningScope, runningTarget, anyRunning, canRerun, onRerun }: {
  test: TestGroup
  /** Namespaces the row's ids: one service appears under several categories. */
  category: string
  data?: QualitySnapshot
  view: SampleView
  runningScope: QualityScope | ''
  runningTarget?: string
  anyRunning: boolean
  canRerun: boolean
  onRerun: (action: QualityAction) => void
}) {
  useLocale()
  const [open, setOpen] = useState(false)
  const busy = test.scope === 'target' ? runningScope === 'target' && runningTarget === test.target : test.scope != null && runningScope === test.scope
  const rerun: QualityAction | null = test.scope === 'target' && test.target
    ? { scope: 'target', target: test.target }
    : test.scope === 'speed' || test.scope === 'nat' || test.scope === 'wifi'
      ? { scope: test.scope }
      : null
  const openable = test.evidence === 'samples' || test.evidence === 'nat' || test.evidence === 'links'
  const bodyID = `quality-${category}-${test.id}-evidence`
  // The Speed row's own rerun is the reference transfer's rerun; offering the
  // same button twice in a stack of two rows reads as two different actions.
  const duplicated = test.scope != null && test.scope === categoryScope(category)

  return <li className="py-1">
    <div className="flex items-start gap-2">
      <div className="min-w-0 flex-1">
        {openable
          ? <button
              type="button"
              aria-expanded={open}
              aria-controls={open ? bodyID : undefined}
              onClick={() => setOpen(value => !value)}
              className="flex min-h-11 w-full items-center gap-1.5 rounded-md text-left text-sm text-foreground hover:text-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring sm:min-h-9"
            >
              <ChevronRight className={cn('size-3.5 shrink-0 text-muted-foreground transition-transform', open && 'rotate-90')} aria-hidden="true" />
              <span className="min-w-0 truncate">{test.label}</span>
            </button>
          : <p className="flex min-h-11 items-center text-sm text-foreground sm:min-h-9">{test.label}</p>}
        {test.detail && <p className={cn('text-xs leading-5 text-muted-foreground tabular-nums', openable && 'pl-5')}>{test.detail}</p>}
      </div>
      <div className="flex shrink-0 items-start gap-2">
        {/* Same trailing columns as the category above, so a service score lands
            under the category score it explains. */}
        <span className="w-24 text-right text-sm font-semibold tabular-nums">
          {test.score != null
            ? Math.round(test.score)
            : <span className="text-xs font-normal text-muted-foreground">{test.scope ? '—' : t('Not measured')}</span>}
        </span>
        <div className="flex w-8 justify-end">
          {rerun && !duplicated && canRerun && <Button
            variant="quiet"
            size="icon-sm"
            className={busy ? 'disabled:opacity-100' : undefined}
            disabled={anyRunning}
            onClick={() => onRerun(rerun)}
            aria-label={t('Rerun {name} tests', { name: test.label })}
            title={t('Rerun {name} tests', { name: test.label })}
          >
            {busy ? <LoaderCircle className="animate-spin" aria-hidden="true" /> : <RotateCw aria-hidden="true" />}
          </Button>}
        </div>
      </div>
    </div>
    {open && test.evidence === 'samples' && test.samples && <SampleList id={bodyID} samples={test.samples} url={test.url} family={test.family} view={view} />}
    {open && test.evidence === 'nat' && <NATDetail id={bodyID} data={data} />}
    {open && test.evidence === 'links' && <WirelessDetail id={bodyID} data={data} />}
    {/* The transfer is the row's own measurement, not a detail under it, so its
        two numbers are visible as soon as the category is open. */}
    {test.evidence === 'transfer' && <TransferDetail data={data} busy={busy} />}
  </li>
}

function sampleOutcome(sample: QualitySample) {
  const parts: string[] = [sample.state]
  // The stage only explains a sample that did not reach its resource; on a
  // successful one it would be the same word six times.
  if (sample.state !== 'ok' && sample.stage) parts.push(sample.stage)
  if (sample.status) parts.push(`HTTP ${sample.status}`)
  return parts.join(' · ')
}

function sampleTone(view: SampleView, state: QualitySample['state']) {
  if (state === 'ok') return view === 'timing' ? 'text-muted-foreground' : 'text-foreground'
  return state === 'failed' || state === 'error' ? 'text-destructive' : 'text-muted-foreground'
}

function SampleList({ id, samples, url, family, view }: {
  id: string
  samples: QualitySample[]
  url?: string
  family?: 'ipv4' | 'ipv6'
  view: SampleView
}) {
  useLocale()
  return <div id={id} className="mt-1 mb-1.5 ml-5 max-w-3xl rounded-lg bg-muted/30 px-3 py-2">
    {url && <p className="flex flex-wrap items-baseline gap-x-1.5 text-xs leading-5 text-muted-foreground">
      {/* The score reads one family per service, so the attempts say which. */}
      {family && <span className="font-medium text-foreground">{family === 'ipv4' ? 'IPv4' : 'IPv6'}</span>}
      <span className="min-w-0 break-all font-mono">{url}</span>
    </p>}
    <ol className="mt-1 space-y-1">
      {samples.map((sample, index) => <li key={index} className="text-xs">
        <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5">
          <span className="flex min-w-0 items-baseline gap-1.5">
            <span className="tabular-nums text-muted-foreground">{index + 1}.</span>
            <span className={cn('min-w-0 break-words font-medium', sampleTone(view, sample.state))}>{sampleOutcome(sample)}</span>
          </span>
          {view !== 'access' && <span className="tabular-nums text-muted-foreground">
            {view === 'continuity'
              ? `TCP ${duration(sample, 'tcp_ms')}`
              : `DNS ${duration(sample, 'dns_ms')} · TCP ${duration(sample, 'tcp_ms')} · HTTPS ${duration(sample, 'response_ms')}`}
          </span>}
        </div>
        {sample.error && <p className="mt-0.5 break-words text-muted-foreground">{sample.error}</p>}
        {/* One address per path, not one per attempt: repeated six times it is
            noise, and when DNS moves the row changes and says so. */}
        {view === 'timing' && sample.address && sample.address !== samples[index - 1]?.address
          && <TechnicalValue className="mt-0.5 block text-xs">{sample.address}</TechnicalValue>}
      </li>)}
    </ol>
  </div>
}

/** Property rows elsewhere lead a six-rem label column; "Observed mapping"
 * needs a little more before it wraps. */
const natRow = 'grid-cols-[8rem_minmax(0,1fr)]'

function NATDetail({ id, data }: { id: string; data?: QualitySnapshot }) {
  useLocale()
  const nat = data?.nat
  return <div id={id} className="mt-1 mb-1.5 ml-5 max-w-xl rounded-lg bg-muted/30 px-3 py-2">
    {nat && <dl>
      <PropertyRow label={t('STUN server')} size="sm" className={natRow}><TechnicalValue>{nat.server}</TechnicalValue></PropertyRow>
      {nat.nat_type && <PropertyRow label={t('NAT type')} size="sm" className={natRow}>{natTypeLabel(nat.nat_type)}</PropertyRow>}
      <PropertyRow label={t('Observed mapping')} size="sm" className={natRow}><TechnicalValue>{nat.mapped}</TechnicalValue></PropertyRow>
      <PropertyRow label={t('Mapping')} size="sm" className={natRow}>{mappingVerdict(nat.mapping)}</PropertyRow>
      <PropertyRow label={t('Filtering')} size="sm" className={natRow}>{filteringVerdict(nat.filtering)}</PropertyRow>
    </dl>}
    {data?.nat_error && <p className="break-words text-xs leading-5 text-muted-foreground">{data.nat_error}</p>}
    <p className="mt-1 text-xs leading-5 text-muted-foreground">{t('An observed mapping does not prove public IPv4 assignment, inbound access or P2P success. These observations do not add points.')}</p>
  </div>
}

/**
 * The wireless evidence is a list of links, not a number: each row is one
 * client as its access point reports it. The rate is the radio's negotiated
 * rate, which a footnote says is not throughput — a link can be fast on paper
 * and slow in the air.
 */
function WirelessDetail({ id, data }: { id: string; data?: QualitySnapshot }) {
  const locale = useLocale()
  const wireless = data?.wireless
  if (!wireless) return null
  return <div id={id} className="mt-1 mb-1.5 ml-5 max-w-3xl rounded-lg bg-muted/30 px-3 py-2">
    <ol className="space-y-1">
      {wireless.links.map((link, index) => {
        const rate = Math.max(link.tx_rate_mbps ?? 0, link.rx_rate_mbps ?? 0)
        return <li key={index} className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-xs">
          <span className="flex min-w-0 items-baseline gap-1.5">
            <span className="break-all font-mono">{link.mac}</span>
            {link.band && <span className="uppercase text-muted-foreground">{link.band}</span>}
          </span>
          <span className="tabular-nums text-muted-foreground">
            {link.signal_dbm == null ? '—' : `${link.signal_dbm} dBm`}
            {link.snr_db != null && ` · ${link.snr_db} dB SNR`}
            {rate > 0 && ` · ${Math.round(rate)} Mbps`}
          </span>
        </li>
      })}
    </ol>
    {wireless.aps.filter(ap => ap.state === 'error').map(ap => <p key={ap.id} className="mt-1 break-words text-xs leading-5 text-destructive">
      {t('{accessPoint} did not answer', { accessPoint: ap.name || ap.id })}: {ap.error}
    </p>)}
    {data?.wireless_error && <p className="mt-1 break-words text-xs leading-5 text-destructive">
      {t('The last wireless read failed; these links are from {time}.', { time: formatWhen(wireless.observed_at, locale) })}: {data.wireless_error}
    </p>}
    <p className="mt-1 text-xs leading-5 text-muted-foreground">
      {t('Signal and SNR as the access point reports them, for the clients attached right now. Link rate is not throughput.')}
    </p>
  </div>
}

function duration(sample: QualitySample, key: 'dns_ms' | 'tcp_ms' | 'response_ms') {
  const value = sample[key]
  return value == null ? '—' : value < 1 ? '<1 ms' : `${value.toFixed(1)} ms`
}
