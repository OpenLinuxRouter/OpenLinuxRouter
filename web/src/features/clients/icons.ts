import {
  AirVent,
  Bell,
  BookOpen,
  Bot,
  CircuitBoard,
  Droplets,
  EthernetPort,
  Fan,
  Gamepad2,
  GlassWater,
  HardDrive,
  Hexagon,
  HelpCircle,
  Laptop as LaptopGlyph,
  Lightbulb,
  Monitor,
  Network,
  Plug,
  Printer as PrinterGlyph,
  RadioReceiver,
  Refrigerator,
  Router as RouterGlyph,
  Server,
  Shirt,
  Smartphone,
  Speaker,
  Tablet as TabletGlyph,
  Thermometer,
  Tv as TvGlyph,
  Video,
  WashingMachine,
  Watch,
  Wifi,
  type LucideIcon,
} from 'lucide-react'

import type { VendorKey } from '@/lib/api-types'
import type { ClientCategory } from '@/lib/config-types'

import { brandLogoSrc, brandLogoSlug } from '@/features/brand-logos/brand-logos'
import { type BrandGroup, brandGroups } from '@/features/clients/brand-groups'
import { searchIcons } from '@/features/clients/icon-search'

import accessPoint from '@/assets/device-icons/accesspoint.webp'
import aircon from '@/assets/device-icons/aircon.webp'
import airer from '@/assets/device-icons/airer.webp'
import airPurifier from '@/assets/device-icons/airpurifier.webp'
import camera from '@/assets/device-icons/camera.webp'
import gameConsole from '@/assets/device-icons/console.webp'
import desktop from '@/assets/device-icons/desktop.webp'
import desktopMini from '@/assets/device-icons/desktop-mini.webp'
import doorbell from '@/assets/device-icons/doorbell.webp'
import ereader from '@/assets/device-icons/ereader.webp'
import fridge from '@/assets/device-icons/fridge.webp'
import hub from '@/assets/device-icons/hub.webp'
import humidifier from '@/assets/device-icons/humidifier.webp'
import laptop from '@/assets/device-icons/laptop.webp'
import light from '@/assets/device-icons/light.webp'
import modem from '@/assets/device-icons/modem.webp'
import nas from '@/assets/device-icons/nas.webp'
import phone from '@/assets/device-icons/phone.webp'
import plug from '@/assets/device-icons/plug.webp'
import printer from '@/assets/device-icons/printer.webp'
import router from '@/assets/device-icons/router.webp'
import sbc from '@/assets/device-icons/sbc.webp'
import sensor from '@/assets/device-icons/sensor.webp'
import server from '@/assets/device-icons/server.webp'
import serverTower from '@/assets/device-icons/server-tower.webp'
import speaker from '@/assets/device-icons/speaker.webp'
import networkSwitch from '@/assets/device-icons/switch.webp'
import tablet from '@/assets/device-icons/tablet.webp'
import thermostat from '@/assets/device-icons/thermostat.webp'
import tv from '@/assets/device-icons/tv.webp'
import unknown from '@/assets/device-icons/unknown.webp'
import vacuum from '@/assets/device-icons/vacuum.webp'
import washer from '@/assets/device-icons/washer.webp'
import waterPurifier from '@/assets/device-icons/waterpurifier.webp'
import watch from '@/assets/device-icons/watch.webp'
import amazonSpeaker from '@/assets/device-icons/amazon-speaker.webp'
import appleDesktop from '@/assets/device-icons/apple-desktop.webp'
import appleDesktopMini from '@/assets/device-icons/apple-desktop-mini.webp'
import appleDesktopStudio from '@/assets/device-icons/apple-desktop-studio.webp'
import appleLaptop from '@/assets/device-icons/apple-laptop.webp'
import applePhone from '@/assets/device-icons/apple-phone.webp'
import appleSpeaker from '@/assets/device-icons/apple-speaker.webp'
import appleTablet from '@/assets/device-icons/apple-tablet.webp'
import appleTv from '@/assets/device-icons/apple-tv.webp'
import appleWatch from '@/assets/device-icons/apple-watch.webp'
import brotherPrinter from '@/assets/device-icons/brother-printer.webp'
import canonPrinter from '@/assets/device-icons/canon-printer.webp'
import epsonPrinter from '@/assets/device-icons/epson-printer.webp'
import googlePhone from '@/assets/device-icons/google-phone.webp'
import googleSpeaker from '@/assets/device-icons/google-speaker.webp'
import hpPrinter from '@/assets/device-icons/hp-printer.webp'
import huaweiLaptop from '@/assets/device-icons/huawei-laptop.webp'
import huaweiPhone from '@/assets/device-icons/huawei-phone.webp'
import huaweiTablet from '@/assets/device-icons/huawei-tablet.webp'
import huaweiWatch from '@/assets/device-icons/huawei-watch.webp'
import intelDesktop from '@/assets/device-icons/intel-desktop.webp'
import intelDesktopNuc12Extreme from '@/assets/device-icons/intel-desktop-nuc12extreme.webp'
import ikoolcoreR1 from '@/assets/device-icons/ikoolcore-desktop-r1.webp'
import ikoolcoreR2 from '@/assets/device-icons/ikoolcore-desktop-r2.webp'
import ikoolcoreR2Max from '@/assets/device-icons/ikoolcore-desktop-r2max.webp'
import lenovoLaptop from '@/assets/device-icons/lenovo-laptop.webp'
import lgTv from '@/assets/device-icons/lg-tv.webp'
import microsoftConsole from '@/assets/device-icons/microsoft-console.webp'
import microsoftLaptop from '@/assets/device-icons/microsoft-laptop.webp'
import nintendoConsole from '@/assets/device-icons/nintendo-console.webp'
import raspberryPiSbc from '@/assets/device-icons/raspberry-pi-sbc.webp'
import samsungFridge from '@/assets/device-icons/samsung-fridge.webp'
import samsungPhone from '@/assets/device-icons/samsung-phone.webp'
import samsungTablet from '@/assets/device-icons/samsung-tablet.webp'
import samsungTv from '@/assets/device-icons/samsung-tv.webp'
import samsungWasher from '@/assets/device-icons/samsung-washer.webp'
import samsungWatch from '@/assets/device-icons/samsung-watch.webp'
import sonosSpeaker from '@/assets/device-icons/sonos-speaker.webp'
import sonyConsole from '@/assets/device-icons/sony-console.webp'
import sonyPhone from '@/assets/device-icons/sony-phone.webp'
import sonyTv from '@/assets/device-icons/sony-tv.webp'
import synologyNas from '@/assets/device-icons/synology-nas.webp'
import synologyNas1Bay from '@/assets/device-icons/synology-nas-1bay.webp'
import synologyNas3Bay from '@/assets/device-icons/synology-nas-3bay.webp'
import synologyNas4Bay from '@/assets/device-icons/synology-nas-4bay.webp'
import synologyNas5Bay from '@/assets/device-icons/synology-nas-5bay.webp'
import synologyNas6Bay from '@/assets/device-icons/synology-nas-6bay.webp'
import synologyNas8Bay from '@/assets/device-icons/synology-nas-8bay.webp'
import synologyNas12Bay from '@/assets/device-icons/synology-nas-12bay.webp'
import tpLinkRouter from '@/assets/device-icons/tp-link-router.webp'
import tpLinkRouterXdr6080 from '@/assets/device-icons/tp-link-router-xdr6080.webp'
import ubiquitiAccesspoint from '@/assets/device-icons/ubiquiti-accesspoint.webp'
import xiaomiPhone from '@/assets/device-icons/xiaomi-phone.webp'
import xiaomiRouterBe7000 from '@/assets/device-icons/xiaomi-router-be7000.webp'
import xiaomiSpeaker from '@/assets/device-icons/xiaomi-speaker.webp'
import xiaomiVacuum from '@/assets/device-icons/xiaomi-vacuum.webp'

