import { useLocale } from '@/lib/i18n'

import { Launcher, LauncherSection, LauncherTile } from '@/components/layout/launcher'

import { GATEWAY_GROUPS } from './catalog'

/**
 * Gateway, on the page you land on: one tile per way of building each part.
 *
 * The section keeps a shape the other two directories already have, because the
 * question here is the same one Access asks — "which of these does this network
 * use" — and not "what is each of them doing right now". Status is one level
 * down, on the page a tile opens; a tile holds no switch, no status strip and
 * no draft.
 */
export function GatewayDirectory() {
  useLocale()
  return (
    <Launcher>
      {GATEWAY_GROUPS.map(group => (
        <LauncherSection key={group.id} id={group.id} title={group.title}>
          {group.choices.map(choice => (
            <LauncherTile
              key={choice.id}
              name={choice.name}
              description={choice.description}
              mark={{ kind: 'letter', letter: choice.letter, hue: group.hue }}
              to={choice.to}
              href={choice.href}
              planned={choice.planned}
            />
          ))}
        </LauncherSection>
      ))}
    </Launcher>
  )
}
