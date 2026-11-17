package tools

import (
	"fmt"
	"math"
	"slices"
	"time"
)

type QualityComponent struct {
	ID              string   `json:"id"`
	Label           string   `json:"label"`
	Weight          float64  `json:"weight"`
	EffectiveWeight float64  `json:"effective_weight"`
	Score           *float64 `json:"score"`
	Reason          string   `json:"reason,omitempty"`
	// Target and Kind let a reader group the timing, stability and access
	// components of one service into the single probe that produced them.
	Target string `json:"target,omitempty"`
	Kind   string `json:"kind,omitempty"`
}

type QualityCategory struct {
	ID         string             `json:"id"`
	Label      string             `json:"label"`
	Budget     float64            `json:"budget"`
	State      string             `json:"state"`
	Score      *float64           `json:"score"`
	Earned     float64            `json:"earned_points"`
	Available  float64            `json:"available_points"`
	Components []QualityComponent `json:"components"`
}

type QualityOverall struct {
	State     string   `json:"state"`
	Score     *float64 `json:"score"`
	Earned    float64  `json:"earned_points"`
	Available float64  `json:"available_points"`
	Coverage  float64  `json:"coverage_percent"`
}

type scoreAnchor struct{ value, score float64 }

var downloadCurve = []scoreAnchor{{0, 0}, {3, 20}, {10, 40}, {25, 60}, {75, 80}, {150, 90}, {500, 100}}
var uploadCurve = []scoreAnchor{{0, 0}, {1, 20}, {3, 40}, {10, 60}, {30, 80}, {75, 90}, {200, 100}}
var tcpCurve = []scoreAnchor{{0, 100}, {10, 100}, {30, 90}, {60, 75}, {100, 60}, {200, 35}, {500, 0}}
var dnsCurve = []scoreAnchor{{0, 100}, {10, 100}, {30, 90}, {60, 75}, {100, 60}, {200, 35}, {500, 0}}
var responseCurve = []scoreAnchor{{0, 100}, {30, 100}, {100, 90}, {200, 75}, {500, 50}, {1000, 20}, {2000, 0}}
var variationCurve = []scoreAnchor{{0, 100}, {2, 95}, {5, 85}, {10, 70}, {20, 50}, {50, 20}, {100, 0}}

// -67 dBm is the usual bar for a link that carries a video call; below -75 dBm
// a link is unreliable whatever rate its radio negotiated. The curve is a
// judgement and lives here so the number can be argued with, not buried in a
// handler.
var signalCurve = []scoreAnchor{{-95, 0}, {-85, 10}, {-80, 25}, {-75, 45}, {-70, 65}, {-67, 75}, {-60, 90}, {-50, 100}}

// SNR caps a link whose signal is strong but whose noise floor is high. It can
// only lower the signal score, never raise it.
var snrCurve = []scoreAnchor{{0, 0}, {10, 20}, {15, 45}, {20, 70}, {25, 90}, {30, 100}}

// interpolate answers a curve for any value. scoreValue's guard against
// negative numbers is a guard against a missing duration, and signal strength
// is negative by nature, so the two are separate.
func interpolate(v float64, curve []scoreAnchor) float64 {
	if v <= curve[0].value {
		return curve[0].score
	}
	for i := 1; i < len(curve); i++ {
		a, b := curve[i-1], curve[i]
		if v <= b.value {
			return a.score + (b.score-a.score)*(v-a.value)/(b.value-a.value)
		}
	}
	return curve[len(curve)-1].score
}

func scoreValue(v float64, curve []scoreAnchor) *float64 {
	if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return valuePointer(interpolate(v, curve))
}

func valuePointer(v float64) *float64 { return &v }

func medianScore(samples []QualitySample, field func(QualitySample) *float64, curve []scoreAnchor) *float64 {
	var values []float64
	for _, sample := range samples {
		if v := field(sample); v != nil && *v >= 0 && !math.IsNaN(*v) && !math.IsInf(*v, 0) {
			values = append(values, *v)
		}
	}
	if len(values) < 4 {
		return nil
	}
	slices.Sort(values)
	middle := len(values) / 2
	median := values[middle]
	if len(values)%2 == 0 {
		median = (values[middle-1] + values[middle]) / 2
	}
	return scoreValue(median, curve)
}

func timingScores(samples []QualitySample) [3]*float64 {
	return [3]*float64{
		medianScore(samples, func(s QualitySample) *float64 { return s.TCP }, tcpCurve),
		medianScore(samples, func(s QualitySample) *float64 { return s.DNS }, dnsCurve),
		medianScore(samples, func(s QualitySample) *float64 { return s.Response }, responseCurve),
	}
}