import osAndroid from '@/assets/os-marks/android.svg'
import osAppleDark from '@/assets/os-marks/apple-dark.svg'
import osAppleLight from '@/assets/os-marks/apple-light.svg'
import osArch from '@/assets/os-marks/archlinux.svg'
import osDebian from '@/assets/os-marks/debian.svg'
import osFedora from '@/assets/os-marks/fedora.svg'
import osFreeBSD from '@/assets/os-marks/freebsd.svg'
import osHarmony from '@/assets/os-marks/harmonyos.svg'
import osLinux from '@/assets/os-marks/linux.svg'
import osOpenWrt from '@/assets/os-marks/openwrt.svg'
import osProxmox from '@/assets/os-marks/proxmox.svg'
import osRaspberryPi from '@/assets/os-marks/raspberrypi.svg'
import osUbuntu from '@/assets/os-marks/ubuntu.svg'
import osWindows from '@/assets/os-marks/windows.svg'

// The picture, word and glyph maps.
//
// LABELS and GLYPHS are keyed by ClientCategory, which is *generated* from the
// Go enum (config-types.ts, via the schema in internal/clients/schema.go). The
// maps are typed differently on purpose, and the difference is the whole
// design:
//
//   LABELS is Record<ClientCategory, string>   — total, checked at compile time
//   IMAGES is Partial<Record<IconKey, …>>      — sparse, filled in over time
//
// So adding a category in Go and regenerating breaks `npm run build` until it
// has a *word*, but never until it has a *picture*. That is what lets the icon
// set grow gradually while the list stays correct: an unillustrated category
// shows the fallback image beside its own proper label, rather than showing the
// right picture for the wrong thing or the string "accesspoint".
//
// Adding an image is therefore one file and one line here. Nothing else moves.
//
// IMAGES is keyed more finely than the other two, by IconKey rather than by
// category, because a vendor narrows a picture where a category cannot: a
// laptop is a laptop, but an Apple laptop is a specific-looking object. Only
// pictures gain from that. A *label* for `apple/laptop` would be worse than
// "Laptop" beside a vendor name the row is already showing, and a vendor has no
// line glyph at all. See deviceIcon for the order they resolve in.
//
// Imported through vite rather than referenced from public/, so the assets are
// content-hashed into assets/ and get the immutable cache header that
// internal/webui/webui.go already sets for that prefix. It also means a missing
// file is a build error rather than a 404 nobody notices.

/**
 * A key into IMAGES: a category on its own, a vendor's take on one, one shape
 * of either, or a vendor with nothing else known about the device.
 *
 * The template literal type is doing real work. `apple/laptop` is checked
 * against both vocabularies at compile time, so a key cannot name a vendor that
 * does not exist or a category that has since been renamed. For strings that
 * are also filenames, that is the difference between a build error and a
 * picture which silently never loads.
 */
type IconKey = ClientCategory | VendorKey | `${VendorKey}/${ClientCategory}` | VariantKey

/**
 * A shape of a kind of device too different to share its picture — a NAS with
 * eight bays, a Mac mini beside the iMac. Hand-mirrored from `variants` in
 * internal/clients/icon.go, for the reason OsKey is: the icon field is
 * published as a pattern, so nothing generates this list.
 */
