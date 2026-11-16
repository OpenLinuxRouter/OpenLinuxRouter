import { t, useLocale } from '@/lib/i18n'
import { PageHeading } from '@/components/layout/page-heading'
import { groupOf } from '@/components/layout/sections'

export function SubPage({ section, slug, children, action }: {
  section: string
  slug: string
  children: React.ReactNode
  action?: React.ReactNode
}) {
  useLocale()
  const { section: parent, group } = groupOf(section, slug)
  // The rail already highlights the section, so a sub-page never links back to
  // it. Only Gateway's nested settings need a position, and the section root —
  // the page that carries its section's own name — is that position.
  const breadcrumbs = parent.nested && slug !== 'details'
    ? [{ to: '/gateway', get label() { return t('Gateway') } }, { to: parent.to, label: parent.label }]
    : undefined

  return (
    <div className="space-y-8">
      <PageHeading title={group.label} description={group.blurb}
        breadcrumbs={breadcrumbs} action={action} />
      {children}
    </div>
  )
}
