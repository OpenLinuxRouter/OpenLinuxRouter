package devices

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Huawei reads the web management pages of a Huawei fiber ONT (OptiXstar and
// the HG-series it succeeded). Every carrier-branded build of these pages keeps
// the same shape: a token, a base64 password, and status tables published as
// JavaScript constructor calls, which this adapter reads as data and never
// evaluates.
//
// The ONT is read-only here. Nothing on these pages is written, and no page
// outside the status tree is fetched.
type Huawei struct {
	Client   *http.Client
	Password func(string) (string, error)
}

var _ Adapter = (*Huawei)(nil)

const huaweiBodyLimit = 4 << 20

var (
	errHuaweiHTTP = errors.New("huawei: management request failed")
	errHuaweiAuth = errors.New("huawei: authentication failed")
	errHuaweiPage = errors.New("huawei: unreadable status page")
)

var huaweiTokenRE = regexp.MustCompile(`^[0-9a-fA-F]{16,128}$`)
var huaweiRangeRE = regexp.MustCompile(`^(-?[0-9]+(?:\.[0-9]+)?)\s*[-~]\s*(-?[0-9]+(?:\.[0-9]+)?)`)

type huaweiSession struct {
	adapter *Huawei
	device  Device
	base    *url.URL
	client  http.Client
	cookie  string
}

func (h *Huawei) session(d Device) (*huaweiSession, error) {
	u, err := url.Parse(d.Address)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("%w: Huawei ONT address must be an HTTP origin", ErrInvalid)
	}
	u.Path = "/"
	u.RawPath = ""
	s := &huaweiSession{adapter: h, device: d, base: u}
	if h.Client != nil {
		s.client = *h.Client
	}
	if s.client.Timeout <= 0 || s.client.Timeout > 12*time.Second {
		s.client.Timeout = 12 * time.Second
	}
	// Sessions are carried by the cookie the ONT issues at login. A jar would
	// also replay cookies to a same-origin redirect, which this adapter never
	// wants; the one cookie is held explicitly instead.
	s.client.Jar = nil
	s.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return s, nil
}

// login answers the token-plus-base64 challenge in login.asp. The token is
// single-use, so a failed login is reported rather than retried: each retry
// spends one of the ONT's few attempts before it locks the form for a minute.
func (s *huaweiSession) login(ctx context.Context) error {
	password, err := s.device.Credential(s.adapter.Password)
	if err != nil {
		return err
	}
	tokenPage, _, err := s.request(ctx, http.MethodPost, "/asp/GetRandCount.asp", url.Values{}, "")
	if err != nil {
		return err
	}
	token := strings.TrimSpace(strings.TrimPrefix(tokenPage, "\ufeff"))
	if !huaweiTokenRE.MatchString(token) {
		return errHuaweiAuth
	}
	form := url.Values{}
	form.Set("UserName", s.username())
	form.Set("PassWord", base64.StdEncoding.EncodeToString([]byte(password)))
	form.Set("Language", "chinese")
	form.Set("x.X_HW_Token", token)
	page, cookie, err := s.request(ctx, http.MethodPost, "/login.cgi", form, "Cookie=body:Language:chinese:id=-1")
	if err != nil {
		return err
	}
	if cookie == "" || strings.Contains(page, "/login.asp") {
		return errHuaweiAuth
	}
	s.cookie = cookie
	return nil
}

func (s *huaweiSession) username() string {
	if s.device.Username != "" {
		return s.device.Username
	}
	return "user"
}

func (s *huaweiSession) request(ctx context.Context, method, path string, form url.Values, cookie string) (string, string, error) {
	u := *s.base
	u.Path = path
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return "", "", errHuaweiHTTP
	}
	req.Header.Set("Cache-Control", "no-cache")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	switch {
	case cookie != "":
		req.Header.Set("Cookie", cookie)
	case s.cookie != "":
		req.Header.Set("Cookie", s.cookie)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		return "", "", errHuaweiHTTP
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", errHuaweiHTTP
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, huaweiBodyLimit+1))
	if err != nil {
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		return "", "", errHuaweiHTTP
	}
	if len(data) > huaweiBodyLimit {
		return "", "", errors.New("huawei: management page exceeds size limit")
	}
	issued := ""
	for _, value := range resp.Header.Values("Set-Cookie") {
		name, rest, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(name) == "" {
			continue
		}
		pair, _, _ := strings.Cut(rest, ";")
		issued = strings.TrimSpace(name) + "=" + strings.TrimSpace(pair)
		break
	}
	return string(data), issued, nil
}