type VariantKey =
  | 'synology/nas/1bay'
  | 'synology/nas/3bay'
  | 'synology/nas/4bay'
  | 'synology/nas/5bay'
  | 'synology/nas/6bay'
  | 'synology/nas/8bay'
  | 'synology/nas/12bay'
  // Retired, and still drawn: the bay-count family used to hang off the
  // category. A config that stored one keeps its picture — see MOVED_ICONS —
  // but nothing offers them any more.
  | 'nas/4bay'
  | 'nas/5bay'
  | 'nas/6bay'
  | 'nas/8bay'
  | 'apple/desktop/mini'
  | 'apple/desktop/studio'
  | 'server/tower'
  | 'desktop/mini'
  | 'intel/desktop/nuc12extreme'
  | 'xiaomi/router/be7000'
  | 'tp-link/router/xdr6080'
  | 'desktop/ikoolcorer1'
  | 'desktop/ikoolcorer2'
  | 'desktop/ikoolcorer2max'

const IMAGES: Partial<Record<IconKey, string>> = {
  accesspoint: accessPoint,
  aircon,
  airer,
  airpurifier: airPurifier,
  camera,
  console: gameConsole,
  desktop,
  doorbell,
  ereader,
  fridge,
  hub,
  humidifier,
  laptop,
  light,
  modem,
  nas,
  phone,
  plug,
  printer,
  router,
  sbc,
  sensor,
  server,
  speaker,
  switch: networkSwitch,
  tablet,
  thermostat,
  tv,
  unknown,
  vacuum,
  washer,
  waterpurifier: waterPurifier,
  watch,

  'server/tower': serverTower,
  'desktop/mini': desktopMini,
  'desktop/ikoolcorer1': ikoolcoreR1,
  'desktop/ikoolcorer2': ikoolcoreR2,
  'desktop/ikoolcorer2max': ikoolcoreR2Max,

  // Grouped by maker in the picker, not listed flat — see BRAND_GROUPS below.
  // The order here still matters: it breaks ties between brands holding the
  // same number of pictures, and it is the order within each brand's section.
  'apple/phone': applePhone,
  'apple/tablet': appleTablet,
  'apple/laptop': appleLaptop,
  'apple/desktop': appleDesktop,
  'apple/desktop/mini': appleDesktopMini,
  'apple/desktop/studio': appleDesktopStudio,
  'apple/watch': appleWatch,
  'apple/tv': appleTv,
  'apple/speaker': appleSpeaker,

  'samsung/phone': samsungPhone,
  'samsung/tablet': samsungTablet,
  'samsung/watch': samsungWatch,
  'samsung/tv': samsungTv,
  'samsung/fridge': samsungFridge,
  'samsung/washer': samsungWasher,

  'huawei/phone': huaweiPhone,
  'huawei/tablet': huaweiTablet,
  'huawei/laptop': huaweiLaptop,
  'huawei/watch': huaweiWatch,

  'xiaomi/phone': xiaomiPhone,
  'xiaomi/router/be7000': xiaomiRouterBe7000,
  'xiaomi/speaker': xiaomiSpeaker,
  'xiaomi/vacuum': xiaomiVacuum,

  'sony/phone': sonyPhone,
  'sony/tv': sonyTv,
  'sony/console': sonyConsole,

  'google/phone': googlePhone,
  'google/speaker': googleSpeaker,

  'microsoft/laptop': microsoftLaptop,
  'microsoft/console': microsoftConsole,

  // Beside the other box-makers, not with the phone vendors: the NUC is the
  // one thing Intel sells that anyone has in a house, and it is a desk.
  'intel/desktop': intelDesktop,
  'intel/desktop/nuc12extreme': intelDesktopNuc12Extreme,

  'lenovo/laptop': lenovoLaptop,
  'lg/tv': lgTv,
  'nintendo/console': nintendoConsole,
  'amazon/speaker': amazonSpeaker,
  'sonos/speaker': sonosSpeaker,

  // The NAS family is Synology's, drawn as one product line and listed here in
  // bay order. The two-bay box is the vendor picture itself, so a detected
  // Synology NAS wears something right before anyone knows how many bays it
  // has. One bay is narrower than the rest; the wide ones are the same box
  // with more caddies, which is exactly what a bay count means.
  'synology/nas/1bay': synologyNas1Bay,
  'synology/nas': synologyNas,
  'synology/nas/3bay': synologyNas3Bay,
  'synology/nas/4bay': synologyNas4Bay,
  'synology/nas/5bay': synologyNas5Bay,
  'synology/nas/6bay': synologyNas6Bay,
  'synology/nas/8bay': synologyNas8Bay,
  'synology/nas/12bay': synologyNas12Bay,
  'raspberry-pi/sbc': raspberryPiSbc,
  'tp-link/router': tpLinkRouter,
  'tp-link/router/xdr6080': tpLinkRouterXdr6080,
  'ubiquiti/accesspoint': ubiquitiAccesspoint,
  'hp/printer': hpPrinter,
  'epson/printer': epsonPrinter,
  'canon/printer': canonPrinter,
  'brother/printer': brotherPrinter,
}

/**
 * The operator-facing word for a category.
 *
 * Total by construction. If this stops compiling, a category was added to the
 * Go enum and needs a word here — which is the intended prompt, not an
 * obstacle.
 */
