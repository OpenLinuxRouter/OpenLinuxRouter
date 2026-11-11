//go:build ignore

// Command gen builds oui.gz from the three IEEE registries.
//
// Run it with `make ouidb`, review the diff, commit the result. It is not part
// of the build: the generated file is committed so that `go build` needs no
// network and two builds of one commit embed identical bytes.
//
// Usage:
//
//	go run gen.go                 # fetch from standards-oui.ieee.org
//	go run gen.go -src /tmp/ieee  # use already-downloaded CSVs
//
// The -src form matters more than it looks. Most edits here are to the name
// cleanup below, and those want to be re-run and re-read a dozen times against
// the same input; refetching 5 MB each time to change one alias is how a
// generator stops being run at all.
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// registry is one of IEEE's three block sizes.
//
// Width is the assignment's length in hex digits, and it is the reason this
// table exists at all: 24, 28 and 36 bits are 6, 7 and 9 digits, and a row
// whose assignment is not exactly that long is malformed rather than
// interesting. Checking it here is what lets the loader downstream be a plain
// map lookup with no validation of its own.
type registry struct {
	name  string
	url   string
	file  string
	width int
}

// userAgent identifies this generator to IEEE. See read().
const userAgent = "open-linux-router-ouidb/1 (+https://github.com/open-linux-router/open-linux-router)"

var registries = []registry{
	{"MA-L", "https://standards-oui.ieee.org/oui/oui.csv", "oui.csv", 6},
	{"MA-M", "https://standards-oui.ieee.org/oui28/mam.csv", "mam.csv", 7},
	{"MA-S", "https://standards-oui.ieee.org/oui36/oui36.csv", "oui36.csv", 9},
}

func main() {
	src := flag.String("src", "", "directory of already-downloaded IEEE CSVs; fetch when empty")
	out := flag.String("out", "oui.gz", "file to write")
	flag.Parse()

	entries := map[string]string{}
	for _, reg := range registries {
		raw, err := read(reg, *src)
		if err != nil {
			log.Fatalf("%s: %v", reg.name, err)
		}
		n, err := parse(raw, reg, entries)
		if err != nil {
			log.Fatalf("%s: %v", reg.name, err)
		}
		log.Printf("%s: %d usable of %d rows", reg.name, n, bytes.Count(raw, []byte{'\n'}))
	}

	if err := write(*out, entries); err != nil {
		log.Fatal(err)
	}
	report(entries)
}

func read(reg registry, src string) ([]byte, error) {
	if src != "" {
		return os.ReadFile(filepath.Join(src, reg.file))
	}
	// No timeout on the client beyond this one: oui.csv is ~4 MB and IEEE
	// serves it slowly enough that the stdlib default of none is not the
	// hazard a short deadline would be.
	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequest(http.MethodGet, reg.url, nil)
	if err != nil {
		return nil, err
	}
	// Without this IEEE answers 418 to Go's default "Go-http-client/2.0" while
	// serving curl the file happily. Identifying the tool is the right thing to
	// do anyway; the point worth recording is that a naked stdlib GET does not
	// work here, so nobody removes this as noise.
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", reg.url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// parse adds one registry's usable rows to entries.
//
// Later registries overwrite earlier ones on a collision, which never happens
// between different widths and is what we want within one: the file is in no
// particular order and a duplicate assignment is a registry erratum.
func parse(raw []byte, reg registry, entries map[string]string) (int, error) {
	r := csv.NewReader(bytes.NewReader(raw))
	// The address column contains embedded newlines and stray quotes. Neither
	// is a column we read, so the lenient reader is the correct one: failing
	// the whole registry over a malformed postal address would be absurd.
	r.FieldsPerRecord = -1
	r.LazyQuotes = true

	header, err := r.Read()
	if err != nil {
		return 0, fmt.Errorf("reading header: %w", err)
	}
	assignment := slices.Index(header, "Assignment")
	organization := slices.Index(header, "Organization Name")
	if assignment < 0 || organization < 0 {
		return 0, fmt.Errorf("unexpected columns %q; IEEE changed the format", header)
	}

	var n int
	for {
		row, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, err
		}
		if len(row) <= assignment || len(row) <= organization {
			continue
		}

		prefix := strings.ToUpper(strings.TrimSpace(row[assignment]))
		if len(prefix) != reg.width || !isHex(prefix) {
			continue
		}
		vendor := clean(row[organization])
		if vendor == "" || dropped(vendor) {
			continue
		}
		entries[prefix] = vendor
		n++
	}
	return n, nil
}