func (s *huaweiSession) page(ctx context.Context, path string) (string, error) {
	page, _, err := s.request(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return "", err
	}
	if strings.Contains(page, "txt_Password") && strings.Contains(page, "GetRandCount") {
		return "", errHuaweiAuth
	}
	return page, nil
}

// Read assembles one dated observation from four independent status pages. A
// page that fails leaves its own section in error and does not suppress the
// others: a rebooting ONT can answer the optical page while the WAN list is
// still empty.
func (h *Huawei) Read(ctx context.Context, d Device) (Snapshot, error) {
	now := time.Now().UTC()
	snap := Snapshot{DeviceID: d.ID, ObservedAt: now, Capabilities: Capabilities{}, Collections: map[string]Collection{}}
	for _, name := range []string{"vlans", "fdb", "neighbors", "radios", "wireless", "associations"} {
		snap.Collections[name] = Collection{State: "unsupported", ObservedAt: now, Error: "An ONT has no switch VLAN table, MAC table, radio or wireless clients to read here"}
	}
	for _, key := range []string{"vlans.read", "pvids.read", "fdb.lookup", "fdb.list", "neighbors.read", "radios.read", "wireless.read", "associations.read", "ports.configure", "vlans.configure", "wireless.configure", "poe.configure", "device.reboot"} {
		snap.Capabilities[key] = Unsupported("Not implemented for ONT web pages")
	}

	session, err := h.session(d)
	if err != nil {
		return snap, err
	}
	if err = session.login(ctx); err != nil {
		return snap, err
	}

	ont := &ONT{}
	snap.Identity = Identity{Manufacturer: "Huawei", Roles: append([]Role(nil), d.Roles...)}

	devicePage, deviceErr := session.page(ctx, "/html/ssmp/deviceinfo/deviceinfo.asp")
	if deviceErr == nil {
		deviceErr = huaweiReadIdentity(devicePage, &snap, ont)
	}
	snap.Collections["identity"] = huaweiCollection(now, deviceErr)
	snap.Capabilities["identity.read"] = huaweiCapability(snap.Collections["identity"])

	opticPage, opticErr := session.page(ctx, "/html/amp/opticinfo/opticinfo.asp")
	if opticErr == nil {
		ont.Optical, opticErr = huaweiReadOptical(opticPage)
	}
	if opticErr != nil {
		ont.Optical = nil
	}
	snap.Collections["optical"] = huaweiCollection(now, opticErr)
	snap.Capabilities["optical.read"] = huaweiCapability(snap.Collections["optical"])

	ethPage, ethErr := session.page(ctx, "/html/amp/ethinfo/ethinfo.asp")
	if ethErr == nil {
		snap.Ports, ont.PON.Mode, ethErr = huaweiReadPorts(ethPage)
	}
	if ethErr != nil {
		snap.Ports = nil
	}
	snap.Ports = huaweiWithLinePort(snap.Ports, ont)
	snap.Collections["ports"] = huaweiCollection(now, ethErr)
	snap.Capabilities["ports.read"] = huaweiCapability(snap.Collections["ports"])

	wanErr := huaweiReadWAN(ctx, session, ont)
	snap.Collections["wan"] = huaweiCollection(now, wanErr)
	snap.Capabilities["wan.read"] = huaweiCapability(snap.Collections["wan"])
	if wanErr != nil {
		ont.WAN = nil
	}

	snap.ONT = ont
	return snap, nil
}

func huaweiCollection(now time.Time, err error) Collection {
	if err != nil {
		return Collection{State: "error", ObservedAt: now, Error: err.Error()}
	}
	return Collection{State: "ok", ObservedAt: now}
}