func stabilityScores(samples []QualitySample) [2]*float64 {
	var out [2]*float64
	if len(samples) < qualitySamples {
		return out
	}
	connected, attempted := 0, 0
	var delta float64
	pairs := 0
	var previous *float64
	for _, s := range samples {
		if s.State == "error" || s.State == "cancelled" {
			previous = nil
			continue
		}
		attempted++
		if s.TCP != nil {
			connected++
			if previous != nil {
				delta += math.Abs(*s.TCP - *previous)
				pairs++
			}
		}
		previous = s.TCP
	}
	if attempted == qualitySamples {
		out[0] = valuePointer(100 * float64(connected) / float64(attempted))
	}
	if pairs >= 3 {
		out[1] = scoreValue(delta/float64(pairs), variationCurve)
	}
	return out
}

// Alternatives choose one whole family result, not independently selected
// timing fields from incompatible paths. Compare normalized measured scores.
func bestFamily(results []QualityTargetResult, id string, stability bool) []QualitySample {
	var best []QualitySample
	bestScore := -1.0
	bestWeight := -1.0
	for _, r := range results {
		if r.ID != id {
			continue
		}
		var scores []*float64
		var weights []float64
		if stability {
			v := stabilityScores(r.Samples)
			scores = v[:]
			weights = []float64{.4, .3}
		} else {
			v := timingScores(r.Samples)
			scores = v[:]
			weights = []float64{.6, .15, .25}
		}
		total := 0.0
		weight := 0.0
		for i, s := range scores {
			if s != nil {
				total += *s * weights[i]
				weight += weights[i]
			}
		}
		if weight > 0 {
			total /= weight
		} else {
			total = -1
		}
		if total > bestScore || (total == bestScore && weight > bestWeight) {
			bestScore = total
			bestWeight = weight
			best = r.Samples
		}
	}
	return best
}

func accessScore(results []QualityTargetResult, id string) *float64 {
	knownFailure := false
	unknown := false
	families := make(map[string]bool)
	for _, r := range results {
		if r.ID != id {
			continue
		}
		families[r.Family] = true
		if len(r.Samples) != qualitySamples {
			unknown = true
		}
		for _, s := range r.Samples {
			if s.State == "ok" {
				return valuePointer(100)
			}
			if s.State == "failed" {
				knownFailure = true
			} else {
				unknown = true
			}
		}
	}
	if knownFailure && !unknown && families["ipv4"] && families["ipv6"] {
		return valuePointer(0)
	}
	return nil
}

// wirelessScore scores each observed link on its own signal and SNR, then
// averages them. A mean rather than a median: one device stuck in a dead corner
// is exactly the case a median would hide, and a wireless score that ignores the
// worst link would be describing the best one. The negotiated rate stays out of
// the score — it is what the radio agreed to, not what the air carries — but it
// is kept as evidence beside the link.
func wirelessScore(w *QualityWireless) (*float64, string) {
	if w == nil || len(w.APs) == 0 {
		return nil, "No access point is configured for measurement"
	}
	answered := 0
	for _, ap := range w.APs {
		if ap.State == "ok" {
			answered++
		}
	}
	total, links := 0.0, 0
	for _, link := range w.Links {
		if link.SignalDBm == nil {
			continue
		}
		value := interpolate(float64(*link.SignalDBm), signalCurve)
		if link.SNRDb != nil {
			if snr := interpolate(float64(*link.SNRDb), snrCurve); snr < value {
				value = snr
			}
		}
		total += value
		links++
	}
	if links == 0 {
		return nil, "No associated client reported a signal level"
	}
	reason := fmt.Sprintf("Mean of %d links scored on signal and SNR, observed on %d of %d access points; only associated clients, no throughput measurement",
		links, answered, len(w.APs))
	return valuePointer(total / float64(links)), reason
}

func category(id, label string, budget float64, components ...QualityComponent) QualityCategory {
	c := QualityCategory{ID: id, Label: label, Budget: budget, State: "unavailable", Components: components}
	weighted, weight := 0.0, 0.0
	for _, item := range components {
		if item.Score == nil {
			continue
		}
		weighted += *item.Score * item.Weight
		weight += item.Weight
	}
	if weight > 0 {
		c.Score = valuePointer(weighted / weight)
		for i := range c.Components {
			if c.Components[i].Score != nil {
				c.Components[i].EffectiveWeight = c.Components[i].Weight / weight
			}
		}
		c.State = "partial"
		if weight >= 1-1e-9 {
			c.State = "ready"
		}
	}
	return c
}