// dropped reports the two organisation names that are not vendors.
//
// "IEEE Registration Authority" is the placeholder on an MA-L that IEEE
// subdivided and sold as MA-M and MA-S blocks — 436 of them. Keeping those
// would be worse than useless: longest-match falls back to the MA-L, so every
// device in a subdivided block whose own small block is unregistered would be
// reported as made by IEEE. "Private" is a registrant who paid to be withheld,
// which is a fact about the registry rather than a name for a device.
//
// Both cases end the same way, and it is the right ending: no vendor. A screen
// that says nothing is repairable by the operator naming the device. A screen
// that says "IEEE Registration Authority" teaches them not to trust the column.
func dropped(vendor string) bool {
	switch strings.ToLower(vendor) {
	case "ieee registration authority", "private":
		return true
	}
	return false
}

func isHex(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

// legalTail matches a trailing company-form suffix, with the punctuation and
// brackets IEEE registrants spell it with.
//
// Stripped because the registry is a legal document and the device list is not:
// "Apple, Inc." under a laptop is noise, and on a card two hundred pixels wide
// it is noise that pushes out the word the operator came to read. "Apple" is
// the same claim, shorter.
var legalTail = regexp.MustCompile(`(?i)[\s,]*[(\[]?\b(?:` + strings.Join([]string{
	`inc|incorporated|llc|l\.l\.c|ltd|ltda|limited|co|corp|corporate|corporation|company`,
	`gmbh|mbh|ag|a\.g|kg|s\.?a|s\.?a\.?s|sarl|s\.?r\.?l|s\.?p\.?a|s\.?l|b\.?v|n\.?v`,
	`a/s|ab|as|oy|oyj|plc|pty|pte|k\.?k|llp|lp|ulc|jsc|ooo|pjsc|cv|d\.?d|d\.?o\.?o`,
}, `|`) + `)\b\.?[)\]]?[\s,.]*$`)

var whitespace = regexp.MustCompile(`\s+`)

// clean turns a registrant's legal name into something worth putting under a
// device.
//
// Three steps, and deliberately not a fourth. It collapses whitespace, strips
// trailing company forms, and title-cases a name that is shouting. It does
// *not* strip trailing descriptors — "Technologies", "Networks", "Systems" —
// even though doing so would turn "Huawei Technologies" into "Huawei" without
// an alias. That rule is right about nine names in ten and wrong about
// "Extreme Networks" and "Palo Alto Networks", and a vendor column that is
// occasionally a different company's name is worse than one that is
// occasionally two words too long. The vendors that matter on a home network
// get an alias instead, which is a decision someone made rather than a rule
// that fires.
func clean(name string) string {
	s := whitespace.ReplaceAllString(strings.TrimSpace(name), " ")

	// Repeatedly, because "Co.,Ltd." is two of them and "Co., Ltd. Inc" exists.
	// Bounded rather than looped to exhaustion: a name that is nothing but
	// company forms should end up empty and be dropped, not loop forever.
	for range 5 {
		loc := legalTail.FindStringIndex(s)
		if loc == nil || loc[0] == 0 {
			// loc[0] == 0 means the whole name is a company form. Leaving it
			// alone lets it be dropped by the empty check rather than becoming
			// the empty string here, which reads the same downstream but keeps
			// the reason visible in a log line.
			break
		}
		s = strings.TrimRight(s[:loc[0]], " ,.")
	}
	s = strings.TrimRight(s, " ,.")

	if isShouting(s) {
		s = titleCase(s)
	}
	return applyAlias(s)
}

// isShouting reports a name written in all capitals, which about a fifth of the
// registry is. Left alone they make the column look like an error log, and they
// are the only names where re-casing is safe — a name with any lower-case
// letter in it was typed by someone who meant the capitals they used, so "eero"
// and "vivo" survive.
func isShouting(s string) bool {
	var letters bool
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			return false
		}
		if r >= 'A' && r <= 'Z' {
			letters = true
		}
	}
	return letters && len(s) > 3
}