export const LABELS: Record<ClientCategory, string> = {
  '': 'Not set',
  unknown: 'Unknown',

  phone: 'Phone',
  tablet: 'Tablet',
  laptop: 'Laptop',
  desktop: 'Desktop',
  watch: 'Watch',
  ereader: 'E-reader',

  tv: 'TV',
  speaker: 'Speaker',
  console: 'Games console',

  camera: 'Camera',
  doorbell: 'Doorbell',
  thermostat: 'Thermostat',
  sensor: 'Sensor',
  plug: 'Smart plug',
  light: 'Light',
  vacuum: 'Vacuum',

  airpurifier: 'Air purifier',
  waterpurifier: 'Water purifier',
  humidifier: 'Humidifier',
  aircon: 'Air conditioner',
  airer: 'Clothes airer',
  fridge: 'Fridge',
  washer: 'Washing machine',

  printer: 'Printer',
  nas: 'Network storage',
  server: 'Server',
  sbc: 'Single-board computer',

  modem: 'Modem',
  router: 'Router',
  accesspoint: 'Access point',
  switch: 'Network switch',
  hub: 'Smart home hub',
}

/**
 * A line glyph per category, for the ones with no photograph yet.
 *
 * This is what makes a partly-filled icon set look deliberate instead of
 * broken. The alternative — falling back to the unknown *photograph* — puts the
 * same grey box on a doorbell, a speaker and a smart plug, so a list of eight
 * devices reads as a rendering failure and the picker becomes unusable: a dozen
 * identical tiles distinguished only by their caption.
 *
 * A glyph is honestly a different kind of thing. It says "no picture yet" while
 * still telling the operator what the row is, and it never impersonates a
 * device we do not have artwork for.
 *
 * Total by construction, like LABELS: a new category needs a glyph, and gets a
 * usable row the moment it exists rather than when someone gets round to
 * rendering it.
 */
export const GLYPHS: Record<ClientCategory, LucideIcon> = {
  '': HelpCircle,
  unknown: HelpCircle,

  phone: Smartphone,
  tablet: TabletGlyph,
  laptop: LaptopGlyph,
  desktop: Monitor,
  watch: Watch,
  ereader: BookOpen,

  tv: TvGlyph,
  speaker: Speaker,
  console: Gamepad2,

  camera: Video,
  doorbell: Bell,
  thermostat: Thermometer,
  sensor: RadioReceiver,
  plug: Plug,
  light: Lightbulb,
  vacuum: Bot,

  airpurifier: Fan,
  waterpurifier: GlassWater,
  humidifier: Droplets,
  aircon: AirVent,
  airer: Shirt,
  fridge: Refrigerator,
  washer: WashingMachine,

  printer: PrinterGlyph,
  nas: HardDrive,
  server: Server,
  sbc: CircuitBoard,

  modem: EthernetPort,
  router: RouterGlyph,
  accesspoint: Wifi,
  switch: Network,
  hub: Hexagon,
}

/**
 * The photograph for a device, or undefined when nothing has been drawn for it
 * yet.
 *
 * Two axes, each falling back independently — most specific first:
 *
 *   1. `apple/laptop`  both known, and the picture can be both
 *   2. `apple`         the vendor, when nothing says what kind of thing it is
 *   3. `laptop`        the category, when the vendor has no artwork
 *   4. undefined       neither, and the caller decides
 *
 * Rung 2 sits above rung 3 only when there is no category to use, which is why
 * it is reached through `category === 'unknown'` rather than by ordering alone.
 * A picture of the right *kind of thing* beats one of the right *brand*: an
 * operator scanning a list is looking for their printer, not for Brother.
 *
 * Undefined rather than a fallback image, so the caller has to decide what to
 * show; that decision is what keeps an unillustrated device looking intentional
 * rather than broken. See DeviceIcon.
 *
 * `vendorMarked` says the caller has a vendor mark it would rather show than the
 * anonymous `unknown` photograph. Without it this function has a hole that is
 * easy to miss and was: `unknown` *is* registered in IMAGES, so an uncategorised
 * device returns the grey box at rung 3 and never reaches the caller's fallback
 * at all. Six devices from six different makers came out as six identical
 * boxes, which is the exact screen this whole mechanism exists to fix.
 *
 * Per-model artwork (ICONS.md tier 2) would sit above all of these. It has no
 * assets and no key vocabulary yet, so it is not wired in — adding it means one
 * more rung at the top and nothing else.
 */
export function deviceIcon(
  category: ClientCategory,
  vendor?: VendorKey,
  vendorMarked = false,
): string | undefined {
  // 'unknown' and '' are both "nobody has said" — see Category in
  // internal/clients/category.go, where the two differ in provenance but not in
  // how much either tells a picture.
  const uncategorised = category === 'unknown' || category === ''

  if (vendor) {
    const both = IMAGES[`${vendor}/${category}`]
    if (both) return both

    if (uncategorised) {
      const own = IMAGES[vendor]
      if (own) return own
    }
  }

  // The grey box is the answer for a device nothing is known about. A device
  // whose maker is known is not that device, even when nobody has drawn it.
  if (uncategorised && vendorMarked) return undefined

  return IMAGES[category]
}

// hasOwnIcon(category) used to live here and had no callers. Rather than grow
// it a vendor argument nobody would pass, it is gone: `deviceIcon(...) !==
// undefined` is what it was, and is shorter than importing it.

