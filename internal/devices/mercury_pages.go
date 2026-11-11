package devices

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

func mercuryPageCount(page, key string) (int, error) {
	v, e := mercuryLiteral(page, key)
	if e != nil {
		return 0, e
	}
	n, e := mercuryCount(v, 64)
	if e != nil || n == 0 {
		return 0, errMercuryLiteral
	}
	return n, nil
}
func mercuryObjectCount(ds map[string]any) (int, error) {
	n, e := mercuryCount(ds["portNum"], 64)
	if e != nil || n == 0 {
		return 0, errMercuryLiteral
	}
	return n, nil
}
func mercuryPorts(n int) []Port {
	p := make([]Port, n)
	for i := range p {
		p[i] = Port{ID: strconv.Itoa(i + 1), Name: "Port " + strconv.Itoa(i+1), Kind: "other"}
	}
	return p
}
func mercuryIdentity(page string) (Identity, error) {
	ds, e := mercuryObject(page, "info_ds")
	if e != nil {
		return Identity{}, e
	}
	model, firmware := mercuryFirst(ds, "hardwareStr"), mercuryFirst(ds, "firmwareStr")
	if model != "SE109 Pro" || firmware == "" {
		return Identity{}, errMercuryLiteral
	}
	id := Identity{Manufacturer: "Mercury", Model: model, Firmware: firmware, Roles: []Role{Switch}}
	if mac, e := mercuryMAC(mercuryFirst(ds, "macStr")); e == nil {
		id.MACs = []string{mac}
	}
	re := regexp.MustCompile(`^(\d+) day - (\d+) hour - (\d+) min - (\d+) sec$`)
	if parts := re.FindStringSubmatch(mercuryFirst(ds, "workTime")); parts != nil {
		total := uint64(0)
		valid := true
		for i, mult := range []uint64{86400, 3600, 60, 1} {
			v, e := strconv.ParseUint(parts[i+1], 10, 64)
			if e != nil || v > (math.MaxUint64-total)/mult {
				valid = false
				break
			}
			total += v * mult
		}
		if valid {
			id.UptimeSeconds = Ptr(total)
		}
	}
	return id, nil
}
func mercuryMainPorts(page string) ([]Port, error) {
	n, e := mercuryPageCount(page, "max_port_num")
	if e != nil {
		return nil, e
	}
	ds, e := mercuryObject(page, "port_info")
	if e != nil {
		return nil, e
	}
	values := map[string][]any{}
	for _, key := range []string{"state", "spd_act", "rx_rate", "tx_rate"} {
		a, e := mercuryArray(ds, key, n)
		if e != nil {
			return nil, e
		}
		values[key] = a
	}
	out := mercuryPorts(n)
	media, _ := ds["port_type"].([]any)
	for i := range out {
		if i < len(media) {
			if code, err := mercuryUint(media[i]); err == nil {
				// The captured UI uses RJ45 for 0 and SFP/SC for 1, 2,
				// and 6. Combo ports 3-5 do not identify a single medium.
				switch code {
				case 0:
					out[i].Kind = "ethernet"
				case 1, 2, 6:
					out[i].Kind = "fiber"
				}
			}
		}
		state, e := mercuryCount(values["state"][i], 1)
		if e != nil {
			return nil, e
		}
		out[i].Enabled = Ptr(state == 1)
		code, e := mercuryUint(values["spd_act"][i])
		if e != nil {
			return nil, e
		}
		if state == 0 {
			out[i].LinkUp = Ptr(false)
		} else if code == 0 {
			out[i].LinkUp = Ptr(false)
		} else if code >= 2 && code <= 8 {
			out[i].LinkUp = Ptr(true)
		}
		switch code {
		case 2, 3:
			out[i].SpeedMbps = Ptr(uint64(10))
		case 4, 5:
			out[i].SpeedMbps = Ptr(uint64(100))
		case 6:
			out[i].SpeedMbps = Ptr(uint64(1000))
		case 7:
			out[i].SpeedMbps = Ptr(uint64(2500))
		case 8:
			out[i].SpeedMbps = Ptr(uint64(10000))
		}
		if code == 2 || code == 4 {
			out[i].Duplex = "half"
		} else if code >= 3 && code <= 8 {
			out[i].Duplex = "full"
		}
		rx, e := mercuryUint(values["rx_rate"][i])
		if e != nil {
			return nil, e
		}
		tx, e := mercuryUint(values["tx_rate"][i])
		if e != nil {
			return nil, e
		}
		out[i].RXMbps = Ptr(float64(rx) / 1000)
		out[i].TXMbps = Ptr(float64(tx) / 1000)
	}
	return out, nil
}
func mercuryStatistics(page string) ([]Port, error) {
	n, e := mercuryPageCount(page, "max_port_num")
	if e != nil {
		return nil, e
	}
	ds, e := mercuryObject(page, "all_info")
	if e != nil {
		return nil, e
	}
	a, e := mercuryArray(ds, "pkts", 4*n)
	if e != nil {
		return nil, e
	}
	out := mercuryPorts(n)
	for i := range out {
		v := [4]uint64{}
		for j := range v {
			v[j], e = mercuryUint(a[4*i+j])
			if e != nil {
				return nil, e
			}
		}
		out[i].TXPackets = Ptr(v[0])
		out[i].TXErrors = Ptr(v[1])
		out[i].RXPackets = Ptr(v[2])
		out[i].RXErrors = Ptr(v[3])
	}
	return out, nil
}
func mercuryVLANs(page string) ([]VLAN, int, error) {
	ds, e := mercuryObject(page, "qvlan_ds")
	if e != nil {
		return nil, 0, e
	}
	n, e := mercuryObjectCount(ds)
	if e != nil {
		return nil, 0, e
	}
	if _, e = mercuryCount(ds["state"], 1); e != nil {
		return nil, 0, e
	}
	count, e := mercuryCount(ds["count"], 4094)
	if e != nil {
		return nil, 0, e
	}
	values := map[string][]any{}
	for _, key := range []string{"vids", "names", "tagMbrs", "untagMbrs"} {
		a, e := mercuryArray(ds, key, count)
		if e != nil {
			return nil, 0, e
		}
		values[key] = a
	}
	out := make([]VLAN, count)
	seen := map[int]bool{}
	for i := range out {
		vid, e := mercuryCount(values["vids"][i], 4094)
		if e != nil || vid == 0 || seen[vid] {
			return nil, 0, errMercuryLiteral
		}
		seen[vid] = true
		name, ok := values["names"][i].(string)
		if !ok {
			return nil, 0, errMercuryLiteral
		}
		tag, e := mercuryUint(values["tagMbrs"][i])
		if e != nil {
			return nil, 0, e
		}
		untag, e := mercuryUint(values["untagMbrs"][i])
		if e != nil {
			return nil, 0, e
		}
		if n < 64 && (tag>>n != 0 || untag>>n != 0) || tag&untag != 0 {
			return nil, 0, errMercuryLiteral
		}
		out[i] = VLAN{ID: vid, Name: name, Tagged: []string{}, Untagged: []string{}}
		for p := 0; p < n; p++ {
			if tag&(uint64(1)<<p) != 0 {
				out[i].Tagged = append(out[i].Tagged, strconv.Itoa(p+1))
			}
			if untag&(uint64(1)<<p) != 0 {
				out[i].Untagged = append(out[i].Untagged, strconv.Itoa(p+1))
			}
		}
	}
	return out, n, nil
}
func mercuryPVIDs(page string) ([]int, error) {
	ds, e := mercuryObject(page, "pvid_ds")
	if e != nil {
		return nil, e
	}
	n, e := mercuryObjectCount(ds)
	if e != nil {
		return nil, e
	}
	if _, e = mercuryCount(ds["state"], 1); e != nil {
		return nil, e
	}
	a, e := mercuryArray(ds, "pvids", n)
	if e != nil {
		return nil, e
	}
	out := make([]int, n)
	for i := range out {
		out[i], e = mercuryCount(a[i], 4094)
		if e != nil || out[i] == 0 {
			return nil, errMercuryLiteral
		}
	}
	return out, nil
}

// Keep schema diagnostics constant: data and HTML may contain credentials.
func mercurySafeError(e error) string {
	if e == nil {
		return ""
	}
	if strings.HasPrefix(e.Error(), "mercury:") {
		return e.Error()
	}
	return "mercury: collection failed"
}