func huaweiCapability(c Collection) Capability {
	if c.State == "ok" {
		return Supported()
	}
	if c.Error != "" {
		return Unsupported(c.Error)
	}
	return Capability{State: "unknown"}
}

// huaweiReadIdentity fills identity, PON state and health from the device page.
// Fields are positional in the page's constructor call, so a page that no
// longer carries enough of them is reported as unreadable rather than guessed.
func huaweiReadIdentity(page string, snap *Snapshot, ont *ONT) error {
	devices, err := huaweiObjects(page, "stDeviceInfo")
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		return errHuaweiPage
	}
	info := devices[0]
	if len(info) < 15 {
		return errHuaweiPage
	}
	model, serial := huaweiField(info, 4), huaweiField(info, 1)
	hardware, software := huaweiField(info, 2), huaweiField(info, 3)
	if model == "" || serial == "" {
		return errHuaweiPage
	}
	snap.Identity.Model = model
	snap.Identity.Serial = serial
	snap.Identity.Firmware = strings.TrimSpace(software + " " + hardware)
	if mac := huaweiMAC(huaweiField(info, 7)); mac != "" {
		snap.Identity.MACs = append(snap.Identity.MACs, mac)
	}
	for _, line := range strings.Split(huaweiField(info, 14), "\n") {
		if mac := huaweiMAC(line); mac != "" {
			snap.Identity.MACs = append(snap.Identity.MACs, mac)
		}
	}
	ont.Customization = huaweiScalar(page, "customizeDes")
	if uptime, ok := huaweiUptime(page); ok {
		snap.Identity.UptimeSeconds = uptime
	}

	health := &ONTHealth{}
	health.CPUPercent = huaweiPercent(huaweiScalar(page, "cpuUsed"))
	health.MemoryPercent = huaweiPercent(huaweiScalar(page, "memUsed"))
	health.NATSessions = huaweiNATSessions(huaweiScalar(page, "NatSessionInfo"))
	if health.CPUPercent != nil || health.MemoryPercent != nil || health.NATSessions != nil {
		ont.Health = health
	}

	states, stateErr := huaweiObjects(page, "ONTInfo")
	if stateErr != nil {
		return stateErr
	}
	for _, state := range states {
		if len(state) == 0 {
			continue
		}
		value := strings.ToUpper(strings.TrimSpace(huaweiField(state, len(state)-1)))
		if value == "" {
			continue
		}
		ont.PON.State = value
		registered := value == "ONLINE" || value == "O5" || value == "WORKING"
		ont.PON.Registered = Ptr(registered)
		break
	}
	if ont.PON.Mode == "" {
		ont.PON.Mode = strings.ToUpper(huaweiScalar(page, "ontPonMode"))
	}
	return nil
}

// huaweiReadOptical reads the transceiver table. The receive power is the
// number an operator checks after a fiber fault, and the page publishes the
// vendor's own acceptable window next to it, so both are carried.
func huaweiReadOptical(page string) (*Optical, error) {
	modules, err := huaweiObjects(page, "stOpticInfo")
	if err != nil {
		return nil, err
	}
	if len(modules) == 0 || len(modules[0]) < 16 {
		return nil, errHuaweiPage
	}
	m := modules[0]
	optical := &Optical{
		ModuleVendor: strings.TrimSpace(huaweiField(m, 9)),
		ModuleSerial: strings.TrimSpace(huaweiField(m, 10)),
		DateCode:     strings.TrimSpace(huaweiField(m, 11)),
	}
	if status := strings.ToLower(strings.TrimSpace(huaweiField(m, 1))); status == "ok" || status == "up" {
		optical.LinkUp = Ptr(true)
	} else if status != "" && status != "--" {
		optical.LinkUp = Ptr(false)
	}
	optical.TXDBm = huaweiFloat(huaweiField(m, 2))
	optical.RXDBm = huaweiFloat(huaweiField(m, 3))
	optical.VoltageMV = huaweiUint(huaweiField(m, 4))
	optical.TemperatureC = huaweiFloat(huaweiField(m, 5))
	optical.CurrentMA = huaweiUint(huaweiField(m, 6))
	optical.TXWavelengthNM = huaweiUint(huaweiField(m, 12))
	optical.RXWavelengthNM = huaweiUint(huaweiField(m, 13))
	switch strings.TrimSpace(huaweiField(m, 15)) {
	case "0":
		optical.LOS = Ptr(false)
	case "1":
		optical.LOS = Ptr(true)
	}
	if ref := huaweiScalar(page, "opticTxRxRef"); ref != "" {
		if tx, rx, ok := huaweiOpticalRange(ref); ok {
			optical.TXRangeDBm = tx
			optical.RXRangeDBm = rx
		}
	}
	return optical, nil
}