/**
 * A vendor's name reduced to a mark that survives at 32 pixels.
 *
 * This is what a device with a vendor and no category gets instead of a
 * picture, and it is not a consolation prize — it is the only answer available.
 * Artwork would mean drawing "an Apple something", which means choosing between
 * a phone, a watch and a laptop, which is inventing information; ICONS.md and
 * detect.go both refuse to do that. Artwork also only ever covers the few dozen
 * vendors somebody has drawn, where the registry knows thirty thousand. Letters
 * cover all of them.
 *
 * The rules, in order, each earning its place on a name that really occurs:
 *
 *   1. A short all-capitals first word *is* the mark already: "WNC", "ZTE",
 *      "HP", "LG". Because the split happens on the hyphen too, "TP-Link"
 *      gives "TP" rather than the "TL" nobody would recognise.
 *   2. Two or more words give their first two initials: "Raspberry Pi" → RP,
 *      "Philips Hue" → PH, "Texas Instruments" → TI.
 *   3. One word gives its first two letters: "Huawei" → HU, "eero" → EE.
 *
 * Upper-cased at the end rather than preserved, because a column of these is
 * scanned for difference rather than read for spelling, and "eE" beside "HU"
 * reads as a bug.
 */
export function vendorInitials(vendor?: string): string {
  if (!vendor) return ''

  // Split on anything that is not a letter or digit, so hyphens, dots and
  // ampersands all separate words: "TP-Link", "D-Link", "Routerboard.com".
  const words = vendor.split(/[^A-Za-z0-9]+/).filter(Boolean)
  if (words.length === 0) return ''

  const first = words[0]
  if (first.length <= 4 && first === first.toUpperCase()) return first
  if (words.length >= 2) return (first[0] + words[1][0]).toUpperCase()
  return first.slice(0, 2).toUpperCase()
}

/** The line glyph for a category, used wherever there is no photograph. */
export function deviceGlyph(category: ClientCategory): LucideIcon {
  return GLYPHS[category] ?? HelpCircle
}

/** The word for a category, for anywhere a label is needed. */
export function categoryLabel(category: ClientCategory): string {
  return LABELS[category] ?? LABELS.unknown
}

/**
 * The vocabulary in reading order, grouped the way an operator thinks about a
 * network rather than alphabetically. Mirrors the order in
 * internal/clients/category.go.
 *
 * The groups are *only* an order now: the picker draws one flat Common grid
 * (see COMMON_TILES), so the headings these groups used to own are gone. The
 * grouping survives because it still decides what sits next to what — the
 * phones before the appliances, the grey box last — and because a heading can
 * come back without the order having to be rediscovered.
 *
 * "Not set" is omitted: clearing a category is a distinct action, not a value
 * to pick from a list.
 */
export const CATEGORY_ORDER: ClientCategory[][] = [
  ['phone', 'tablet', 'laptop', 'desktop', 'watch', 'ereader'],
  ['tv', 'speaker', 'console'],
  ['camera', 'doorbell', 'thermostat', 'sensor', 'plug', 'light', 'vacuum'],
  ['airpurifier', 'waterpurifier', 'humidifier', 'aircon', 'airer', 'fridge', 'washer'],
  ['printer', 'nas', 'server', 'sbc'],
  ['modem', 'router', 'accesspoint', 'switch', 'hub'],
  ['unknown'],
]

/**
 * An operating system with a mark — internal/devices operatingSystems, in the
 * same order. Hand-mirrored for the reason VendorKey is: the icon field is
 * validated in Go but published as a pattern, not an enum, so nothing
 * generates this list.
 */
export type OsKey =
  | 'windows'
  | 'apple'
  | 'android'
  | 'harmonyos'
  | 'linux'
  | 'ubuntu'
  | 'debian'
  | 'fedora'
  | 'archlinux'
  | 'freebsd'
  | 'proxmox'
  | 'openwrt'
  | 'raspberrypi'

/**
 * The OS marks, in full colour, from thesvg.org (MIT). Logos, which ICONS.md
 * otherwise forbids — see "Operating-system marks" there for why these are
 * the exception.
 *
 * `dark` is the variant for a dark theme, for the one mark that is a single
 * colour and would vanish on its own background: Apple's is black on light
 * and white on dark. HarmonyOS wears Huawei's flower, because its own mark is
 * the wordmark "HMOS", which is unreadable at 32 pixels and black besides.
 */
export const OS_MARKS: Record<OsKey, { label: string; src: string; dark?: string }> = {
  windows: { label: 'Windows', src: osWindows },
  apple: { label: 'macOS / iOS', src: osAppleLight, dark: osAppleDark },
  android: { label: 'Android', src: osAndroid },
  harmonyos: { label: 'HarmonyOS', src: osHarmony },
  linux: { label: 'Linux', src: osLinux },
  ubuntu: { label: 'Ubuntu', src: osUbuntu },
  debian: { label: 'Debian', src: osDebian },
  fedora: { label: 'Fedora', src: osFedora },
  archlinux: { label: 'Arch Linux', src: osArch },
  freebsd: { label: 'FreeBSD', src: osFreeBSD },
  proxmox: { label: 'Proxmox', src: osProxmox },
  openwrt: { label: 'OpenWrt', src: osOpenWrt },
  raspberrypi: { label: 'Raspberry Pi OS', src: osRaspberryPi },
}

export const OS_KEYS = Object.keys(OS_MARKS) as OsKey[]

/**
 * What a stored `icon` draws, or undefined when it names nothing drawable —
 * an Apple TV nobody has painted yet, say, which then falls back to the
 * category's own picture exactly as if no icon were set.
 */
export type ChosenIcon =
  | { kind: 'photo'; src: string }
  | { kind: 'os'; src: string; dark?: string; label: string }

