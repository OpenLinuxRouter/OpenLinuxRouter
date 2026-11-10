package network

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Mercury reads the captured SE109 Pro web protocol. Password receives the
// configured environment variable name. Each operation owns a fresh cookie jar,
// so even devices sharing a hostname cannot inherit each other's login session.
// Client supplies transport settings; its jar and redirect policy are not used.
type Mercury struct {
	Client   *http.Client
	Password func(string) (string, error)
}

var _ Adapter = (*Mercury)(nil)
var _ FDBLookup = (*Mercury)(nil)
var errMercuryHTTP = errors.New("mercury: management request failed")
var errMercuryAuth = errors.New("mercury: authentication failed")

type mercurySession struct {
	adapter        *Mercury
	device         Device
	base           *url.URL
	client         http.Client
	loginAttempted bool
	loginErr       error
}

func (m *Mercury) session(d Device) (*mercurySession, error) {
	u, e := url.Parse(d.Address)
	if e != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("%w: Mercury address must be an HTTP origin", ErrInvalid)
	}
	u.Path = "/"
	u.RawPath = ""
	s := &mercurySession{adapter: m, device: d, base: u}
	if m.Client != nil {
		s.client = *m.Client
	}
	if s.client.Timeout <= 0 || s.client.Timeout > 10*time.Second {
		s.client.Timeout = 10 * time.Second
	}
	s.client.Jar, _ = cookiejar.New(nil)
	// Do not forward cookies or credentials, including to a same-origin redirect.
	s.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return s, nil
}
func (s *mercurySession) request(ctx context.Context, path string, form url.Values) (string, error) {
	u := *s.base
	parts := strings.SplitN(path, "?", 2)
	u.Path = "/" + parts[0]
	if len(parts) == 2 {
		u.RawQuery = parts[1]
	}
	method := http.MethodGet
	var body io.Reader
	if form != nil {
		method = http.MethodPost
		body = strings.NewReader(form.Encode())
	}
	req, e := http.NewRequestWithContext(ctx, method, u.String(), body)
	if e != nil {
		return "", errMercuryHTTP
	}
	req.Header.Set("Cache-Control", "no-cache")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, e := s.client.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errMercuryHTTP
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errMercuryHTTP
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, mercuryBodyLimit+1))
	if e != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errMercuryHTTP
	}
	if len(b) > mercuryBodyLimit {
		return "", errors.New("mercury: management page exceeds size limit")
	}
	return string(b), nil
}
func mercuryIsLogin(page string) bool {
	return strings.Contains(page, "plain_password") && strings.Contains(page, "logon.cgi")
}

var mercuryTokenRE = regexp.MustCompile(`\bg_tid\s*=\s*([0-9]{1,20})\s*[,;]`)

