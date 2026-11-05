import { GatewayDirectory } from '@/features/gateway/directory'

/**
 * The gateway, on the page you land on.
 *
 * The page itself is in features/gateway/directory.tsx: a group per part of the
 * router, a tile per way of building it. It replaced the interface cards and
 * the IPv4/IPv6 stacks, which answered "is this working" on the section's own
 * page; that answer is one level down now, on the page each tile opens, beside
 * the switches that change it.
 */
export function GatewayPage() {
  return <GatewayDirectory />
}