// titleCase capitalises each word, and each hyphen-separated part of a word, so
// "D-LINK" becomes "D-Link" rather than "D-link". Acronyms lose — "ZTE" becomes
// "Zte" — which is what the alias table is for.
func titleCase(s string) string {
	parts := strings.Split(s, " ")
	for i, word := range parts {
		segs := strings.Split(word, "-")
		for j, seg := range segs {
			if seg == "" {
				continue
			}
			segs[j] = strings.ToUpper(seg[:1]) + strings.ToLower(seg[1:])
		}
		parts[i] = strings.Join(segs, "-")
	}
	return strings.Join(parts, " ")
}

// alias is a prefix rule: a cleaned name that starts with `prefix`, compared
// case-insensitively, is replaced wholesale by `name`.
//
// A prefix rather than an exact match because one company holds dozens of
// registrations under names that differ: "HUAWEI TECHNOLOGIES CO.,LTD" and
// "Huawei Device Co., Ltd." are 2066 assignments between them, and they are
// one vendor to an operator looking at a device list. Exact matches would need
// a line each and would go stale the next time IEEE saw a new spelling.
//
// Order matters — first match wins — so a longer prefix must precede any
// shorter one it starts with. "hewlett packard enterprise" before "hewlett
// packard" is the case that will bite.
type alias struct {
	prefix string
	name   string
}

// aliases is hand-maintained and covers what turns up on a home network. It is
// polish, not correctness: a vendor with no alias still gets its registry name,
// which is true, just longer.
var aliases = []alias{
	// Longest-first pairs, where one prefix contains another.
	{"hewlett packard enterprise", "HPE"},
	{"hewlett packard", "HP"},
	{"hp ", "HP"},
	{"guangdong oppo", "OPPO"},
	{"new h3c", "H3C"},
	{"seiko epson", "Epson"},
	{"beijing xiaomi", "Xiaomi"},
	{"philips lighting", "Philips Hue"},
	{"signify", "Philips Hue"},

	// Phones, computers and the chips inside them.
	{"apple", "Apple"},
	{"samsung", "Samsung"},
	{"huawei", "Huawei"},
	{"xiaomi", "Xiaomi"},
	{"vivo mobile", "vivo"},
	{"oneplus", "OnePlus"},
	{"motorola", "Motorola"},
	{"zte", "ZTE"},
	{"lenovo", "Lenovo"},
	{"dell", "Dell"},
	{"asustek", "ASUS"},
	{"asus", "ASUS"},
	{"acer", "Acer"},
	{"microsoft", "Microsoft"},
	{"google", "Google"},
	{"amazon", "Amazon"},
	{"sony", "Sony"},
	{"lg electronics", "LG"},
	{"nintendo", "Nintendo"},
	{"intel", "Intel"},
	{"realtek", "Realtek"},
	{"broadcom", "Broadcom"},
	{"qualcomm", "Qualcomm"},
	{"mediatek", "MediaTek"},
	{"marvell", "Marvell"},
	{"nordic semiconductor", "Nordic"},
	{"silicon laborator", "Silicon Labs"},
	{"espressif", "Espressif"},
	{"raspberry pi", "Raspberry Pi"},
	{"arduino", "Arduino"},

	// Contract manufacturers, which is who a great many devices are registered
	// to. Named by the trade name an operator might recognise.
	{"hon hai", "Foxconn"},
	{"foxconn", "Foxconn"},
	{"wistron neweb", "Wistron NeWeb"},
	{"wistron", "Wistron"},
	{"quanta", "Quanta"},
	{"compal", "Compal"},
	{"pegatron", "Pegatron"},
	{"liteon", "Lite-On"},
	{"lite-on", "Lite-On"},

	// Network gear.
	{"tp-link", "TP-Link"},
	{"tplink", "TP-Link"},
	{"d-link", "D-Link"},
	{"netgear", "Netgear"},
	{"ubiquiti", "Ubiquiti"},
	// MikroTik holds every one of its blocks as "Routerboard.com"; the word
	// "Mikrotik" appears nowhere in the registry. An alias on the trade name
	// alone was a rule that never fired — which is the mistake an alias list
	// makes easy to write and impossible to spot, and is why
	// TestEveryVendorKeyIsReachable in internal/devices exists.
	{"routerboard", "MikroTik"},
	{"mikrotik", "MikroTik"},
	{"cisco", "Cisco"},
	{"aruba", "Aruba"},
	{"juniper", "Juniper"},
	{"ruckus", "Ruckus"},
	{"zyxel", "Zyxel"},
	{"sagemcom", "Sagemcom"},
	{"technicolor", "Technicolor"},
	{"arris", "ARRIS"},
	{"commscope", "CommScope"},
	{"actiontec", "Actiontec"},
	{"fiberhome", "FiberHome"},
	{"eero", "eero"},
	{"aerohive", "Aerohive"},

	// Storage, printers and cameras.
	{"synology", "Synology"},
	{"qnap", "QNAP"},
	{"western digital", "Western Digital"},
	{"seagate", "Seagate"},
	{"brother", "Brother"},
	{"canon", "Canon"},
	{"ricoh", "Ricoh"},
	{"xerox", "Xerox"},
	{"kyocera", "Kyocera"},
	{"hikvision", "Hikvision"},
	{"dahua", "Dahua"},
	{"axis communication", "Axis"},
	{"reolink", "Reolink"},

	// Home automation and media.
	{"sonos", "Sonos"},
	{"roku", "Roku"},
	{"tuya", "Tuya"},
	{"shelly", "Shelly"},
	{"allterco", "Shelly"},
	{"ikea", "IKEA"},
	{"ecobee", "ecobee"},
	{"nest labs", "Nest"},
	{"ring ", "Ring"},
	{"wyze", "Wyze"},
	{"tado", "tado"},

	// Hypervisors. A virtual NIC is a device on the network like any other, and
	// these are the OUIs a lab full of VMs is made of.
	{"vmware", "VMware"},
	{"proxmox", "Proxmox"},
	{"parallels", "Parallels"},
	{"oracle", "Oracle"},
	{"xensource", "Xen"},
	{"nutanix", "Nutanix"},
}