// huaweiOpticalRange parses the page's window notation, "0.6-5.6dBm:-28.5--10.0dBm",
// where a negative maximum runs straight into the separating hyphen.
func huaweiOpticalRange(ref string) (tx, rx *SignalRange, ok bool) {
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) != 2 {
		return nil, nil, false
	}
	tx = huaweiRange(parts[0])
	rx = huaweiRange(parts[1])
	return tx, rx, tx != nil && rx != nil
}

func huaweiRange(value string) *SignalRange {
	m := huaweiRangeRE.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return nil
	}
	low, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return nil
	}
	high, err := strconv.ParseFloat(m[2], 64)
	if err != nil || high <= low {
		return nil
	}
	return &SignalRange{Min: low, Max: high}
}

// huaweiReadPorts reads the four LAN ports and the uplink mode they sit behind.
// The page splits one port across two constructor lists: its media in geInfos
// and its counters in userEthInfos, joined on the LANPort index.
func huaweiReadPorts(page string) ([]Port, string, error) {
	media, err := huaweiObjects(page, "GEInfo")
	if err != nil {
		return nil, "", err
	}
	if len(media) == 0 {
		return nil, "", errHuaweiPage
	}
	counters, err := huaweiObjects(page, "LANStats")
	if err != nil {
		return nil, "", err
	}
	stats := map[int][]string{}
	for _, counter := range counters {
		if len(counter) < 13 {
			continue
		}
		if index, ok := huaweiPortIndex(huaweiField(counter, 0)); ok {
			stats[index] = counter
		}
	}
	uplink := ""
	if uplinks, err := huaweiObjects(page, "stConfigPort"); err == nil && len(uplinks) > 0 && len(uplinks[0]) > 2 {
		uplink = strings.TrimSpace(huaweiField(uplinks[0], 2))
	}
	var ports []Port
	for _, entry := range media {
		if len(entry) < 4 {
			continue
		}
		index, ok := huaweiPortIndex(huaweiField(entry, 0))
		if !ok {
			continue
		}
		port := Port{ID: fmt.Sprintf("lan%d", index), Name: fmt.Sprintf("LAN %d", index), Kind: "ethernet"}
		up := strings.TrimSpace(huaweiField(entry, 3)) == "1"
		port.LinkUp = Ptr(up)
		port.Enabled = Ptr(true)
		if up {
			switch strings.TrimSpace(huaweiField(entry, 2)) {
			case "0":
				port.SpeedMbps = Ptr(uint64(10))
			case "1":
				port.SpeedMbps = Ptr(uint64(100))
			case "2":
				port.SpeedMbps = Ptr(uint64(1000))
			case "3":
				port.SpeedMbps = Ptr(uint64(2500))
			case "6":
				port.SpeedMbps = Ptr(uint64(5000))
			case "4":
				port.SpeedMbps = Ptr(uint64(10000))
			case "7":
				port.SpeedMbps = Ptr(uint64(25000))
			}
			switch strings.TrimSpace(huaweiField(entry, 1)) {
			case "0":
				port.Duplex = "half"
			case "1":
				port.Duplex = "full"
			}
		}
		if counter, ok := stats[index]; ok {
			port.TXPackets = huaweiCounter(huaweiField(counter, 2), huaweiField(counter, 1))
			port.TXBytes = huaweiBytes(huaweiField(counter, 4), huaweiField(counter, 3), huaweiField(counter, 6), huaweiField(counter, 5))
			port.RXPackets = huaweiCounter(huaweiField(counter, 8), huaweiField(counter, 7))
			port.RXBytes = huaweiBytes(huaweiField(counter, 10), huaweiField(counter, 9), huaweiField(counter, 12), huaweiField(counter, 11))
		}
		ports = append(ports, port)
	}
	if len(ports) == 0 {
		return nil, "", errHuaweiPage
	}
	return ports, uplink, nil
}