export function chosenIcon(icon?: string): ChosenIcon | undefined {
  if (!icon) return undefined
  if (brandLogoSlug(icon)) {
    const slug = brandLogoSlug(icon)
    return { kind: 'os', label: slug, src: brandLogoSrc(slug) }
  }
  if (icon.startsWith('os/')) {
    const mark = OS_MARKS[icon.slice(3) as OsKey]
    return mark && { kind: 'os', ...mark }
  }
  const moved = MOVED_ICONS[icon]
  const src = IMAGES[icon as IconKey] ?? (moved ? IMAGES[moved] : undefined)
  return src ? { kind: 'photo', src } : undefined
}

/**
 * Values that still resolve, under a picture that has moved.
 *
 * When the NAS bay-count family was filed under Synology, the drawings
 * travelled with it: `nas/4bay` and `synology/nas/4bay` are the same box. A
 * config written before the move is still valid — the vocabulary keeps the old
 * keys for exactly that reason — so this is the one place that has to know
 * where a picture went. Nothing new is written with the old name: the picker
 * cannot offer a key that is no longer in IMAGES.
 */
const MOVED_ICONS: Partial<Record<string, IconKey>> = {
  'nas/4bay': 'synology/nas/4bay',
  'nas/5bay': 'synology/nas/5bay',
  'nas/6bay': 'synology/nas/6bay',
  'nas/8bay': 'synology/nas/8bay',
}

/**
 * The words for a vendor's own pictures, where the vendor has a name for the
 * thing that says more than "<Vendor> <category>". Only pictures that exist
 * are offered, so this is keyed like IMAGES and just as sparse.
 */
const BRAND_LABELS: Partial<Record<IconKey, string>> = {
  'apple/phone': 'iPhone',
  'apple/tablet': 'iPad',
  'apple/laptop': 'MacBook',
  'apple/desktop': 'iMac',
  'apple/desktop/mini': 'Mac mini',
  'apple/desktop/studio': 'Mac Studio',
  'apple/watch': 'Apple Watch',
  'apple/tv': 'Apple TV',
  'apple/speaker': 'HomePod',

  'samsung/phone': 'Galaxy phone',
  'samsung/tablet': 'Galaxy Tab',
  'samsung/watch': 'Galaxy Watch',
  'samsung/tv': 'Samsung TV',
  'samsung/fridge': 'Samsung fridge',
  'samsung/washer': 'Samsung washer',

  'huawei/phone': 'Huawei phone',
  'huawei/tablet': 'MatePad',
  'huawei/laptop': 'MateBook',
  'huawei/watch': 'Huawei Watch',

  'xiaomi/phone': 'Xiaomi phone',
  'xiaomi/speaker': 'Xiaomi speaker',
  'xiaomi/vacuum': 'Xiaomi vacuum',

  'sony/phone': 'Xperia',
  'sony/tv': 'Bravia',
  'sony/console': 'PlayStation',

  'google/phone': 'Pixel',
  'google/speaker': 'Nest speaker',

  'microsoft/laptop': 'Surface',
  'microsoft/console': 'Xbox',
  'intel/desktop': 'Intel NUC',
  'intel/desktop/nuc12extreme': 'Intel NUC 12 Extreme',
  'xiaomi/router/be7000': 'Xiaomi BE7000',
  'tp-link/router/xdr6080': 'TP-Link TL-XDR6080 Turbo',

  'lenovo/laptop': 'ThinkPad',
  'lg/tv': 'LG TV',
  'nintendo/console': 'Switch',
  'amazon/speaker': 'Echo',
  'sonos/speaker': 'Sonos',

  'synology/nas': '2-bay',
  'synology/nas/1bay': '1-bay',
  'synology/nas/3bay': '3-bay',
  'synology/nas/4bay': '4-bay',
  'synology/nas/5bay': '5-bay',
  'synology/nas/6bay': '6-bay',
  'synology/nas/8bay': '8-bay',
  'synology/nas/12bay': '12-bay',
  'raspberry-pi/sbc': 'Raspberry Pi',
  'tp-link/router': 'TP-Link router',
  'ubiquiti/accesspoint': 'UniFi AP',
  'hp/printer': 'HP printer',
  'epson/printer': 'Epson printer',
  'canon/printer': 'Canon printer',
  'brother/printer': 'Brother printer',
}

/** What a picker tile stores: the picture, and the category it implies. */
export interface IconChoiceTile {
  icon: string
  category: ClientCategory
  label: string
}

/**
 * What a maker is called, for the heading over their pictures.
 *
 * Total by construction over VendorKey, for the same reason LABELS is total
 * over ClientCategory: it is the prompt for a name, not a place to remember one.
 * A brand is a proper noun and is *not* translated — Apple is Apple in a Chinese
 * UI, and the zh-CN catalogue has no entry for it — but it still has to be
 * spelled here rather than derived from the slug, because a slug is a filename
 * and its shape leaks: "Tp-link", "Raspberry-pi", "Philips-hue". Deriving a
 * name from a key is how the picker used to caption a vendor's fallback tile,
 * and it was wrong for exactly the makers whose names have a hyphen in them.
 */
