package network

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// OpenWrt reads the stock rpcd/netifd/hostapd ubus APIs. Password resolves
// Device.PasswordEnv; an empty Device.Username uses OpenWrt's default "root".
// Sessions are scoped to one operation and never exposed in observations/errors.
// Client may supply a trusted TLS transport; redirects are always disabled.
type OpenWrt struct {
	Client   *http.Client
	Password func(string) (string, error)
}

const openwrtAnonymous = "00000000000000000000000000000000"
const openwrtMaxBody = 4 << 20

type openwrtSession struct {
	adapter   *OpenWrt
	device    Device
	endpoint  string
	token     string
	id        uint64
	refreshed bool
}
type openwrtStatusError struct{ code int }

func (e openwrtStatusError) Error() string { return fmt.Sprintf("OpenWrt ubus status %d", e.code) }
func (e openwrtStatusError) Is(target error) bool {
	return (target == ErrUnsupported && (e.code == 3 || e.code == 4 || e.code == 6 || e.code == 8)) || (target == ErrInvalid && e.code == 2)
}

func (o *OpenWrt) session(ctx context.Context, d Device) (*openwrtSession, error) {
	address := d.Address
	if !strings.Contains(address, "://") {
		address = "https://" + address
	}
	u, err := url.Parse(address)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("%w: invalid OpenWrt HTTP address", ErrInvalid)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/ubus"
	} else if strings.TrimRight(u.Path, "/") != "/ubus" {
		return nil, fmt.Errorf("%w: OpenWrt endpoint must be /ubus", ErrInvalid)
	}
	s := &openwrtSession{adapter: o, device: d, endpoint: u.String()}
	if err := s.login(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *openwrtSession) rpc(ctx context.Context, method string, params []any, out any) error {
	s.id++
	id := s.id
	body, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      uint64 `json:"id"`
		Method  string `json:"method"`
		Params  []any  `json:"params"`
	}{"2.0", id, method, params})
	if err != nil {
		return errors.New("OpenWrt request encoding failed")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("OpenWrt request creation failed")
	}
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{}
	if s.adapter.Client != nil {
		client = *s.adapter.Client
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("OpenWrt request failed: %w", ctx.Err())
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("OpenWrt request failed: %w", context.DeadlineExceeded)
		}
		if errors.Is(err, context.Canceled) {
			return fmt.Errorf("OpenWrt request failed: %w", context.Canceled)
		}
		return errors.New("OpenWrt HTTP request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return openwrtStatusError{6}
		case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
			return openwrtStatusError{8}
		}
		return fmt.Errorf("OpenWrt HTTP status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, openwrtMaxBody+1))
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("OpenWrt response read failed: %w", ctx.Err())
		}
		return errors.New("OpenWrt response read failed")
	}
	if len(data) > openwrtMaxBody {
		return errors.New("OpenWrt response exceeds size limit")
	}
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      uint64          `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &envelope) != nil || envelope.JSONRPC != "2.0" || envelope.ID != id {
		return errors.New("OpenWrt malformed JSON-RPC response")
	}
	if envelope.Error != nil {
		if len(envelope.Result) != 0 {
			return errors.New("OpenWrt malformed JSON-RPC response")
		}
		switch envelope.Error.Code {
		case -32002:
			return openwrtStatusError{6}
		case -32601:
			return openwrtStatusError{3}
		}
		return errors.New("OpenWrt JSON-RPC request failed")
	}
	payload := envelope.Result
	if method == "call" {
		var result []json.RawMessage
		var code int
		if json.Unmarshal(payload, &result) != nil || len(result) < 1 || len(result) > 2 || bytes.Equal(bytes.TrimSpace(result[0]), []byte("null")) || json.Unmarshal(result[0], &code) != nil || code < 0 || code > 13 {
			return errors.New("OpenWrt malformed ubus result")
		}
		if code != 0 {
			return openwrtStatusError{code}
		}
		if len(result) == 1 && (len(params) < 3 || params[2] != "del_client") {
			return errors.New("OpenWrt missing ubus read payload")
		}
		payload = json.RawMessage(`{}`)
		if len(result) == 2 {
			payload = result[1]
		}
	}
	if len(payload) == 0 || bytes.Equal(bytes.TrimSpace(payload), []byte("null")) || json.Unmarshal(payload, out) != nil {
		return errors.New("OpenWrt malformed ubus payload")
	}
	return nil
}

func (s *openwrtSession) login(ctx context.Context) error {
	if s.adapter.Password == nil {
		return fmt.Errorf("%w: OpenWrt password resolver is required", ErrInvalid)
	}
	password, err := s.adapter.Password(s.device.PasswordEnv)
	if err != nil {
		return fmt.Errorf("%w: OpenWrt password resolution failed", ErrInvalid)
	}
	username := s.device.Username
	if username == "" {
		username = "root"
	}
	var result struct {
		Token string `json:"ubus_rpc_session"`
	}
	err = s.rpc(ctx, "call", []any{openwrtAnonymous, "session", "login", map[string]any{"username": username, "password": password}}, &result)
	if err != nil {
		var status openwrtStatusError
		if errors.As(err, &status) && status.code == 6 {
			return fmt.Errorf("%w: OpenWrt login rejected", ErrInvalid)
		}
		return err
	}
	if len(result.Token) != 32 {
		return fmt.Errorf("%w: OpenWrt login returned no valid session", ErrInvalid)
	}
	for _, c := range result.Token {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return fmt.Errorf("%w: OpenWrt login returned no valid session", ErrInvalid)
		}
	}
	s.token = result.Token
	return nil
}

// call retries a permission failure once, after reauthentication, for reads only.
// A mutation is never replayed, including when the HTTP outcome is ambiguous.
func (s *openwrtSession) call(ctx context.Context, object, method string, args any, out any, read bool) error {
	err := s.rpc(ctx, "call", []any{s.token, object, method, args}, out)
	var status openwrtStatusError
	if read && !s.refreshed && errors.As(err, &status) && status.code == 6 {
		s.refreshed = true
		if loginErr := s.login(ctx); loginErr != nil {
			return loginErr
		}
		return s.rpc(ctx, "call", []any{s.token, object, method, args}, out)
	}
	return err
}

func openwrtCollection(now time.Time, errs ...error) Collection {
	c := Collection{State: "ok", ObservedAt: now}
	for _, err := range errs {
		if err != nil {
			c.State = "unsupported"
			c.Error = "OpenWrt method unavailable or permission denied"
			if !errors.Is(err, ErrUnsupported) {
				c.State = "error"
				c.Error = err.Error()
				return c
			}
		}
	}
	return c
}
func openwrtCapability(c Collection) Capability {
	switch c.State {
	case "ok":
		return Supported()
	case "unsupported":
		return Unsupported(c.Error)
	default:
		return Capability{State: "unknown", Reason: c.Error}
	}
}
func openwrtKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Read reports each collection independently. ObservedAt marks the read attempt's
// start; sequential ubus calls do not form an atomic device snapshot. Identity
// roles combine declared Device.Roles with AP only when an AP interface is seen;
// OpenWrt distribution or a software bridge alone does not imply router/switch.
// Stock ubus has no portable FDB, bridge VLAN, PVID, or neighbor API.
func (o *OpenWrt) Read(ctx context.Context, d Device) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	now := time.Now().UTC()
	snap := Snapshot{DeviceID: d.ID, ObservedAt: now, Capabilities: Capabilities{}, Collections: map[string]Collection{}}
	for _, name := range []string{"vlans", "pvids", "fdb", "neighbors"} {
		snap.Collections[name] = Collection{State: "unsupported", ObservedAt: now, Error: "No portable stock ubus API"}
	}
	for _, name := range []string{"vlans.read", "pvids.read", "fdb.lookup", "fdb.list", "neighbors.read", "ports.configure", "vlans.configure", "wireless.configure", "poe.configure", "device.reboot"} {
		snap.Capabilities[name] = Unsupported("No implemented stock ubus API")
	}
	s, err := o.session(ctx, d)
	if err != nil {
		return snap, err
	}
	var board struct {
		Model     string `json:"model"`
		BoardName string `json:"board_name"`
		Hostname  string `json:"hostname"`
		Release   struct {
			Distribution string `json:"distribution"`
			Version      string `json:"version"`
			Revision     string `json:"revision"`
		} `json:"release"`
	}
	boardErr := s.call(ctx, "system", "board", map[string]any{}, &board, true)
	if boardErr == nil && board.Model == "" && board.Hostname == "" && board.Release.Distribution == "" {
		boardErr = errors.New("OpenWrt malformed system board")
	}
	var info struct {
		Uptime *uint64 `json:"uptime"`
	}
	infoErr := s.call(ctx, "system", "info", map[string]any{}, &info, true)
	if infoErr == nil && info.Uptime == nil {
		infoErr = errors.New("OpenWrt malformed system info")
	}
	snap.Identity = Identity{Roles: append([]Role(nil), d.Roles...)}
	if boardErr == nil {
		snap.Identity.Model = board.Model
		snap.Identity.Hostname = board.Hostname
		snap.Identity.Firmware = strings.TrimSpace(board.Release.Distribution + " " + board.Release.Version + " " + board.Release.Revision)
	}
	if infoErr == nil {
		snap.Identity.UptimeSeconds = info.Uptime
	}
	snap.Collections["identity"] = openwrtCollection(now, boardErr, infoErr)
	snap.Capabilities["identity.read"] = openwrtCapability(snap.Collections["identity"])
	var devices map[string]openwrtDevice
	deviceErr := s.call(ctx, "network.device", "status", map[string]any{}, &devices, true)
	if deviceErr == nil && devices == nil {
		deviceErr = errors.New("OpenWrt malformed device status")
	}
	if deviceErr != nil {
		devices = nil
	}
	for _, name := range openwrtKeys(devices) {
		dev := devices[name]
		if name == "" || (dev.Up == nil && dev.Type == "") {
			deviceErr = openwrtMergeError(deviceErr, errors.New("OpenWrt malformed device status entry"))
			continue
		}
		p := Port{ID: name, Name: name, Kind: openwrtPortKind(dev.Type), MAC: dev.MAC, ParentID: dev.Parent, Members: dev.Members, Enabled: dev.Up, LinkUp: dev.Carrier, Duplex: dev.Duplex, RXBytes: dev.Statistics.RXBytes, TXBytes: dev.Statistics.TXBytes, RXPackets: dev.Statistics.RXPackets, TXPackets: dev.Statistics.TXPackets, RXErrors: dev.Statistics.RXErrors, TXErrors: dev.Statistics.TXErrors}
		if len(dev.Members) > 0 {
			p.Kind = "bridge"
		}
		p.SpeedMbps, p.Duplex = openwrtSpeed(dev.Speed, p.Duplex)
		snap.Ports = append(snap.Ports, p)
		if mac, err := openwrtMAC(dev.MAC); err == nil {
			snap.Identity.MACs = append(snap.Identity.MACs, mac)
		}
	}
	var interfaces struct {
		Interfaces []struct {
			Device   string           `json:"device"`
			L3Device string           `json:"l3_device"`
			IPv4     []openwrtAddress `json:"ipv4-address"`
			IPv6     []openwrtAddress `json:"ipv6-address"`
		} `json:"interface"`
	}
	addressErr := s.call(ctx, "network.interface", "dump", map[string]any{}, &interfaces, true)
	if addressErr == nil && interfaces.Interfaces == nil {
		addressErr = errors.New("OpenWrt malformed interface dump")
	}
	if addressErr != nil {
		interfaces.Interfaces = nil
	}
	for _, intf := range interfaces.Interfaces {
		name := intf.L3Device
		if name == "" {
			name = intf.Device
		}
		for i := range snap.Ports {
			if snap.Ports[i].ID == name {
				for _, addr := range append(intf.IPv4, intf.IPv6...) {
					if ip := net.ParseIP(addr.Address); ip != nil && addr.Mask != nil {
						bits := 128
						if ip.To4() != nil {
							bits = 32
						}
						if *addr.Mask >= 0 && *addr.Mask <= bits {
							snap.Ports[i].Addresses = append(snap.Ports[i].Addresses, fmt.Sprintf("%s/%d", ip.String(), *addr.Mask))
						}
					}
				}
			}
		}
	}
	snap.Collections["ports"] = openwrtCollection(now, deviceErr, addressErr)
	snap.Collections["statistics"] = openwrtCollection(now, deviceErr)
	snap.Capabilities["ports.read"] = openwrtCapability(snap.Collections["ports"])
	snap.Capabilities["statistics.read"] = openwrtCapability(snap.Collections["statistics"])
	radios, wireless, wirelessErr := s.wireless(ctx)
	snap.Radios = radios
	snap.Wireless = wireless
	snap.Collections["radios"] = openwrtCollection(now, wirelessErr)
	snap.Collections["wireless"] = openwrtCollection(now, wirelessErr)
	associationErr := wirelessErr
	wirelessCollectionErr := wirelessErr
	disconnectSupported := false
	disconnectAssessmentErr := wirelessErr
	infoApplied := map[string]bool{}
	for i := range snap.Wireless {
		w := &snap.Wireless[i]
		if w.Mode != "ap" && w.Mode != "ap-wds" {
			associationErr = openwrtMergeError(associationErr, fmt.Errorf("%w: stock hostapd cannot enumerate station-mode associations", ErrUnsupported))
			continue
		}
		apDeclared := false
		for _, role := range snap.Identity.Roles {
			if role == AP {
				apDeclared = true
				break
			}
		}
		if !apDeclared {
			snap.Identity.Roles = append(snap.Identity.Roles, AP)
		}
		object := "hostapd." + w.InterfaceID
		status, statusErr := s.hostStatus(ctx, w.InterfaceID)
		if statusErr == nil {
			if status.SSID != "" {
				w.SSID = status.SSID
			}
			if mac, e := openwrtMAC(status.BSSID); e == nil {
				w.BSSID = mac
			}
			for j := range snap.Radios {
				if snap.Radios[j].ID == w.RadioID && status.Channel != nil && *status.Channel > 0 {
					snap.Radios[j].Channel = status.Channel
				}
			}
		}
		if statusErr == nil && status.Airtime != nil && status.Airtime.Utilization != nil {
			for j := range snap.Radios {
				if snap.Radios[j].ID == w.RadioID {
					snap.Radios[j].UtilizationPercent = status.Airtime.Utilization
				}
			}
		}
		// iwinfo carries what netifd does not: the radio's observed width, noise
		// floor, transmit power and regulatory country. It is also the fallback
		// for an SSID and channel when hostapd's status object is not granted.
		// It is enrichment, so its failure leaves fields unknown.
		infoOK := infoApplied[w.RadioID]
		if !infoOK {
			var info openwrtIWInfo
			if s.call(ctx, "iwinfo", "info", map[string]any{"device": w.InterfaceID}, &info, true) == nil {
				openwrtApplyRadioInfo(snap.Radios, w.RadioID, info)
				infoApplied[w.RadioID] = true
				infoOK = true
				if w.SSID == "" {
					w.SSID = info.SSID
				}
				if w.BSSID == "" {
					if mac, e := openwrtMAC(info.BSSID); e == nil {
						w.BSSID = mac
					}
				}
				if info.Channel != nil && *info.Channel > 0 {
					for j := range snap.Radios {
						if snap.Radios[j].ID == w.RadioID && (snap.Radios[j].Channel == nil || *snap.Radios[j].Channel <= 0) {
							snap.Radios[j].Channel = info.Channel
						}
					}
				}
			}
		}
		if statusErr != nil {
			// A hostapd status failure does not make the radio unreadable when
			// iwinfo supplied the observation, but it still leaves disconnect
			// discovery incomplete rather than proving the method absent.
			disconnectAssessmentErr = openwrtMergeError(disconnectAssessmentErr, statusErr)
			if !infoOK {
				wirelessCollectionErr = openwrtMergeError(wirelessCollectionErr, statusErr)
			}
		}
		var hostapd struct {
			Clients map[string]openwrtClient `json:"clients"`
		}
		hostapdErr := s.call(ctx, object, "get_clients", map[string]any{}, &hostapd, true)
		if hostapdErr == nil && hostapd.Clients == nil {
			hostapdErr = errors.New("OpenWrt malformed hostapd client list")
		}
		if hostapdErr != nil {
			hostapd.Clients = nil
		}
		var iw struct {
			Results []openwrtIWClient `json:"results"`
		}
		iwErr := s.call(ctx, "iwinfo", "assoclist", map[string]any{"device": w.InterfaceID}, &iw, true)
		if iwErr != nil {
			iw.Results = nil
		}
		if hostapdErr != nil && iwErr != nil {
			associationErr = openwrtMergeError(openwrtMergeError(associationErr, hostapdErr), iwErr)
			disconnectAssessmentErr = openwrtMergeError(disconnectAssessmentErr, hostapdErr)
			continue
		}
		// Either source can enumerate an association. hostapd contributes MFP,
		// WMM and airtime; iwinfo contributes signal, rates, retries and traffic.
		type observation struct {
			hostapd *openwrtClient
			iwinfo  *openwrtIWClient
		}
		observed := map[string]*observation{}
		for key, value := range hostapd.Clients {
			c := value
			if c.Associated != nil && !*c.Associated {
				continue
			}
			normalized, e := openwrtMAC(key)
			if e != nil {
				associationErr = openwrtMergeError(associationErr, errors.New("OpenWrt malformed client MAC"))
				continue
			}
			if c.Associated == nil {
				associationErr = openwrtMergeError(associationErr, errors.New("OpenWrt malformed hostapd client entry"))
				continue
			}
			observed[normalized] = &observation{hostapd: &c}
		}
		for index := range iw.Results {
			c := &iw.Results[index]
			normalized, e := openwrtMAC(c.MAC)
			if e != nil {
				associationErr = openwrtMergeError(associationErr, errors.New("OpenWrt malformed client MAC"))
				continue
			}
			if observed[normalized] == nil {
				observed[normalized] = &observation{}
			}
			observed[normalized].iwinfo = c
		}
		macs := make([]string, 0, len(observed))
		for mac := range observed {
			macs = append(macs, mac)
		}
		sort.Strings(macs)
		for _, mac := range macs {
			o := observed[mac]
			association := Association{MAC: mac, NetworkID: w.ID, RadioID: w.RadioID}
			if o.hostapd != nil {
				association.SignalDBm = o.hostapd.Signal
				association.MFP = o.hostapd.MFP
				association.WMM = o.hostapd.WMM
				if o.hostapd.Airtime != nil {
					association.TXAirtimeUS = o.hostapd.Airtime.TX
				}
			}
			if c := o.iwinfo; c != nil {
				if c.Signal != nil {
					association.SignalDBm = c.Signal
				}
				association.SignalAvgDBm = c.SignalAvg
				association.NoiseDBm = c.Noise
				association.InactiveMS = c.Inactive
				association.ConnectedSeconds = c.Connected
				association.ExpectedMbps = openwrtKbps(c.Thr)
				association.RXBytes = c.RX.Bytes
				association.TXBytes = c.TX.Bytes
				association.RXPackets = c.RX.Packets
				association.TXPackets = c.TX.Packets
				association.RXRateMbps = openwrtRate(c.RX.Rate)
				association.TXRateMbps = openwrtRate(c.TX.Rate)
				association.RXMCS = c.RX.MCS
				association.TXMCS = c.TX.MCS
				association.RXWidthMHz = c.RX.MHz
				association.TXWidthMHz = c.TX.MHz
				association.Retries = c.TX.Retries
				association.Failed = c.TX.Failed
			}
			snap.Associations = append(snap.Associations, association)
		}
		if statusErr == nil {
			allowedErr := s.disconnectAllowed(ctx, w.InterfaceID)
			if allowedErr == nil {
				disconnectSupported = true
			} else if !errors.Is(allowedErr, ErrUnsupported) {
				disconnectAssessmentErr = openwrtMergeError(disconnectAssessmentErr, allowedErr)
			}
		}
	}
	for _, w := range snap.Wireless {
		found := false
		for i := range snap.Ports {
			if snap.Ports[i].ID == w.InterfaceID {
				snap.Ports[i].Kind = "wireless"
				if snap.Ports[i].MAC == "" {
					snap.Ports[i].MAC = w.BSSID
				}
				found = true
				break
			}
		}
		if !found {
			snap.Ports = append(snap.Ports, Port{ID: w.InterfaceID, Name: w.InterfaceID, Kind: "wireless", MAC: w.BSSID})
		}
	}
	sort.Slice(snap.Ports, func(i, j int) bool { return snap.Ports[i].ID < snap.Ports[j].ID })
	snap.Collections["wireless"] = openwrtCollection(now, wirelessCollectionErr)
	snap.Collections["associations"] = openwrtCollection(now, associationErr)
	for _, name := range []string{"radios", "wireless", "associations"} {
		snap.Capabilities[name+".read"] = openwrtCapability(snap.Collections[name])
	}
	snap.Capabilities["client.disconnect"] = Unsupported("No observed hostapd interface with del_client permission")
	if disconnectSupported {
		snap.Capabilities["client.disconnect"] = Supported()
	} else if disconnectAssessmentErr != nil {
		snap.Capabilities["client.disconnect"] = Capability{State: "unknown", Reason: "OpenWrt client disconnect discovery was incomplete"}
	}
	allUnsupported := true
	for _, name := range []string{"identity", "ports", "radios", "wireless", "associations"} {
		if snap.Collections[name].State == "ok" {
			return snap, nil
		}
		if snap.Collections[name].State != "unsupported" {
			allUnsupported = false
		}
	}
	if allUnsupported {
		return snap, fmt.Errorf("%w: no OpenWrt core collection is accessible", ErrUnsupported)
	}
	return snap, errors.New("OpenWrt could not read any core collection")
}

func openwrtMergeError(previous, next error) error {
	if previous == nil || (errors.Is(previous, ErrUnsupported) && !errors.Is(next, ErrUnsupported)) {
		return next
	}
	return previous
}

type openwrtAddress struct {
	Address string `json:"address"`
	Mask    *int   `json:"mask"`
}
type openwrtDevice struct {
	Type       string          `json:"type"`
	MAC        string          `json:"macaddr"`
	Parent     string          `json:"parent"`
	Members    []string        `json:"bridge-members"`
	Up         *bool           `json:"up"`
	Carrier    *bool           `json:"carrier"`
	Speed      json.RawMessage `json:"speed"`
	Duplex     string          `json:"duplex"`
	Statistics struct {
		RXBytes   *uint64 `json:"rx_bytes"`
		TXBytes   *uint64 `json:"tx_bytes"`
		RXPackets *uint64 `json:"rx_packets"`
		TXPackets *uint64 `json:"tx_packets"`
		RXErrors  *uint64 `json:"rx_errors"`
		TXErrors  *uint64 `json:"tx_errors"`
	} `json:"statistics"`
}

func openwrtPortKind(kind string) string {
	switch strings.ToLower(kind) {
	case "ethernet":
		return "ethernet"
	case "fiber":
		return "fiber"
	case "bridge":
		return "bridge"
	case "8021q", "8021ad", "vlan":
		return "vlan"
	case "bonding", "bond", "lag":
		return "lag"
	case "wireless":
		return "wireless"
	default:
		// netifd's generic "Network device" also includes virtual devices.
		return "other"
	}
}

func openwrtSpeed(raw json.RawMessage, duplex string) (*uint64, string) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, duplex
	}
	var number uint64
	if json.Unmarshal(raw, &number) == nil {
		return &number, duplex
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return nil, duplex
	}
	if strings.HasSuffix(text, "F") {
		duplex = "full"
		text = strings.TrimSuffix(text, "F")
	} else if strings.HasSuffix(text, "H") {
		duplex = "half"
		text = strings.TrimSuffix(text, "H")
	}
	if n, e := strconv.ParseUint(text, 10, 64); e == nil {
		return &n, duplex
	}
	return nil, duplex
}

type openwrtWirelessConfig struct {
	Band       string          `json:"band"`
	Channel    json.RawMessage `json:"channel"`
	HTMode     string          `json:"htmode"`
	Disabled   *bool           `json:"disabled"`
	Mode       string          `json:"mode"`
	SSID       string          `json:"ssid"`
	Encryption string          `json:"encryption"`
	Networks   []string        `json:"network"`
}
type openwrtWirelessRadio struct {
	Up         *bool                 `json:"up"`
	Disabled   *bool                 `json:"disabled"`
	Config     openwrtWirelessConfig `json:"config"`
	Interfaces []struct {
		Ifname string                `json:"ifname"`
		Config openwrtWirelessConfig `json:"config"`
		IWInfo *openwrtIWInfo        `json:"iwinfo"`
	} `json:"interfaces"`
}

func (s *openwrtSession) wireless(ctx context.Context) ([]Radio, []WirelessNetwork, error) {
	var raw map[string]openwrtWirelessRadio
	err := s.call(ctx, "network.wireless", "status", map[string]any{}, &raw, true)
	if err != nil {
		// Some builds do not grant netifd's wireless object to the management
		// account. LuCI's own read model exposes the same radios and interfaces,
		// including each interface's iwinfo, through a method those builds do
		// grant; use it rather than reporting the radio as unreadable.
		raw = nil
		err = s.call(ctx, "luci-rpc", "getWirelessDevices", map[string]any{}, &raw, true)
	}
	if err != nil {
		return nil, nil, err
	}
	if raw == nil {
		return nil, nil, errors.New("OpenWrt malformed wireless status")
	}
	var radios []Radio
	var networks []WirelessNetwork
	var shapeErr error
	for _, id := range openwrtKeys(raw) {
		r := raw[id]
		if id == "" || (r.Up == nil && r.Disabled == nil) {
			shapeErr = errors.New("OpenWrt malformed wireless radio")
			continue
		}
		enabled := (*bool)(nil)
		if r.Disabled != nil {
			enabled = Ptr(!*r.Disabled)
		} else if r.Config.Disabled != nil {
			enabled = Ptr(!*r.Config.Disabled)
		}
		// Config.channel can be a preference (including auto); only get_status
		// later supplies the radio's observed runtime channel.
		radio := Radio{ID: id, Enabled: enabled, Band: r.Config.Band, ConfiguredWidthMHz: openwrtWidth(r.Config.HTMode)}
		radioIndex := len(radios)
		radios = append(radios, radio)
		for _, i := range r.Interfaces {
			if i.Ifname == "" {
				continue
			}
			network := WirelessNetwork{ID: i.Ifname, RadioID: id, InterfaceID: i.Ifname, SSID: i.Config.SSID, Mode: i.Config.Mode, Encryption: i.Config.Encryption, NetworkNames: i.Config.Networks}
			if i.IWInfo != nil {
				openwrtApplyRadioInfo(radios[radioIndex:], id, *i.IWInfo)
				if network.SSID == "" {
					network.SSID = i.IWInfo.SSID
				}
				if network.BSSID == "" {
					network.BSSID = i.IWInfo.BSSID
				}
			}
			networks = append(networks, network)
		}
	}
	return radios, networks, shapeErr
}

type openwrtHostStatus struct {
	BSSID   string `json:"bssid"`
	SSID    string `json:"ssid"`
	Channel *int   `json:"channel"`
	Freq    *int   `json:"freq"`
	Airtime *struct {
		Utilization *float64 `json:"utilization"`
	} `json:"airtime"`
}

func (s *openwrtSession) hostStatus(ctx context.Context, iface string) (openwrtHostStatus, error) {
	var status openwrtHostStatus
	err := s.call(ctx, "hostapd."+iface, "get_status", map[string]any{}, &status, true)
	if err == nil {
		if _, macErr := openwrtMAC(status.BSSID); macErr != nil {
			err = errors.New("OpenWrt malformed hostapd status")
		}
	}
	return status, err
}

type openwrtClient struct {
	Signal     *int  `json:"signal"`
	Associated *bool `json:"assoc"`
	MFP        *bool `json:"mfp"`
	WMM        *bool `json:"wmm"`
	Airtime    *struct {
		TX *uint64 `json:"tx"`
	} `json:"airtime"`
}
type openwrtIWRate struct {
	Rate    *uint64 `json:"rate"`
	Bytes   *uint64 `json:"bytes"`
	Packets *uint64 `json:"packets"`
	Retries *uint64 `json:"retries"`
	Failed  *uint64 `json:"failed"`
	MCS     *int    `json:"mcs"`
	MHz     *int    `json:"mhz"`
	HT      *bool   `json:"ht"`
	VHT     *bool   `json:"vht"`
	HE      *bool   `json:"he"`
	EHT     *bool   `json:"eht"`
	ShortGI *bool   `json:"short_gi"`
}
type openwrtIWClient struct {
	MAC       string        `json:"mac"`
	Signal    *int          `json:"signal"`
	SignalAvg *int          `json:"signal_avg"`
	Noise     *int          `json:"noise"`
	Inactive  *uint64       `json:"inactive"`
	Connected *uint64       `json:"connected_time"`
	Thr       *uint64       `json:"thr"`
	RX        openwrtIWRate `json:"rx"`
	TX        openwrtIWRate `json:"tx"`
}
type openwrtIWInfo struct {
	SSID      string   `json:"ssid"`
	BSSID     string   `json:"bssid"`
	Channel   *int     `json:"channel"`
	Frequency *int     `json:"frequency"`
	TxPower   *int     `json:"txpower"`
	Noise     *int     `json:"noise"`
	Country   string   `json:"country"`
	HTMode    string   `json:"htmode"`
	HWMode    string   `json:"hwmode"`
	HWMods    []string `json:"hwmodes"`
}

func openwrtRate(rate *uint64) *float64 {
	if rate == nil {
		return nil
	}
	return Ptr(float64(*rate) / 1000)
}
func openwrtKbps(value *uint64) *float64 {
	if value == nil {
		return nil
	}
	return Ptr(float64(*value) / 1000)
}
func openwrtWidth(htmode string) *int {
	mode := strings.ToUpper(strings.TrimSpace(htmode))
	for _, prefix := range []string{"EHT", "HE", "VHT", "HT"} {
		if strings.HasPrefix(mode, prefix) {
			mode = strings.TrimPrefix(mode, prefix)
			break
		}
	}
	for _, width := range []int{320, 160, 80, 40, 20} {
		if mode == strconv.Itoa(width) {
			return Ptr(width)
		}
	}
	return nil
}
func openwrtApplyRadioInfo(radios []Radio, radioID string, info openwrtIWInfo) {
	for i := range radios {
		if radios[i].ID != radioID {
			continue
		}
		radios[i].NoiseDBm = info.Noise
		radios[i].TxPowerDBm = info.TxPower
		radios[i].Country = info.Country
		if width := openwrtWidth(info.HTMode); width != nil {
			radios[i].WidthMHz = width
		}
		if len(info.HWMods) > 0 {
			radios[i].Protocols = append([]string(nil), info.HWMods...)
		}
		return
	}
}
func openwrtMAC(value string) (string, error) {
	mac, err := net.ParseMAC(value)
	if err != nil || len(mac) != 6 || mac[0]&1 != 0 || bytes.Equal(mac, make([]byte, 6)) {
		return "", fmt.Errorf("%w: invalid unicast client MAC", ErrInvalid)
	}
	return mac.String(), nil
}

// disconnectAllowed requires both method existence and session ACL permission.
// uhttpd's JSON-RPC list returns an object directly, unlike call's [status,data].
func (s *openwrtSession) disconnectAllowed(ctx context.Context, iface string) error {
	object := "hostapd." + iface
	var methods map[string]map[string]json.RawMessage
	if err := s.rpc(ctx, "list", []any{s.token, object}, &methods); err != nil {
		return fmt.Errorf("OpenWrt method discovery failed: %w", err)
	}
	signature, ok := methods[object]["del_client"]
	if !ok || len(signature) == 0 {
		return fmt.Errorf("%w: hostapd del_client unavailable", ErrUnsupported)
	}
	var args map[string]json.RawMessage
	if json.Unmarshal(signature, &args) != nil {
		return fmt.Errorf("%w: invalid hostapd del_client signature", ErrUnsupported)
	}
	for name, expected := range map[string]string{"addr": "String", "reason": "Integer", "deauth": "Boolean", "ban_time": "Integer"} {
		var actual string
		if json.Unmarshal(args[name], &actual) != nil || !strings.EqualFold(actual, expected) {
			return fmt.Errorf("%w: incompatible hostapd del_client signature", ErrUnsupported)
		}
	}
	var permission struct {
		Access *bool `json:"access"`
	}
	if err := s.call(ctx, "session", "access", map[string]any{"scope": "ubus", "object": object, "function": "del_client"}, &permission, true); err != nil {
		return fmt.Errorf("OpenWrt del_client permission check failed: %w", err)
	}
	if permission.Access == nil {
		return errors.New("OpenWrt malformed session access response")
	}
	if !*permission.Access {
		return fmt.Errorf("%w: hostapd del_client permission denied", ErrUnsupported)
	}
	return nil
}
func (s *openwrtSession) prepareDisconnect(ctx context.Context, op Operation) (Operation, error) {
	if op.Kind != "client.disconnect" {
		return op, fmt.Errorf("%w: OpenWrt operation unavailable", ErrUnsupported)
	}
	mac, err := openwrtMAC(op.ClientMAC)
	if err != nil {
		return op, err
	}
	op.ClientMAC = mac
	_, networks, err := s.wireless(ctx)
	if err != nil {
		return op, err
	}
	observed := false
	for _, w := range networks {
		if op.Target == w.InterfaceID && (w.Mode == "ap" || w.Mode == "ap-wds") {
			observed = true
			break
		}
	}
	if !observed {
		return op, fmt.Errorf("%w: target is not an observed hostapd interface", ErrInvalid)
	}
	if _, err := s.hostStatus(ctx, op.Target); err != nil {
		return op, err
	}
	return op, s.disconnectAllowed(ctx, op.Target)
}
func (o *OpenWrt) Plan(ctx context.Context, d Device, op Operation) (OperationPlan, error) {
	plan := OperationPlan{DeviceID: d.ID, Operation: op, Impact: "Disconnect the wireless client; it may reconnect immediately"}
	if op.Kind != "client.disconnect" {
		return plan, fmt.Errorf("%w: OpenWrt operation unavailable", ErrUnsupported)
	}
	if _, err := openwrtMAC(op.ClientMAC); err != nil {
		return plan, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := o.session(ctx, d)
	if err != nil {
		return plan, err
	}
	op, err = s.prepareDisconnect(ctx, op)
	if err != nil {
		return plan, err
	}
	plan.Operation = op
	plan.Supported = true
	return plan, nil
}
func (o *OpenWrt) Execute(ctx context.Context, d Device, op Operation) (OperationResult, error) {
	result := OperationResult{DeviceID: d.ID, Operation: op, State: "unknown", ObservedAt: time.Now().UTC()}
	if op.Kind != "client.disconnect" {
		return result, fmt.Errorf("%w: OpenWrt operation unavailable", ErrUnsupported)
	}
	if _, err := openwrtMAC(op.ClientMAC); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := o.session(ctx, d)
	if err != nil {
		return result, err
	}
	op, err = s.prepareDisconnect(ctx, op)
	if err != nil {
		return result, err
	}
	result.Operation = op
	var response map[string]json.RawMessage
	err = s.call(ctx, "hostapd."+op.Target, "del_client", map[string]any{"addr": op.ClientMAC, "deauth": true, "reason": 5, "ban_time": 0}, &response, false)
	result.ObservedAt = time.Now().UTC()
	if err != nil {
		return result, err
	}
	result.State = "accepted"
	return result, nil
}

var _ Adapter = (*OpenWrt)(nil)
var _ Operator = (*OpenWrt)(nil)