// huaweiWithLinePort puts the fiber in front of the LAN ports. The line port is
// its own row because its state comes from the transceiver, not from the
// Ethernet table.
func huaweiWithLinePort(ports []Port, ont *ONT) []Port {
	line := Port{ID: "pon", Name: "PON", Kind: "fiber"}
	if ont.Optical != nil {
		line.LinkUp = ont.Optical.LinkUp
		line.Enabled = Ptr(true)
	}
	if ont.PON.Mode != "" {
		line.Name = "PON " + ont.PON.Mode
	}
	return append([]Port{line}, ports...)
}

// huaweiReadWAN joins three pages: the connection list, the per-connection
// counters and the IPv6 addresses the WANs were given. Each page keys its rows
// by the connection's own instance path, so the join is by path and never by
// row order.
func huaweiReadWAN(ctx context.Context, session *huaweiSession, ont *ONT) error {
	list, err := session.page(ctx, "/html/bbsp/common/wan_list_cache_wan.asp")
	if err != nil {
		return err
	}
	ipWANs, err := huaweiObjects(list, "WanIP")
	if err != nil {
		return err
	}
	pppWANs, err := huaweiObjects(list, "WanPPP")
	if err != nil {
		return err
	}
	connections := make([]WANConnection, 0, len(ipWANs)+len(pppWANs))
	for _, args := range ipWANs {
		if connection, ok := huaweiIPWAN(args); ok {
			connections = append(connections, connection)
		}
	}
	for _, args := range pppWANs {
		if connection, ok := huaweiPPPWAN(args); ok {
			connections = append(connections, connection)
		}
	}
	if len(connections) == 0 {
		return errHuaweiPage
	}
	byPath := map[string]*WANConnection{}
	for i := range connections {
		byPath[connections[i].Path] = &connections[i]
	}
	if page, err := session.page(ctx, "/html/bbsp/common/get_wan_list_ipwanstat.asp"); err == nil {
		huaweiApplyWANStats(page, byPath)
	}
	if page, err := session.page(ctx, "/html/bbsp/common/get_wan_list_pppwanstat.asp"); err == nil {
		huaweiApplyWANStats(page, byPath)
	}
	if page, err := session.page(ctx, "/html/bbsp/common/wan_list_cache_wanipv6.asp"); err == nil {
		huaweiApplyWANIPv6(page, byPath)
	}
	ont.WAN = connections
	return nil
}

// huaweiIPWAN reads one IP-routed connection (DHCP or static). Field positions
// follow the WanIP constructor published in wan_list_info.asp.
func huaweiIPWAN(args []string) (WANConnection, bool) {
	if len(args) < 62 {
		return WANConnection{}, false
	}
	c := WANConnection{
		Path:          huaweiField(args, 0),
		Name:          huaweiField(args, 8),
		Service:       huaweiField(args, 26),
		Status:        huaweiField(args, 5),
		Interface:     huaweiInterface(huaweiField(args, 14)),
		MAC:           huaweiMAC(huaweiField(args, 4)),
		Address:       huaweiAddress(huaweiField(args, 15)),
		Gateway:       huaweiAddress(huaweiField(args, 17)),
		DNS:           huaweiList(huaweiField(args, 20)),
		VLAN:          huaweiUint(huaweiField(args, 21)),
		Priority:      huaweiUint(huaweiField(args, 23)),
		UptimeSeconds: huaweiUint(huaweiField(args, 43)),
	}
	if !c.Valid() {
		return WANConnection{}, false
	}
	return c, true
}

