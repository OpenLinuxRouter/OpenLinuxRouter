package network

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

type HTTP struct {
	Service *Service
	Lock    *core.Lock
	Events  *core.Events
}
type ExecuteRequest struct {
	Operation    Operation `json:"operation"`
	Confirmation string    `json:"confirmation"`
}

func (h HTTP) Routes() []core.Route {
	return []core.Route{
		{Method: "GET", Path: "/config", Tool: "show config", Summary: "Show configured network equipment and credential references, without connecting to devices.", Handler: h.getConfig},
		{Method: "PUT", Path: "/config", Body: core.BodyFull, Mutating: true, Summary: "Replace network equipment inventory without modifying remote equipment.", Handler: h.putConfig},
		{Method: "PATCH", Path: "/config", Body: core.BodyRelaxed, Mutating: true, Summary: "Update network equipment inventory without modifying remote equipment.", Handler: h.patchConfig},
		{Method: "PUT", Path: "/devices/{id}", Mutating: true, Summary: "Add or replace one managed network device without modifying remote equipment.", Handler: h.putDevice},
		{Method: "DELETE", Path: "/devices/{id}", Mutating: true, Summary: "Forget managed network equipment without modifying it.", Handler: h.deleteDevice},
		{Method: "GET", Path: "/contract", Tool: "show contract", Summary: "Show versioned network equipment data and operation schemas.", Handler: h.getContract},
		{Method: "GET", Path: "/devices/{id}/snapshot", Tool: "show snapshot", Summary: "Read one network device with per-section collection status and capabilities.", Handler: h.getSnapshot},
		{Method: "GET", Path: "/devices/{id}/capabilities", Tool: "show capabilities", Summary: "Inspect current read and operation capabilities of one network device.", Handler: h.getCapabilities},
		// Deliberately not a published tool: a scan moves the radio off its
		// channel, so an agent must not start one while reading.
		{Method: "POST", Path: "/devices/{id}/scan", Summary: "Ask one radio which neighbouring BSS it can hear; the scan interrupts that radio for a few seconds.", Handler: h.scan},
		{Method: "GET", Path: "/devices/{id}/fdb", Tool: "show fdb", Summary: "Look up a known MAC and optional VLAN on a network device without claiming physical adjacency.", Query: []core.QueryParam{{Name: "mac", Type: "string", Required: true, Summary: "MAC address to look up."}, {Name: "vlan", Type: "integer", Summary: "Optional VLAN ID between 1 and 4094."}}, Handler: h.getFDB},
		{Method: "POST", Path: "/devices/{id}/operations/plan", Tool: "show operation", Summary: "Preview a typed device operation and issue an expiring one-use confirmation without executing it.", Query: []core.QueryParam{{Name: "kind", Type: "string", Required: true, Summary: "Operation kind: client.disconnect."}, {Name: "target", Type: "string", Required: true, Summary: "Observed wireless network interface ID."}, {Name: "mac", Type: "string", Required: true, Summary: "Wireless client MAC address."}}, Handler: h.planOperation},
		{Method: "POST", Path: "/devices/{id}/operations/execute", Mutating: true, Summary: "Execute a previewed device operation once using its confirmation.", Handler: h.executeOperation},
	}
}
func (h HTTP) getConfig(w http.ResponseWriter, r *http.Request) {
	c, err := Load(h.Service.Store)
	if err != nil {
		writeError(w, err)
		return
	}
	core.WriteConfig(w, c, c.Redacted())
}
func (h HTTP) mutate(w http.ResponseWriter, r *http.Request, edit func(*Config) error) {
	var stored Config
	stale := false
	err := h.Lock.Do(r.Context(), func() error {
		c, err := Load(h.Service.Store)
		if err != nil {
			return err
		}
		if !core.MatchConfig(w, r, c) {
			stale = true
			return nil
		}
		previous := Config{Devices: append([]Device(nil), c.Devices...)}
		if err = edit(&c); err != nil {
			return err
		}
		c = restoreSecrets(c, previous)
		stored, err = Save(h.Service.Store, c)
		return err
	})
	if stale {
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	if h.Events != nil {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}
	core.WriteConfig(w, stored, stored.Redacted())
}
func (h HTTP) putConfig(w http.ResponseWriter, r *http.Request) {
	data, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, 400, err.Error())
		return
	}
	c, err := UnmarshalConfig(data)
	if err != nil {
		core.WriteError(w, 400, err.Error())
		return
	}
	h.mutate(w, r, func(current *Config) error { *current = c; return nil })
}
func (h HTTP) patchConfig(w http.ResponseWriter, r *http.Request) {
	patch, err := core.ReadBody(w, r)
	if err != nil {
		core.WriteError(w, 400, err.Error())
		return
	}
	h.mutate(w, r, func(c *Config) error {
		next, err := core.PatchConfig(*c, patch, UnmarshalConfig)
		if err != nil {
			return errors.Join(ErrInvalid, err)
		}
		*c = next
		return nil
	})
}
func (h HTTP) putDevice(w http.ResponseWriter, r *http.Request) {
	var d Device
	if err := core.DecodeJSON(w, r, &d); err != nil {
		core.WriteError(w, 400, err.Error())
		return
	}
	id := r.PathValue("id")
	if d.ID != "" && d.ID != id {
		core.WriteError(w, 400, "body ID must match URL")
		return
	}
	d.ID = id
	h.mutate(w, r, func(c *Config) error {
		for i := range c.Devices {
			if c.Devices[i].ID == id {
				c.Devices[i] = d
				return nil
			}
		}
		c.Devices = append(c.Devices, d)
		return nil
	})
}
func (h HTTP) deleteDevice(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(c *Config) error {
		for i, d := range c.Devices {
			if d.ID == r.PathValue("id") {
				c.Devices = append(c.Devices[:i], c.Devices[i+1:]...)
				return nil
			}
		}
		return ErrNotFound
	})
}
func (h HTTP) getSnapshot(w http.ResponseWriter, r *http.Request) {
	s, err := h.Service.Read(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	core.WriteJSON(w, 200, s)
}
func (h HTTP) getCapabilities(w http.ResponseWriter, r *http.Request) {
	s, err := h.Service.Read(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	core.WriteJSON(w, 200, struct {
		DeviceID     string       `json:"device_id"`
		Capabilities Capabilities `json:"capabilities"`
	}{s.DeviceID, s.Capabilities})
}
func (h HTTP) scan(w http.ResponseWriter, r *http.Request) {
	var request ScanRequest
	if err := core.DecodeJSON(w, r, &request); err != nil {
		core.WriteError(w, 400, err.Error())
		return
	}
	result, err := h.Service.Scan(r.Context(), r.PathValue("id"), request.RadioID)
	if err != nil {
		// writeError's 504 text speaks of an executed operation; a scan never
		// changes anything and should not borrow that warning.
		if errors.Is(err, context.DeadlineExceeded) {
			core.WriteError(w, 504, "The radio did not finish scanning before the timeout. Nothing was changed; try again later.")
			return
		}
		writeError(w, err)
		return
	}
	core.WriteJSON(w, 200, result)
}
func (h HTTP) getFDB(w http.ResponseWriter, r *http.Request) {
	q := FDBQuery{MAC: r.URL.Query().Get("mac")}
	if value := r.URL.Query().Get("vlan"); value != "" {
		v, err := strconv.Atoi(value)
		if err != nil {
			core.WriteError(w, 400, "invalid VLAN ID")
			return
		}
		q.VLAN = &v
	}
	result, err := h.Service.Lookup(r.Context(), r.PathValue("id"), q)
	if err != nil {
		writeError(w, err)
		return
	}
	core.WriteJSON(w, 200, result)
}
func (h HTTP) planOperation(w http.ResponseWriter, r *http.Request) {
	var op Operation
	if len(r.URL.Query()) > 0 {
		if r.ContentLength != 0 {
			core.WriteError(w, 400, "provide operation parameters in either query or JSON body")
			return
		}
		op = Operation{Kind: r.URL.Query().Get("kind"), Target: r.URL.Query().Get("target"), ClientMAC: r.URL.Query().Get("mac")}
	} else if err := core.DecodeJSON(w, r, &op); err != nil {
		core.WriteError(w, 400, err.Error())
		return
	}
	plan, err := h.Service.Plan(r.Context(), r.PathValue("id"), op)
	if err != nil {
		writeError(w, err)
		return
	}
	core.WriteJSON(w, 200, plan)
}
func (h HTTP) executeOperation(w http.ResponseWriter, r *http.Request) {
	var request ExecuteRequest
	if err := core.DecodeJSON(w, r, &request); err != nil {
		core.WriteError(w, 400, err.Error())
		return
	}
	var result OperationResult
	err := h.Lock.Do(r.Context(), func() error {
		var err error
		result, err = h.Service.Execute(r.Context(), r.PathValue("id"), request.Operation, request.Confirmation)
		return err
	})
	if err != nil {
		if result.DeviceID != "" && !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrUnsupported) {
			core.WriteJSON(w, http.StatusBadGateway, struct {
				Result OperationResult `json:"result"`
				Error  core.ErrorBody  `json:"error"`
			}{result, core.ErrorBody{Message: "Device operation failed; outcome may be unknown. Confirmation was consumed; re-read device state before making a new decision."}})
			return
		}
		writeError(w, err)
		return
	}
	if h.Events != nil {
		h.Events.Publish(core.Event{Type: core.EventApplied, Module: ModuleName})
	}
	core.WriteJSON(w, 200, result)
}
func (h HTTP) getContract(w http.ResponseWriter, r *http.Request) {
	schemas := map[string]any{}
	for name, value := range map[string]any{"device": Device{}, "snapshot": Snapshot{}, "scan_request": ScanRequest{}, "scan_result": ScanResult{}, "fdb_query": FDBQuery{}, "fdb_result": FDBResult{}, "operation": Operation{}, "operation_plan": OperationPlan{}, "execute_request": ExecuteRequest{}, "operation_result": OperationResult{}} {
		p, err := core.Reflect("network_"+name, value)
		if err != nil {
			writeError(w, err)
			return
		}
		schemas[name] = p.Full
	}
	core.WriteJSON(w, 200, map[string]any{"version": 1, "schemas": schemas})
}
func writeError(w http.ResponseWriter, err error) {
	code := http.StatusBadGateway
	message := "Network device request failed; check connectivity, credentials, permissions and firmware."
	switch {
	case errors.Is(err, ErrNotFound):
		code = 404
		message = ErrNotFound.Error()
	case errors.Is(err, ErrInvalid):
		code = 400
		message = err.Error()
	case errors.Is(err, ErrUnsupported):
		code = 422
		message = ErrUnsupported.Error()
	case errors.Is(err, ErrConfirmation):
		code = 409
		message = ErrConfirmation.Error()
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		code = 504
		message = "Network device request timed out; an executed operation may have an unknown outcome and will not be replayed."
	}
	core.WriteError(w, code, message)
}
