package gateway

import (
	"net/http"
	"strings"
	"testing"
)

// The item routes exist so that the *edit* happens on this side of the socket.
// What follows is mostly about the rules that come with an edit — the rename
// cascade, the preserved slot, the refusal to tidy up a dangling reference —
// because those are exactly what a client splicing the list itself does not get.
//
// Helpers (newTestHandler, do, decode, testConfig, hop) live in http_test.go and
// config_test.go.

func TestDeviceOverrideItemRoutes(t *testing.T) {
	h, a := newTestHandler(t, &StaticKernel{})
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatal(w.Body)
	}
	path := "/clients/AA-BB-CC-DD-EE-FF"
	if w := do(t, h, http.MethodPut, path, assignmentBody{Exit: ""}); w.Code != http.StatusOK {
		t.Fatalf("direct override: %d %s", w.Code, w.Body)
	}
	c, _ := a.Load()
	if len(c.Clients) != 1 || c.Clients[0].MAC != "aa:bb:cc:dd:ee:ff" || c.Clients[0].Exit != "" {
		t.Fatalf("stored direct override: %+v", c.Clients)
	}
	if w := do(t, h, http.MethodPut, path, assignmentBody{Exit: "Proxy"}); w.Code != http.StatusOK {
		t.Fatalf("exit override: %d %s", w.Code, w.Body)
	}
	if w := do(t, h, http.MethodDelete, path, nil); w.Code != http.StatusOK {
		t.Fatalf("inherit: %d %s", w.Code, w.Body)
	}
	c, _ = a.Load()
	if len(c.Clients) != 0 {
		t.Fatalf("device still overridden: %+v", c.Clients)
	}
}

func TestPutExitAddsOneWithoutTouchingTheOthers(t *testing.T) {
	h, a := newTestHandler(t, &StaticKernel{})
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	office := Exit{Name: "Office", Via: Via{Kind: ViaInterface, Interface: "wg0"}}
	if w := do(t, h, http.MethodPut, "/exits/Office", office); w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}

	cfg, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Find("Office"); !ok {
		t.Error("Office was not added")
	}
	if _, ok := cfg.Find("Proxy"); !ok {
		t.Error("adding one exit dropped another")
	}
}

// TestPutExitRenamesEveryReferenceWithIt is the reason this route exists.
//
// An exit's name is referenced by the box-wide default and by every assignment,
// so a rename is a change in three places at once. Config.Rename does all three
// together; until this route it had no caller outside a test, and a client that
// spliced the exits array itself changed only the first — leaving the other two
// naming an exit that was no longer there.
func TestPutExitRenamesEveryReferenceWithIt(t *testing.T) {
	h, a := newTestHandler(t, &StaticKernel{})

	cfg := testConfig()
	cfg.Default = "Proxy"
	if w := do(t, h, http.MethodPut, "/config", cfg); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	renamed := Exit{Name: "Relay", Via: Via{Kind: ViaNextHop, NextHop: hop("192.168.1.50")}}
	if w := do(t, h, http.MethodPut, "/exits/Proxy", renamed); w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}

	stored, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := stored.Find("Proxy"); ok {
		t.Error("the old name is still in the exit list")
	}
	if _, ok := stored.Find("Relay"); !ok {
		t.Error("the new name is not in the exit list")
	}
	if stored.Default != "Relay" {
		t.Errorf("the box-wide default still says %q", stored.Default)
	}
	if got, _ := stored.Assigned("br-lan"); got != "Relay" {
		t.Errorf("br-lan still routes via %q", got)
	}
}

func TestPutExitRefusesToRenameOntoAnExistingName(t *testing.T) {
	h, _ := newTestHandler(t, &StaticKernel{})
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	onto := Exit{Name: "Blocked", Via: Via{Kind: ViaBlocked}}
	if w := do(t, h, http.MethodPut, "/exits/Proxy", onto); w.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
}

// TestPutExitKeepsTheSlot guards the other rule an edit carries. Every kernel
// resource this module owns is computed from the slot, so reallocating one on
// edit would move the exit's route table out from under every flow that is
// already marked for it.
func TestPutExitKeepsTheSlot(t *testing.T) {
	h, a := newTestHandler(t, &StaticKernel{})
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}
	before, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	was, _ := before.Find("Proxy")

	// A different next hop, and no slot in the body: a caller does not know
	// about slots and must not have to.
	edited := Exit{Name: "Proxy", Via: Via{Kind: ViaNextHop, NextHop: hop("192.168.1.60")}}
	if w := do(t, h, http.MethodPut, "/exits/Proxy", edited); w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}

	after, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	now, _ := after.Find("Proxy")
	if now.Slot != was.Slot {
		t.Fatalf("slot moved from %d to %d", was.Slot, now.Slot)
	}
}

// TestDeleteExitStillInUseIsRefused: Config.Remove deliberately leaves the
// dangling reference for Validate to report, rather than silently re-pointing
// somebody's devices at the modem because an exit was deleted in another window.
func TestDeleteExitStillInUseIsRefused(t *testing.T) {
	h, a := newTestHandler(t, &StaticKernel{})
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	w := do(t, h, http.MethodDelete, "/exits/Proxy", nil)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "Proxy") {
		t.Errorf("the refusal should name the exit: %s", w.Body)
	}

	cfg, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Find("Proxy"); !ok {
		t.Error("a refused delete removed it anyway")
	}
}