func mercuryToken(page string) (string, error) {
	v := mercuryTokenRE.FindStringSubmatch(page)
	if v == nil {
		return "", errMercuryAuth
	}
	if _, e := strconv.ParseUint(v[1], 10, 64); e != nil {
		return "", errMercuryAuth
	}
	return v[1], nil
}
func (s *mercurySession) login(ctx context.Context) (string, error) {
	if s.loginAttempted {
		if s.loginErr != nil {
			return "", s.loginErr
		}
		return "", errMercuryAuth
	}
	s.loginAttempted = true
	s.loginErr = errMercuryAuth
	if s.device.Username == "" || (s.device.Password == "" && s.device.PasswordEnv == "") {
		return "", errMercuryAuth
	}
	password := s.adapter.Password
	if password == nil {
		password = func(key string) (string, error) {
			v, ok := os.LookupEnv(key)
			if !ok {
				return "", errMercuryAuth
			}
			return v, nil
		}
	}
	secret, e := s.device.Credential(password)
	if e != nil || secret == "" {
		return "", errMercuryAuth
	}
	crypto, e := s.request(ctx, "cryp_new.js", nil)
	if e != nil {
		return "", e
	}
	encoded, e := mercuryEncode(secret, crypto)
	if e != nil {
		return "", e
	}
	page, e := s.request(ctx, "logon.cgi", url.Values{"username": {s.device.Username}, "password": {encoded}, "logon": {"登录"}})
	if e != nil {
		return "", e
	}
	result := regexp.MustCompile(`\bvar\s+logonInfo\s*=\s*new Array\(\s*(-?\d+)`).FindStringSubmatch(page)
	if result != nil && result[1] != "0" {
		return "", errMercuryAuth
	}
	root, e := s.request(ctx, "", nil)
	if e != nil {
		return "", e
	}
	if _, e = mercuryToken(root); e != nil || mercuryIsLogin(root) {
		return "", errMercuryAuth
	}
	s.loginErr = nil
	return root, nil
}
func (s *mercurySession) get(ctx context.Context, path string) (string, error) {
	page, e := s.request(ctx, path, nil)
	if e != nil {
		return "", e
	}
	if !mercuryIsLogin(page) {
		return page, nil
	}
	root, e := s.login(ctx)
	if e != nil {
		return "", e
	}
	if path == "" {
		return root, nil
	}
	page, e = s.request(ctx, path, nil)
	if e == nil && mercuryIsLogin(page) {
		return "", errMercuryAuth
	}
	return page, e
}
func mercuryEncode(secret, script string) (string, error) {
	// Recognize the captured algorithm, then extract its two data constants.
	// A changed encoding function must be reviewed before submitting a password.
	salt := regexp.MustCompile(`\bvar\s+b="([^"\\]{1,256})"`).FindStringSubmatch(script)
	alphabet := regexp.MustCompile(`\bvar\s+a="([^"\\]{1,512})"`).FindStringSubmatch(script)
	if salt == nil || alphabet == nil || len(secret) > 256 {
		return "", errors.New("mercury: unsupported password encoding firmware")
	}
	for _, v := range []string{salt[1], alphabet[1]} {
		for i := 0; i < len(v); i++ {
			if v[i] < 33 || v[i] > 126 {
				return "", errors.New("mercury: unsupported password encoding firmware")
			}
		}
	}
	compact := strings.Join(strings.Fields(script), "")
	compact = strings.TrimPrefix(compact, "varg_portMng=0;")
	expected := `functionhex_md5(c){varb="` + salt[1] + `";vara="` + alphabet[1] + `";returnsecurityEncode(c,b,a)}functionsecurityEncode(f,d,b){vark=b;vara="";varh,e,c,j;varl=187,i=187;e=f.length;c=d.length;j=k.length;h=e>c?e:c;for(varg=0;g<h;g++){l=187;i=187;if(g>=e){i=d.charCodeAt(g)}else{if(g>=c){l=f.charCodeAt(g)}else{l=f.charCodeAt(g);i=d.charCodeAt(g)}}a+=k.charAt((l^i)%j)}returna};`
	if compact != expected {
		return "", errors.New("mercury: unsupported password encoding firmware")
	}
	for i := 0; i < len(secret); i++ {
		if secret[i] < 32 || secret[i] > 126 {
			return "", errors.New("mercury: unsupported password encoding")
		}
	}
	n := max(len(secret), len(salt[1]))
	var b strings.Builder
	for i := 0; i < n; i++ {
		a, c := byte(187), byte(187)
		if i < len(secret) {
			a = secret[i]
		}
		if i < len(salt[1]) {
			c = salt[1][i]
		}
		b.WriteByte(alphabet[1][int(a^c)%len(alphabet[1])])
	}
	return b.String(), nil
}

func mercuryCapabilities() Capabilities {
	c := Capabilities{}
	for _, k := range []string{"identity.read", "ports.read", "statistics.read", "vlans.read", "pvids.read", "fdb.lookup"} {
		c[k] = Supported()
	}
	for _, k := range []string{"fdb.list", "neighbors.read", "radios.read", "wireless.read", "associations.read", "client.disconnect", "ports.configure", "vlans.configure", "wireless.configure", "poe.configure", "device.reboot"} {
		c[k] = Unsupported("not provided by the captured SE109 Pro read protocol")
	}
	return c
}

// Read retains successful sections when a later request or page schema fails.
// Rates are Mbps and counters are packets; the firmware supplies no byte totals.
func (m *Mercury) Read(ctx context.Context, d Device) (Snapshot, error) {
	out := Snapshot{DeviceID: d.ID, ObservedAt: time.Now(), Capabilities: mercuryCapabilities(), Collections: map[string]Collection{}}
	s, e := m.session(d)
	if e != nil {
		return out, e
	}
	successes := 0
	count := 0
	record := func(section string, e error) {
		c := Collection{State: "ok", ObservedAt: time.Now()}
		if e != nil {
			c.State = "error"
			c.Error = mercurySafeError(e)
			out.Capabilities[section+".read"] = Capability{State: "unknown", Reason: "collection failed; authentication, connectivity or firmware may differ"}
		} else {
			successes++
		}
		out.Collections[section] = c
	}
	ports := func(n int) error {
		if count != 0 && n != count {
			return errMercuryLiteral
		}
		if count == 0 {
			count = n
			out.Ports = mercuryPorts(n)
		}
		return nil
	}
	main, mainErr := s.get(ctx, "MainRpm.htm")
	if mainErr == nil {
		out.Identity, e = mercuryIdentity(main)
	} else {
		e = mainErr
	}
	record("identity", e)
	if mainErr == nil {
		var p []Port
		p, e = mercuryMainPorts(main)
		if e == nil {
			e = ports(len(p))
			if e == nil {
				out.Ports = p
			}
		}
	} else {
		e = mainErr
	}
	record("ports", e)
	stats, err := s.get(ctx, "PortStatisticsRpm.htm")
	if err == nil {
		var p []Port
		p, err = mercuryStatistics(stats)
		if err == nil {
			err = ports(len(p))
			if err == nil {
				for i := range p {
					out.Ports[i].RXPackets = p[i].RXPackets
					out.Ports[i].TXPackets = p[i].TXPackets
					out.Ports[i].RXErrors = p[i].RXErrors
					out.Ports[i].TXErrors = p[i].TXErrors
				}
			}
		}
	}
	record("statistics", err)
	vlan, err := s.get(ctx, "Vlan8021QRpm.htm")
	if err == nil {
		var n int
		var v []VLAN
		v, n, err = mercuryVLANs(vlan)
		if err == nil {
			var ds map[string]any
			ds, err = mercuryObject(vlan, "qvlan_ds")
			if err == nil {
				var enabled int
				enabled, err = mercuryCount(ds["state"], 1)
				if err == nil {
					err = ports(n)
					if err == nil {
						out.VLANs = v
						out.VLANEnabled = Ptr(enabled == 1)
					}
				}
			}
		}
	}
	record("vlans", err)
	pvid, err := s.get(ctx, "Vlan8021QPvidRpm.htm")
	if err == nil {
		var ids []int
		ids, err = mercuryPVIDs(pvid)
		if err == nil {
			err = ports(len(ids))
			if err == nil {
				for i, v := range ids {
					out.Ports[i].PVID = Ptr(v)
				}
			}
		}
	}
	record("pvids", err)
	for _, k := range []string{"fdb", "neighbors", "radios", "wireless", "associations"} {
		out.Collections[k] = Collection{State: "unsupported", ObservedAt: time.Now()}
	}
	out.ObservedAt = time.Now()
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	if successes == 0 {
		return out, errors.New("mercury: no management sections could be collected")
	}
	return out, nil
}

