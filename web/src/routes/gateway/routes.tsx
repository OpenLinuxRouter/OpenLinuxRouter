import { t, useLocale } from '@/lib/i18n'
import { AlertTriangle } from 'lucide-react'

import { SubPage } from '@/components/layout/sub-page'
import { SettingsList } from '@/components/layout/settings-list'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Heading } from '@/components/ui/heading'
import { List, ListEmpty, ListRow } from '@/components/ui/list'
import { Skeleton } from '@/components/ui/skeleton'
import { useGatewayRoutes, useGatewayStatus } from '@/features/gateway/queries'
import type { RouteRow, RouteTable } from '@/lib/api-types'

/**
 * The routing table, on a page of its own under Gateway.
 *
 * Read-only, and not a setting that is missing a switch: a static route is its
 * own object (docs/gateway.md §2.4) and this page reports the tables every route
 * is already in — the box's main table, the tables olr writes per exit, and any
 * table a rule from outside olr points at. The one thing it must never do is
 * present those three as the same kind of thing, because olr deliberately does
 * not own the main table (design.md §3.4).
 */
export function GatewayRoutesPage() {
  useLocale()
  const routes = useGatewayRoutes()
  const status = useGatewayStatus()
  const tables = routes.data?.tables ?? []
  const foreign = status.data?.foreign ?? []

  return (
    <SubPage section="/gateway" slug="routes">
      {routes.isError ? (
        <Alert variant="destructive">
          <AlertTitle>{t('Could not read the routing table')}</AlertTitle>
          <AlertDescription>{routes.error.message}</AlertDescription>
        </Alert>
      ) : routes.isPending ? (
        <Skeleton className="h-32 w-full" />
      ) : !routes.data?.known ? (
        <Alert>
          <AlertTriangle />
          <AlertTitle>{t('Routing state unknown')}</AlertTitle>
          <AlertDescription>
            {routes.data?.error ?? t('External rules could not be verified. Check the router\'s permissions.')}
          </AlertDescription>
        </Alert>
      ) : tables.length === 0 ? (
        <ListEmpty>{t('No routes were reported.')}</ListEmpty>
      ) : (
        ([4, 6] as const).map((family) => {
          const ofFamily = tables.filter((table) => table.family === family)
          if (ofFamily.length === 0) return null
          return (
            <section key={family} className="space-y-3">
              <Heading>{family === 4 ? t('IPv4') : t('IPv6')}</Heading>
              {ofFamily.map((table) => <RouteTableCard key={table.table} table={table} />)}
            </section>
          )
        })
      )}

      {foreign.length > 0 && (
        <SettingsList
          section="/gateway"
          heading={null}
          rows={[{ slug: 'unmanaged', value: `${foreign.length} rule${foreign.length === 1 ? '' : 's'}` }]}
        />
      )}
    </SubPage>
  )
}

/**
 * One table: who owns it, which rule selects it, and its routes in match order.
 *
 * The ownership line is the part that matters. "main" and an exit's name look
 * alike as headings and mean opposite things — one is the box's, one is ours —
 * so the second line says it in words rather than leaving it to the reader to
 * remember which numbers are olr's.
 */
function RouteTableCard({ table }: { table: RouteTable }) {
  useLocale()
  return (
    <div className="space-y-2">
      <div className="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-1">
        <h3 className="text-sm font-semibold">{table.name}</h3>
        <span className="text-meta text-muted-foreground">
          {table.owner === 'main'
            ? t('This router’s own table')
            : table.owner === 'exit'
              ? t('OLR’s table for this exit · priority {priority}', { priority: table.priority ?? 0 })
              : t('Table {id}, written outside OLR', { id: table.table })}
        </span>
      </div>
      <List>
        {table.routes.map((route) => <RouteListRow key={`${table.table}-${route.dst}-${route.metric ?? 0}-${route.dev ?? ''}`} route={route} />)}
      </List>
    </div>
  )
}

function RouteListRow({ route }: { route: RouteRow }) {
  useLocale()
  const where = [
    route.via ? `via ${route.via}` : undefined,
    route.dev ? `dev ${route.dev}` : undefined,
    route.metric ? `metric ${route.metric}` : undefined,
  ].filter(Boolean).join(' · ')
  // How the row was installed, unless the row is not a unicast route at all —
  // "unreachable" is the route's whole meaning and belongs where the value is.
  const trailing = route.kind && route.kind !== 'unicast' ? route.kind : route.proto
  return <ListRow title={route.dst} subtitle={where || undefined} trailing={trailing || undefined} />
}
