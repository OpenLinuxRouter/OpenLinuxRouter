import { t, useLocale } from '@/lib/i18n'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { NoProxyAlert } from '@/features/ingress/no-proxy'
import { useIngressProviders, useIngressStatus } from '@/features/ingress/queries'
import type { Certificate, IngressConfig } from '@/lib/config-types'

/** What the API hands back in place of the stored credential. */
const REDACTED = '********'

/**
 * The settings paid for once, so that publishing a service afterwards is two
 * fields.
 *
 * The domain is one of them because it is this module's own fact now: it is the
 * zone the operator writes records into, and the one the wildcard is for. It is
 * deliberately not the resolver's local domain — that names the devices in the
 * house and never needs a certificate (docs/ingress.md §3).
 *
 * One wildcard certificate covers every name that will ever be published, and
 * getting it means proving the domain is yours — which for a name the internet
 * cannot resolve means writing a temporary record through your DNS provider's
 * API. Hence a provider and a credential, and hence this being a dialog you open
 * about once.
 *
 * Two things here are not what they look like:
 *
 * The provider list is **fetched**, not bundled. olr ships no proxy, so which
 * providers exist is a property of the binary the operator installed — a list
 * compiled into this app would offer names their Caddy does not have.
 *
 * The credential field starts empty even when one is stored, and an empty field
 * means "leave it alone". The API never returns the real value, and the mask it
 * returns instead means unchanged — so there is no way to destroy a working
 * credential by opening this dialog to read it and pressing Save.
 */
export function CertificateDialog({
  open,
  onOpenChange,
  initial,
  domain,
  publicDomain,
  onSubmit,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  initial: Certificate
  domain?: string
  /** The suffix exposed services answer on, when one is set. */
  publicDomain?: string
  onSubmit: (fields: Partial<IngressConfig>) => void
}) {
  useLocale()
  const [suffix, setSuffix] = useState(domain ?? '')
  const [publicSuffix, setPublicSuffix] = useState(publicDomain ?? '')
  const [provider, setProvider] = useState(initial.provider ?? '')
  const [token, setToken] = useState('')
  const [email, setEmail] = useState(initial.acme_email ?? '')

  const providers = useIngressProviders()
  // Served from react-query's cache; the page above has already asked.
  const status = useIngressStatus()
  const stored = (initial.provider_token ?? '') !== ''

  function reset() {
    setSuffix(domain ?? '')
    setPublicSuffix(publicDomain ?? '')
    setProvider(initial.provider ?? '')
    setToken('')
    setEmail(initial.acme_email ?? '')
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) reset()
        onOpenChange(next)
      }}
    >
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t('Certificate')}</DialogTitle>
          <DialogDescription> {t('How olr proves')} {suffix ? <span className="font-mono">{suffix}</span> : t('your domain')} {t('is yours, so browsers trust every address you publish.')}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="cert-domain">{t('Published domain')}</Label>
            <Input
              id="cert-domain"
              autoComplete="off"
              value={suffix}
              placeholder="home.example.com"
              onChange={(e) => setSuffix(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              {t('Names you publish live under this: grafana here becomes grafana.{domain}. The records are yours, in this domain\'s zone — point them at this router.', { domain: suffix || 'home.example.com' })}
            </p>
            <p className="text-xs text-muted-foreground">
              {t('Not the same as the resolver\'s local domain, which names the clients in the house and never needs a certificate.')}
            </p>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="cert-public-domain">{t('Public domain (optional)')}</Label>
            <Input
              id="cert-public-domain"
              autoComplete="off"
              value={publicSuffix}
              placeholder="example.com"
              onChange={(e) => setPublicSuffix(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              {t('Where exposed services answer from the internet: rustfs here becomes rustfs.{domain}. A second zone and a second wildcard certificate, so the names above stay on this network and out of public DNS. Empty means nothing is exposed.', { domain: publicSuffix || 'example.com' })}
            </p>
            <p className="text-xs text-muted-foreground">
              {t('The provider credential must be allowed to write this zone as well, or the second certificate never issues.')}
            </p>
          </div>

          {providers.isError && (
            <NoProxyAlert
              message={(providers.error as Error).message}
              binary={status.data?.binary}
            />
          )}

          <div className="space-y-1.5">
            <Label htmlFor="cert-provider">{t('Who hosts your DNS')}</Label>
            <Select value={provider || undefined} onValueChange={(v) => setProvider(v ?? '')}>
              <SelectTrigger id="cert-provider" disabled={!providers.data}>
                <SelectValue placeholder={providers.data ? 'Choose a provider' : t('Unavailable')} />
              </SelectTrigger>
              <SelectContent>
                {(providers.data?.providers ?? []).map((p) => (
                  <SelectItem key={p} value={p}>
                    {p}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              Only the providers your proxy was built to talk to are listed.
            </p>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="cert-token">{t('API token')}</Label>
            <Input
              id="cert-token"
              type="password"
              autoComplete="off"
              value={token}
              placeholder={stored ? 'Stored — leave blank to keep it' : ''}
              onChange={(e) => setToken(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              {stored
                ? 'A token is stored. It is never shown again; type a new one only to replace it.'
                : 'Scope it to this one domain if your provider allows that — it is kept on the router and can change your DNS.'}
            </p>
          </div>

          <Disclosure summary={t('Expiry warnings')}>
            <div className="space-y-1.5">
              <Label htmlFor="cert-email">{t('Email address')}</Label>
              <Input
                id="cert-email"
                type="email"
                autoComplete="off"
                value={email}
                placeholder="you@example.com"
                onChange={(e) => setEmail(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                Optional. The certificate authority uses it to warn you before a certificate
                expires — a warning worth having, since olr watches for the same thing.
              </p>
            </div>
          </Disclosure>
        </div>

        <DialogFooter>
          <Button variant="quiet" onClick={() => onOpenChange(false)}>{t('Cancel')}</Button>
          <Button
            onClick={() => {
              const cert: Certificate = { provider }
              // Only sent when typed. An untouched field must not overwrite the
              // stored credential, and RFC 7386 leaves an unmentioned key alone —
              // so omitting it is both the simplest and the safest thing.
              if (token !== '' && token !== REDACTED) cert.provider_token = token
              if (email !== (initial.acme_email ?? '')) cert.acme_email = email
              const fields: Partial<IngressConfig> = { certificate: cert }
              // Only when typed, for the same reason the credential is: an
              // untouched field must not rewrite a value it never read.
              if (suffix.trim() !== (domain ?? '')) fields.domain = suffix.trim()
              // Empty is a real value here: it is how the operator turns the
              // feature off. With a service still exposed the daemon refuses,
              // naming the service, which is the message they need.
              if (publicSuffix.trim() !== (publicDomain ?? '')) {
                fields.public_domain = publicSuffix.trim()
              }
              onSubmit(fields)
              onOpenChange(false)
            }}
          >{t('Save')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