const BRAND_NAMES: Record<VendorKey, string> = {
  acer: 'Acer',
  amazon: 'Amazon',
  apple: 'Apple',
  asus: 'Asus',
  brother: 'Brother',
  canon: 'Canon',
  cisco: 'Cisco',
  dell: 'Dell',
  epson: 'Epson',
  espressif: 'Espressif',
  google: 'Google',
  hp: 'HP',
  hpe: 'HPE',
  huawei: 'Huawei',
  intel: 'Intel',
  lenovo: 'Lenovo',
  lg: 'LG',
  microsoft: 'Microsoft',
  mikrotik: 'MikroTik',
  netgear: 'Netgear',
  nintendo: 'Nintendo',
  'philips-hue': 'Philips Hue',
  qnap: 'QNAP',
  'raspberry-pi': 'Raspberry Pi',
  roku: 'Roku',
  samsung: 'Samsung',
  sonos: 'Sonos',
  sony: 'Sony',
  synology: 'Synology',
  'tp-link': 'TP-Link',
  ubiquiti: 'Ubiquiti',
  xiaomi: 'Xiaomi',
  zyxel: 'Zyxel',
}

/**
 * Every vendor picture that has been drawn, as picker tiles. Derived from
 * IMAGES rather than listed, so a new `huawei/phone` shows up in the picker
 * the moment its line is added there. A vendor's variant — the Mac mini — is
 * one of these too: it is Apple's, not every desktop's.
 *
 * Not exported: the picker's unit is a brand section, not a tile, and a flat
 * list is exactly the thing BRAND_GROUPS exists to stop it from rendering.
 */
const BRAND_TILES: IconChoiceTile[] = (Object.keys(IMAGES) as IconKey[])
  .filter((key) => key.includes('/') && !(key.split('/')[0] in LABELS))
  .map((key) => {
    const [vendor, category] = key.split('/') as [VendorKey, ClientCategory]
    return {
      icon: key,
      category,
      label: BRAND_LABELS[key] ?? `${BRAND_NAMES[vendor]} ${LABELS[category].toLowerCase()}`,
    }
  })

/**
 * The brand sections of the picker, in the order they are shown: the makers
 * with several pictures each, biggest first, then one section for the makers
 * with a single picture.
 *
 * Derived, so adding a picture is still one file and one line in IMAGES. The
 * rule — and why one picture does not earn a heading — is in brand-groups.ts.
 *
 * The last section's label is a message rather than a brand, which is why it is
 * the one label the picker passes through `t()`; the vendor names it does not.
 */
export const BRAND_GROUPS: BrandGroup<IconChoiceTile>[] = brandGroups(
  BRAND_TILES,
  (tile) => tile.icon.split('/')[0],
  (vendor) => BRAND_NAMES[vendor as VendorKey] ?? vendor,
  'Other brands',
)

const VARIANT_LABELS: Partial<Record<VariantKey, string>> = {
  'server/tower': 'Tower server',
  'desktop/mini': 'Mini PC',
  'desktop/ikoolcorer1': 'iKOOLCORE R1',
  'desktop/ikoolcorer2': 'iKOOLCORE R2',
  'desktop/ikoolcorer2max': 'iKOOLCORE R2 Max',
}

/**
 * Category variants, shown in the picker right after the category's own tile:
 * the tower server beside Server, the mini PC beside Desktop. They are shapes,
 * not products — which is why the NAS bay counts are not here: those are
 * Synology's, and hang off the maker instead (see ICONS.md).
 */
export function variantChoices(category: ClientCategory): IconChoiceTile[] {
  return (Object.keys(IMAGES) as IconKey[])
    .filter((key) => key.startsWith(`${category}/`))
    .map((key) => ({ icon: key, category, label: VARIANT_LABELS[key as VariantKey] ?? key }))
}

/**
 * The extra words a picture answers to, on top of its own name.
 *
 * A tile already names itself, its maker and its kind, and that covers most of
 * what anyone types. It does not cover the words that belong to the *thing*
 * rather than to the picture of it: somebody hunting for their NAS types
 * "storage", somebody hunting for the box in the cupboard types "rack", and
 * somebody hunting for the plug types "socket". None of those words is written
 * on a tile, and a search that cannot find a NAS by the word "storage" is a
 * search nobody uses twice.
 *
 * Keyed by the same IconKey IMAGES is, so a tag cannot name a picture that does
 * not exist. That failure would be silent otherwise: a mistyped key is a tag
 * nobody ever reads.
 */