// huaweiPPPWAN reads one PPPoE connection. Its password arrives as the ONT's
// own placeholder text and is dropped here rather than carried in a snapshot.
func huaweiPPPWAN(args []string) (WANConnection, bool) {
	if len(args) < 57 {
		return WANConnection{}, false
	}
	c := WANConnection{
		Path:          huaweiField(args, 0),
		Name:          huaweiField(args, 8),
		Service:       huaweiField(args, 27),
		Status:        huaweiField(args, 5),
		Interface:     "PPPoE",
		MAC:           huaweiMAC(huaweiField(args, 4)),
		Address:       huaweiAddress(huaweiField(args, 14)),
		Gateway:       huaweiAddress(huaweiField(args, 15)),
		DNS:           huaweiList(huaweiField(args, 18)),
		Username:      huaweiField(args, 19),
		VLAN:          huaweiUint(huaweiField(args, 23)),
		Priority:      huaweiUint(huaweiField(args, 25)),
		UptimeSeconds: huaweiUint(huaweiField(args, 40)),
	}
	if !c.Valid() {
		return WANConnection{}, false
	}
	return c, true
}

func huaweiInterface(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "dhcp":
		return "DHCP"
	case "static":
		return "Static"
	}
	return strings.TrimSpace(mode)
}

func huaweiApplyWANStats(page string, byPath map[string]*WANConnection) {
	stats, err := huaweiObjects(page, "WaninfoStats")
	if err != nil {
		return
	}
	for _, args := range stats {
		if len(args) < 5 {
			continue
		}
		path := strings.TrimSuffix(huaweiField(args, 0), ".Stats")
		connection, ok := byPath[path]
		if !ok {
			continue
		}
		// The page reports byte counters both as one 32-bit number and, on
		// firmware that has them, as a high/low pair. The pair survives a wrap;
		// the single number does not, so it is only used when it is all there is.
		sent := huaweiCounter(huaweiField(args, 11), huaweiField(args, 12))
		if sent == nil {
			sent = huaweiUint(huaweiField(args, 1))
		}
		received := huaweiCounter(huaweiField(args, 13), huaweiField(args, 14))
		if received == nil {
			received = huaweiUint(huaweiField(args, 2))
		}
		connection.TXBytes = sent
		connection.RXBytes = received
		connection.TXPackets = huaweiUint(huaweiField(args, 3))
		connection.RXPackets = huaweiUint(huaweiField(args, 4))
	}
}

// huaweiApplyWANIPv6 joins the IPv6 page on the connection's own MAC: the WAN
// list and the IPv6 list are two views of the same PVC, and the MAC is the only
// field both carry that is unique per PVC.
func huaweiApplyWANIPv6(page string, byPath map[string]*WANConnection) {
	addresses, err := huaweiObjects(page, "IPv6AddressInfo")
	if err != nil {
		return
	}
	prefixes, err := huaweiObjects(page, "IPv6PrefixInfo")
	if err != nil {
		return
	}
	wanInfo, err := huaweiObjects(page, "IPv6WanInfo")
	if err != nil {
		return
	}
	byInterface := map[string][]string{}
	for _, args := range addresses {
		if len(args) < 4 {
			continue
		}
		if key := huaweiShowInterface(huaweiField(args, 0)); key != "" && huaweiAddress(huaweiField(args, 3)) != "" {
			byInterface[key] = append(byInterface[key], huaweiField(args, 3))
		}
	}
	prefixByInterface := map[string]string{}
	for _, args := range prefixes {
		if len(args) < 3 {
			continue
		}
		if key := huaweiShowInterface(huaweiField(args, 0)); key != "" {
			prefixByInterface[key] = huaweiField(args, 2)
		}
	}
	byMAC := map[string]*WANConnection{}
	for _, connection := range byPath {
		if connection.MAC != "" {
			byMAC[connection.MAC] = connection
		}
	}
	for _, args := range wanInfo {
		if len(args) < 6 {
			continue
		}
		connection, ok := byMAC[huaweiMAC(huaweiField(args, 4))]
		if !ok {
			continue
		}
		key := huaweiShowInterface(huaweiField(args, 0))
		if key == "" {
			continue
		}
		for _, address := range byInterface[key] {
			if ip := huaweiAddress(address); ip != "" {
				connection.IPv6 = append(connection.IPv6, ip)
			}
		}
		if prefix := strings.TrimSpace(prefixByInterface[key]); prefix != "" {
			connection.IPv6Prefix = prefix
		}
	}
}