func applyAlias(s string) string {
	lower := strings.ToLower(s)
	for _, a := range aliases {
		if strings.HasPrefix(lower, a.prefix) {
			return a.name
		}
	}
	return s
}

// write emits the sorted table, gzipped.
//
// Sorted so the committed file has a reviewable diff: adding an alias should
// show as the lines it changed, not as 53 000 lines reordered. The gzip header
// is left at its zero value for the same reason — a modification time in there
// would make every regeneration a whole-file change.
func write(path string, entries map[string]string) error {
	lines := make([]string, 0, len(entries))
	for prefix, vendor := range entries {
		lines = append(lines, prefix+"\t"+vendor)
	}
	sort.Strings(lines)

	var plain bytes.Buffer
	fmt.Fprintf(&plain, "# IEEE MA-L, MA-M and MA-S registries, retrieved %s.\n",
		time.Now().UTC().Format("2006-01-02"))
	fmt.Fprintf(&plain, "# Generated by gen.go. Do not edit; run `make ouidb`.\n")
	for _, line := range lines {
		plain.WriteString(line)
		plain.WriteByte('\n')
	}

	var packed bytes.Buffer
	zw, err := gzip.NewWriterLevel(&packed, gzip.BestCompression)
	if err != nil {
		return err
	}
	if _, err := zw.Write(plain.Bytes()); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}

	if err := os.WriteFile(path, packed.Bytes(), 0o644); err != nil {
		return err
	}
	log.Printf("wrote %s: %d prefixes, %d bytes (%d uncompressed)",
		path, len(entries), packed.Len(), plain.Len())
	return nil
}

// report prints the vendors holding the most assignments.
//
// This is the review step, and it is why it prints rather than just writing the
// file: the names below are what an operator will actually read, and the diff
// of a gzipped blob cannot show whether an edit to clean() improved them or
// mangled them.
func report(entries map[string]string) {
	count := map[string]int{}
	for _, vendor := range entries {
		count[vendor]++
	}
	vendors := make([]string, 0, len(count))
	for vendor := range count {
		vendors = append(vendors, vendor)
	}
	sort.Slice(vendors, func(i, j int) bool {
		if count[vendors[i]] != count[vendors[j]] {
			return count[vendors[i]] > count[vendors[j]]
		}
		return vendors[i] < vendors[j]
	})

	log.Printf("%d distinct vendors; the 25 largest, for eyeballing:", len(vendors))
	for _, vendor := range vendors[:min(25, len(vendors))] {
		log.Printf("  %5d  %s", count[vendor], vendor)
	}
}