func mercuryMAC(s string) (string, error) {
	if len(s) == 12 {
		s = s[0:2] + ":" + s[2:4] + ":" + s[4:6] + ":" + s[6:8] + ":" + s[8:10] + ":" + s[10:12]
	}
	mac, e := net.ParseMAC(s)
	if e != nil || len(mac) != 6 {
		return "", ErrInvalid
	}
	return mac.String(), nil
}

// LookupFDB uses only the captured form's search fields. It does not enumerate
// the forwarding table, and a result describes reachability through a port.
func (m *Mercury) LookupFDB(ctx context.Context, d Device, q FDBQuery) (FDBResult, error) {
	out := FDBResult{DeviceID: d.ID, ObservedAt: time.Now(), Entries: []FDBEntry{}}
	mac, e := mercuryMAC(q.MAC)
	if e != nil || q.VLAN != nil && (*q.VLAN < 1 || *q.VLAN > 4094) {
		return out, ErrInvalid
	}
	s, e := m.session(d)
	if e != nil {
		return out, e
	}
	// The main page bounds physical port numbers; LAG identifiers are deliberately
	// not guessed from a result, as their numbering has not been verified.
	main, e := s.get(ctx, "MainRpm.htm")
	if e != nil {
		return out, e
	}
	count, e := mercuryPageCount(main, "max_port_num")
	if e != nil {
		return out, e
	}
	root, e := s.get(ctx, "")
	if e != nil {
		return out, e
	}
	token, e := mercuryToken(root)
	if e != nil {
		return out, e
	}
	query := url.Values{"txt_macAddress_search": {mac}, "txt_vid_search": {""}, "token": {token}}
	if q.VLAN != nil {
		query.Set("txt_vid_search", strconv.Itoa(*q.VLAN))
	}
	// Never retry this token-bearing URI through a new login session. A caller can
	// start a new lookup to obtain a fresh token after session expiry.
	page, e := s.request(ctx, "mac_address_search.cgi?"+query.Encode(), nil)
	if e != nil {
		return out, e
	}
	if mercuryIsLogin(page) {
		return out, errMercuryAuth
	}
	ds, e := mercuryObject(page, "mac_ds")
	if e != nil {
		return out, e
	}
	n, e := mercuryCount(ds["macNum"], 4096)
	if e != nil {
		return out, e
	}
	rows, e := mercuryArray(ds, "mac_info", n)
	if e != nil {
		return out, e
	}
	for _, v := range rows[:n] {
		row, ok := v.(map[string]any)
		if !ok {
			return out, errMercuryLiteral
		}
		raw, ok := row["mac"].(string)
		if !ok {
			return out, errMercuryLiteral
		}
		found, e := mercuryMAC(raw)
		if e != nil {
			continue
		}
		if found != mac {
			continue
		}
		p, e := mercuryCount(row["port"], count)
		if e != nil || p == 0 {
			continue
		}
		vid, e := mercuryCount(row["vid"], 4094)
		if e != nil {
			return out, e
		}
		if q.VLAN != nil && vid != *q.VLAN {
			continue
		}
		entry := FDBEntry{MAC: found, PortID: strconv.Itoa(p)}
		if vid > 0 {
			entry.VLAN = Ptr(vid)
		}
		out.Entries = append(out.Entries, entry)
	}
	out.ObservedAt = time.Now()
	return out, nil
}