// huaweiShowInterface extracts the index of a "…X_HW_ShowInterface.<n>" path.
func huaweiShowInterface(path string) string {
	index := strings.LastIndex(path, "X_HW_ShowInterface.")
	if index < 0 {
		return ""
	}
	rest := path[index+len("X_HW_ShowInterface."):]
	key, _, _ := strings.Cut(rest, ".")
	if _, err := strconv.ParseUint(key, 10, 32); err != nil {
		return ""
	}
	return key
}

// huaweiObjects returns the decoded argument lists of every `new name(...)` in
// page. Status pages publish their tables this way; values are read as data and
// never evaluated.
func huaweiObjects(page, name string) ([][]string, error) {
	needle := "new " + name + "("
	var out [][]string
	for offset := 0; len(out) <= 256; {
		index := strings.Index(page[offset:], needle)
		if index < 0 {
			return out, nil
		}
		start := offset + index + len(needle)
		args, end, err := huaweiArguments(page, start)
		if err != nil {
			return nil, err
		}
		out = append(out, args)
		offset = end
	}
	return nil, errHuaweiPage
}

func huaweiArguments(page string, start int) ([]string, int, error) {
	var args []string
	var current strings.Builder
	quoted := false
	depth := 1
	for i := start; i < len(page); i++ {
		c := page[i]
		switch {
		case quoted && c == '\\':
			if i+1 >= len(page) {
				return nil, 0, errHuaweiPage
			}
			current.WriteByte(c)
			current.WriteByte(page[i+1])
			i++
		case c == '"':
			quoted = !quoted
			current.WriteByte(c)
		case quoted:
			current.WriteByte(c)
		case c == '(':
			depth++
			current.WriteByte(c)
		case c == ')':
			depth--
			if depth == 0 {
				args = append(args, strings.TrimSpace(current.String()))
				return args, i + 1, nil
			}
			current.WriteByte(c)
		case c == ',':
			args = append(args, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteByte(c)
		}
		if current.Len() > 4096 || len(args) > 128 {
			return nil, 0, errHuaweiPage
		}
	}
	return nil, 0, errHuaweiPage
}

// huaweiValue decodes one argument: a double-quoted string with \xNN escapes,
// or a bare token such as a number or null.
func huaweiValue(arg string) (string, bool) {
	if len(arg) < 2 || arg[0] != '"' || arg[len(arg)-1] != '"' {
		return arg, true
	}
	body := arg[1 : len(arg)-1]
	if !strings.Contains(body, `\`) {
		return body, true
	}
	var out strings.Builder
	for i := 0; i < len(body); i++ {
		if body[i] != '\\' {
			out.WriteByte(body[i])
			continue
		}
		if i+1 >= len(body) {
			return "", false
		}
		switch body[i+1] {
		case 'x':
			if i+3 >= len(body) {
				return "", false
			}
			v, err := strconv.ParseUint(body[i+2:i+4], 16, 8)
			if err != nil {
				return "", false
			}
			out.WriteByte(byte(v))
			i += 3
		case '\\', '"':
			out.WriteByte(body[i+1])
			i++
		default:
			return "", false
		}
	}
	return out.String(), true
}

// huaweiField returns one decoded argument, or "" when the page has no such
// argument. A page that cannot decode its own escapes is treated as absent.
func huaweiField(args []string, index int) string {
	if index < 0 || index >= len(args) {
		return ""
	}
	value, ok := huaweiValue(args[index])
	if !ok {
		return ""
	}
	return value
}

func huaweiScalar(page, name string) string {
	index := strings.Index(page, "var "+name+" = '")
	if index < 0 {
		return ""
	}
	rest := page[index+len("var "+name+" = '"):]
	end := strings.Index(rest, "'")
	if end < 0 {
		return ""
	}
	value, ok := huaweiValue(`"` + rest[:end] + `"`)
	if !ok {
		return ""
	}
	return value
}

func huaweiUptime(page string) (*uint64, bool) {
	value := strings.TrimSpace(huaweiScalar(page, "dev_uptime"))
	if value == "" {
		return nil, false
	}
	seconds, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return nil, false
	}
	return &seconds, true
}

func huaweiPercent(value string) *float64 {
	value = strings.TrimSuffix(strings.TrimSpace(value), "%")
	if value == "" {
		return nil
	}
	percent, err := strconv.ParseFloat(value, 64)
	if err != nil || percent < 0 || percent > 100 {
		return nil
	}
	return &percent
}

func huaweiNATSessions(value string) *uint64 {
	used, _, ok := strings.Cut(strings.TrimSpace(value), "/")
	if !ok {
		return nil
	}
	sessions, err := strconv.ParseUint(strings.TrimSpace(used), 10, 64)
	if err != nil {
		return nil
	}
	return &sessions
}

func huaweiFloat(value string) *float64 {
	value = strings.TrimSpace(value)
	if value == "" || value == "--" {
		return nil
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil
	}
	return &number
}

func huaweiUint(value string) *uint64 {
	value = strings.TrimSpace(value)
	if value == "" || value == "--" {
		return nil
	}
	number, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return nil
	}
	return &number
}

// huaweiCounter recombines a 32-bit pair into one count.
func huaweiCounter(high, low string) *uint64 {
	h, err := strconv.ParseUint(strings.TrimSpace(high), 10, 64)
	if err != nil {
		return nil
	}
	l, err := strconv.ParseUint(strings.TrimSpace(low), 10, 64)
	if err != nil || h > (^uint64(0)>>32) {
		return nil
	}
	return Ptr(h<<32 + l)
}

// huaweiBytes recombines the good and bad halves of one direction. The page
// keeps four 32-bit values per direction; a counter that would overflow is
// reported as absent rather than wrapped.
func huaweiBytes(high, good, badHigh, bad string) *uint64 {
	up, err := strconv.ParseUint(strings.TrimSpace(high), 10, 64)
	if err != nil || up > (^uint64(0)>>32) {
		return nil
	}
	low, err := strconv.ParseUint(strings.TrimSpace(good), 10, 64)
	if err != nil {
		return nil
	}
	badUp, err := strconv.ParseUint(strings.TrimSpace(badHigh), 10, 64)
	if err != nil || badUp > (^uint64(0)>>32) {
		return nil
	}
	badLow, err := strconv.ParseUint(strings.TrimSpace(bad), 10, 64)
	if err != nil {
		return nil
	}
	total := up << 32
	if total>>32 != up || total+low < total {
		return nil
	}
	total += low
	if total+badLow < total {
		return nil
	}
	total += badLow
	if total+badUp<<32 < total {
		return nil
	}
	return Ptr(total + badUp<<32)
}

// huaweiPortIndex reads the "LANPort.<n>" suffix of a port instance path.
func huaweiPortIndex(path string) (int, bool) {
	index := strings.LastIndex(path, "LANPort.")
	if index < 0 {
		return 0, false
	}
	rest := path[index+len("LANPort."):]
	digits, _, _ := strings.Cut(rest, ".")
	number, err := strconv.Atoi(digits)
	if err != nil || number < 1 || number > 32 {
		return 0, false
	}
	return number, true
}

var huaweiMACRE = regexp.MustCompile(`^[0-9A-Fa-f]{2}([:-][0-9A-Fa-f]{2}){5}$`)

func huaweiMAC(value string) string {
	value = strings.TrimSpace(value)
	if !huaweiMACRE.MatchString(value) {
		return ""
	}
	return strings.ToUpper(strings.ReplaceAll(value, "-", ":"))
}

// huaweiAddress normalises one IPv4 address, or returns "" for the 0.0.0.0 a
// disconnected connection reports.
func huaweiAddress(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "--" || value == "0.0.0.0" {
		return ""
	}
	return value
}

// huaweiList splits the ONT's comma-separated DNS lists.
func huaweiList(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" && item != "0.0.0.0" && item != "--" {
			out = append(out, item)
		}
	}
	return out
}