func scoreQuality(s QualitySnapshot, now time.Time) ([]QualityCategory, QualityOverall) {
	reference := QualityComponent{ID: "reference", Label: "Cloudflare download and upload", Weight: .4, Reason: "Bandwidth test not run", Kind: "reference"}
	if s.Speed != nil {
		d, u := scoreValue(s.Speed.Download, downloadCurve), scoreValue(s.Speed.Upload, uploadCurve)
		if d != nil && u != nil {
			reference.Score = valuePointer(*d*.7 + *u*.3)
			reference.Reason = "Cloudflare edge throughput; 70% download and 30% upload; other service transfers not measured"
		}
	}
	if s.SpeedError != "" {
		reference.Reason = s.SpeedError
	}
	categories := []QualityCategory{category("speed", "Speed", 25, reference, QualityComponent{ID: "services", Label: "Service-specific transfers", Weight: .6, Reason: "Not implemented", Kind: "services"})}
	latency, stability, functions := []QualityComponent{}, []QualityComponent{}, []QualityComponent{}
	for _, target := range qualityTargets {
		timing := timingScores(bestFamily(s.Targets, target.ID, false))
		for i, item := range []struct {
			id, label string
			weight    float64
		}{{"tcp", "TCP connection", .6}, {"dns", "DNS lookup", .15}, {"response", "HTTPS response", .25}} {
			latency = append(latency, QualityComponent{ID: target.ID + "-" + item.id, Label: target.Name + " · " + item.label, Weight: item.weight / float64(len(qualityTargets)), Score: timing[i], Reason: "Median of at least four completed timings; best measured IP family", Target: target.ID, Kind: item.id})
		}
		consistency := stabilityScores(bestFamily(s.Targets, target.ID, true))
		stability = append(stability,
			QualityComponent{ID: target.ID + "-continuity", Label: target.Name + " · TCP connection success", Weight: .4 / float64(len(qualityTargets)), Score: consistency[0], Reason: "Six connection attempts; not packet-loss measurement", Target: target.ID, Kind: "continuity"},
			QualityComponent{ID: target.ID + "-variation", Label: target.Name + " · TCP timing variation", Weight: .3 / float64(len(qualityTargets)), Score: consistency[1], Reason: "Mean absolute difference between consecutive completed TCP timings", Target: target.ID, Kind: "variation"})
		functions = append(functions, QualityComponent{ID: target.ID + "-access", Label: target.Name + " · Test resource access", Weight: .5 / float64(len(qualityTargets)), Score: accessScore(s.Targets, target.ID), Reason: "IPv4 or IPv6; expected resource response, not full service functionality", Target: target.ID, Kind: "access"})
	}
	stability = append(stability, QualityComponent{ID: "load", Label: "Consistency under load", Weight: .3, Reason: "Not implemented", Kind: "load"})
	functions = append(functions, QualityComponent{ID: "protocols", Label: "Protocol capabilities", Weight: .25, Reason: "Not implemented", Kind: "protocols"}, QualityComponent{ID: "inbound-p2p", Label: "Inbound and P2P", Weight: .25, Reason: "Not implemented; STUN observations alone do not verify this capability", Kind: "inbound-p2p"})
	categories = append(categories, category("latency", "Latency", 25, latency...), category("stability", "Stability", 25, stability...))
	// Wireless evidence comes from the access points' own client tables, so an
	// unreachable access point is an error beside the score, never a zero.
	wifiScore, wifiReason := wirelessScore(s.Wireless)
	if s.WirelessError != "" {
		wifiReason = s.WirelessError
	}
	categories = append(categories, category("wifi", "Wi-Fi", 15, QualityComponent{ID: "client-link", Label: "Measured wireless links", Weight: 1, Score: wifiScore, Reason: wifiReason, Kind: "client-link"}))
	categories = append(categories, category("functionality", "Functionality", 10, functions...))
	return aggregateQuality(categories, s, now)
}

func aggregateQuality(categories []QualityCategory, s QualitySnapshot, now time.Time) ([]QualityCategory, QualityOverall) {
	o := QualityOverall{State: "unavailable"}
	budget := 0.0
	for i := range categories {
		c := &categories[i]
		c.Earned, c.Available = 0, 0
		if c.Score == nil {
			continue
		}
		budget += c.Budget
		for _, component := range c.Components {
			if component.Score != nil {
				o.Coverage += c.Budget * component.Weight
			}
		}
	}
	if budget > 0 {
		for i := range categories {
			c := &categories[i]
			if c.Score == nil {
				continue
			}
			c.Available = 100 * c.Budget / budget
			c.Earned = *c.Score * c.Available / 100
			o.Earned += c.Earned
		}
		o.Available = 100
		o.State = "ready"
		o.Score = valuePointer(o.Earned)
	}
	if s.State == "running" {
		o.State = "measuring"
		o.Score = nil
	}
	if s.State == "cancelled" || s.State == "timed_out" {
		o.State = "partial"
		o.Score = nil
	}
	return categories, o
}