func TestDeleteExitNothingUsesSucceeds(t *testing.T) {
	h, a := newTestHandler(t, &StaticKernel{})
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}
	if w := do(t, h, http.MethodDelete, "/assignments/br-iot", nil); w.Code != http.StatusOK {
		t.Fatalf("releasing the assignment failed: %d %s", w.Code, w.Body)
	}

	if w := do(t, h, http.MethodDelete, "/exits/Blocked", nil); w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}

	cfg, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Find("Blocked"); ok {
		t.Error("it is still there")
	}
}

func TestDeletingSomethingThatIsNotThereIsANotFound(t *testing.T) {
	h, _ := newTestHandler(t, &StaticKernel{})
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	if w := do(t, h, http.MethodDelete, "/exits/Nope", nil); w.Code != http.StatusNotFound {
		t.Errorf("unknown exit: status %d: %s", w.Code, w.Body)
	}
	if w := do(t, h, http.MethodDelete, "/assignments/br-nope", nil); w.Code != http.StatusNotFound {
		t.Errorf("unassigned network: status %d: %s", w.Code, w.Body)
	}
}

func TestPutAssignmentPointsOneNetworkAtAnExit(t *testing.T) {
	h, a := newTestHandler(t, &StaticKernel{})
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	w := do(t, h, http.MethodPut, "/assignments/br-iot", assignmentBody{Exit: "Proxy"})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}

	cfg, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := cfg.Assigned("br-iot"); got != "Proxy" {
		t.Fatalf("br-iot routes via %q", got)
	}
}

// Naming an exit that does not exist is Validate's answer to give, and it gives
// it with the list of exits that do. No client repeats that check.
func TestPutAssignmentNamingNoExitIsRefused(t *testing.T) {
	h, _ := newTestHandler(t, &StaticKernel{})
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	w := do(t, h, http.MethodPut, "/assignments/br-iot", assignmentBody{Exit: "Ghost"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "Ghost") {
		t.Errorf("the refusal should name what was asked for: %s", w.Body)
	}
}

// --- dry run and the confirm gate ------------------------------------------

func TestDryRunOnAnItemRouteWritesNothing(t *testing.T) {
	h, a := newTestHandler(t, &StaticKernel{})
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	office := Exit{Name: "Office", Via: Via{Kind: ViaInterface, Interface: "wg0"}}
	w := do(t, h, http.MethodPut, "/exits/Office?dry_run=true", office)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	// The same shape POST /plan gives, so one question has one answer whichever
	// route it was asked down.
	if plan := decode[planView](t, w); !plan.Known {
		t.Error("the plan should say the kernel was readable")
	}

	cfg, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Find("Office"); ok {
		t.Fatal("a dry run wrote the change")
	}
}

// A caller that meant to confirm a disruptive change and mistyped is much better
// off hearing about it than having the change quietly held — or quietly made.
func TestAMalformedFlagIsRefusedRatherThanIgnored(t *testing.T) {
	h, _ := newTestHandler(t, &StaticKernel{})
	w := do(t, h, http.MethodDelete, "/exits/Proxy?confirm=yes%20please", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
}

// TestADisruptiveChangeIsHeldUntilConfirmed is §5.3.3's one interruption: the
// operator is about to move traffic that is flowing, and that is the single case
// worth a second round trip. Everything else applies on the first.
func TestADisruptiveChangeIsHeldUntilConfirmed(t *testing.T) {
	k := &StaticKernel{}
	h, a := newTestHandler(t, k)
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	// Somebody on br-lan is now using the path that is about to move.
	k.Active = []string{"192.168.1.20"}

	move := assignmentBody{Exit: "Blocked"}
	w := do(t, h, http.MethodPut, "/assignments/br-lan", move)
	if w.Code != http.StatusConflict {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	held := decode[applyResponse](t, w)
	if held.Plan.Impact != ImpactDisruptive {
		t.Errorf("impact %q", held.Plan.Impact)
	}
	// 409 means two things on this module and a client tells them apart by the
	// body, so the body has to be unambiguous.
	if held.Plan.Blocked != "" {
		t.Errorf("this is the confirm gate, not the foreign-rule refusal: %q", held.Plan.Blocked)
	}

	cfg, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := cfg.Assigned("br-lan"); got != "Proxy" {
		t.Fatalf("a held change was applied anyway: br-lan routes via %q", got)
	}

	// The same request, confirmed.
	if w := do(t, h, http.MethodPut, "/assignments/br-lan?confirm=true", move); w.Code != http.StatusOK {
		t.Fatalf("confirmed: status %d: %s", w.Code, w.Body)
	}
	cfg, err = a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := cfg.Assigned("br-lan"); got != "Blocked" {
		t.Fatalf("br-lan routes via %q", got)
	}
}

// The repair path is not a change, so the gate does not apply to it. Requiring
// confirmation would mean a box whose rules somebody flushed needs an extra flag
// to put them back, and that is the box that most needs putting back.
func TestApplyIsNotHeldByTheConfirmGate(t *testing.T) {
	k := &StaticKernel{}
	h, _ := newTestHandler(t, k)
	if w := do(t, h, http.MethodPut, "/config", testConfig()); w.Code != http.StatusOK {
		t.Fatalf("setup failed: %s", w.Body)
	}

	k.Active = []string{"192.168.1.20"}
	k.State = nil // somebody flushed our rules by hand

	if w := do(t, h, http.MethodPost, "/apply", nil); w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if len(k.State) == 0 {
		t.Fatal("re-applying should have put it back")
	}
}