const ICON_TAGS: Partial<Record<IconKey, readonly string[]>> = {
  phone: ['smartphone', 'mobile', 'cellular', 'handset'],
  tablet: ['pad', 'slate'],
  laptop: ['notebook', 'computer', 'portable'],
  desktop: ['pc', 'computer', 'workstation', 'tower'],
  watch: ['smartwatch', 'wearable'],
  ereader: ['ebook', 'kindle', 'reading'],
  tv: ['television', 'screen', 'display'],
  speaker: ['audio', 'sound', 'smart speaker'],
  console: ['gaming', 'game', 'playstation', 'xbox'],
  camera: ['cctv', 'surveillance', 'security', 'ip camera'],
  doorbell: ['bell', 'entry', 'video doorbell'],
  thermostat: ['heating', 'hvac', 'climate', 'radiator'],
  sensor: ['motion', 'contact', 'temperature', 'humidity', 'leak'],
  plug: ['socket', 'outlet', 'relay', 'power', 'smart plug'],
  light: ['lamp', 'bulb', 'lighting'],
  vacuum: ['robot vacuum', 'robovac', 'cleaner'],
  airpurifier: ['air cleaner', 'hepa', 'purifier'],
  waterpurifier: ['water filter', 'reverse osmosis', 'ro'],
  humidifier: ['moisture', 'mist'],
  aircon: ['air conditioner', 'ac', 'cooling', 'hvac', 'heat pump'],
  airer: ['drying rack', 'laundry', 'clothes'],
  fridge: ['refrigerator', 'freezer'],
  washer: ['washing machine', 'laundry', 'dryer'],
  printer: ['scanner', 'inkjet', 'laser', 'mfp', 'print'],
  nas: ['network storage', 'storage', 'backup', 'file server', 'disk'],
  server: ['host', 'rack', 'rackmount', 'bare metal'],
  sbc: ['single board computer', 'raspberry pi', 'arduino', 'esp32', 'board'],
  modem: ['dsl', 'cable', 'isp', 'fibre'],
  router: ['gateway', 'wifi', 'wireless', 'routing'],
  accesspoint: ['wifi', 'wireless', 'mesh'],
  switch: ['ethernet', 'lan', 'network switch'],
  hub: ['zigbee', 'zwave', 'thread', 'matter', 'bridge', 'home assistant'],
  unknown: ['other', 'generic', 'unrecognised'],

  'nas/4bay': ['four bay'],
  'nas/5bay': ['five bay'],
  'nas/6bay': ['six bay'],
  'nas/8bay': ['eight bay'],
  'desktop/mini': ['small', 'sff'],
  'desktop/ikoolcorer1': ['mini pc', 'firewall box'],
  'desktop/ikoolcorer2': ['mini pc', 'firewall box'],
  'desktop/ikoolcorer2max': ['mini pc', 'firewall box'],

  'apple/phone': ['iphone'],
  'apple/tablet': ['ipad', 'ipad pro'],
  'apple/laptop': ['macbook', 'macbook air', 'macbook pro', 'mac'],
  'apple/desktop': ['imac', 'mac'],
  'apple/desktop/mini': ['mac mini', 'mini pc', 'mac'],
  'apple/desktop/studio': ['mac studio', 'mac'],
  'apple/watch': ['apple watch'],
  'apple/tv': ['apple tv', 'set top box'],
  'apple/speaker': ['homepod'],
  'samsung/phone': ['galaxy', 'android'],
  'google/phone': ['pixel', 'android'],
  'microsoft/console': ['xbox'],
  'microsoft/laptop': ['surface'],
  'sony/console': ['playstation', 'ps5'],
  'nintendo/console': ['switch', 'handheld'],
  'amazon/speaker': ['echo', 'alexa'],
  'sonos/speaker': ['sonos'],
  'lenovo/laptop': ['thinkpad'],
  'huawei/laptop': ['matebook'],
  'huawei/tablet': ['matepad'],
  'xiaomi/phone': ['redmi', 'poco'],
  'intel/desktop': ['nuc', 'mini pc'],
  'intel/desktop/nuc12extreme': ['nuc', 'mini pc'],
  'raspberry-pi/sbc': ['rpi', 'pi'],
  'ubiquiti/accesspoint': ['unifi', 'ap'],
  'synology/nas': ['diskstation', 'dsm'],
  'tp-link/router': ['tp link', 'deco'],
  'xiaomi/router/be7000': ['wifi 7', 'mesh'],
  'tp-link/router/xdr6080': ['wifi 7', 'mesh'],
  'lg/tv': ['oled'],
  'hp/printer': ['laserjet'],
  'canon/printer': ['pixma'],
  'epson/printer': ['ecotank'],
}

/**
 * The picker's last section: every picture that is nobody's product.
 *
 * One flat grid, not the six themed groups the picker used to render. Themes
 * were a table of contents for a vocabulary nobody browses by theme — someone
 * who does not know what to call the box in the hall is not going to guess
 * whether it is filed under Home or Computing, and they are going to search
 * instead. The search is the way in; a grid of shapes is the thing you scan
 * when you do not yet know the word, and scanning is faster in one grid than
 * in six.
 *
 * The category groups still decide the order, so the list reads the way it
 * always did — personal devices first, the grey box last — with each variant
 * immediately after the category it is a shape of.
 */
export const COMMON_TILES: IconChoiceTile[] = CATEGORY_ORDER.flatMap((group) =>
  group.flatMap((category) => [
    { icon: '', category, label: categoryLabel(category) },
    ...variantChoices(category),
  ]),
)

/**
 * The words one tile answers to, in the order that decides who wins a tie.
 *
 * The label leads on purpose: icon-search ranks a tile whose text *opens* with
 * the query above one that merely holds it somewhere, so "mac" has to meet
 * "MacBook" before it meets a path that happens to contain those letters. The
 * key and the maker come next because they are still names; the category and
 * the tags come last because they are descriptions, and a description that
 * happens to hold the query is the weakest answer worth showing.
 */
function searchText(tile: IconChoiceTile): string {
  const key = tile.icon || tile.category
  const [first] = key.split('/')
  const vendor = first in BRAND_NAMES ? BRAND_NAMES[first as VendorKey] : ''
  return [
    tile.label,
    key.split('/').join(' '),
    vendor,
    categoryLabel(tile.category),
    ...(ICON_TAGS[key as IconKey] ?? []),
  ]
    .filter(Boolean)
    .join(' ')
}

const SEARCHABLE_ICONS = [...COMMON_TILES, ...BRAND_TILES].map((tile) => ({ tile, text: searchText(tile) }))

/**
 * The bundled pictures a query asks for, best first.
 *
 * The other half of the picker's search is theSVG's, asked over HTTP because
 * six thousand marks do not belong in a browser; this half is local and
 * instant. Both are rendered as the same tiles, so a query is one list of
 * pictures however each one was found.
 */
export function searchBundledIcons(query: string): IconChoiceTile[] {
  return searchIcons(SEARCHABLE_ICONS, (entry) => entry.text, query).map((entry) => entry.tile)
}
