package realtime

import (
	"encoding/json"
	"testing"
	"time"
)

const officeTestWorkspace = "ws-office"

type officeFrame struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func newOfficeTestClient(hub *Hub, userID string) *Client {
	c := &Client{
		hub:           hub,
		send:          make(chan []byte, 32),
		userID:        userID,
		workspaceID:   officeTestWorkspace,
		subscriptions: make(map[scopeKey]bool),
	}
	hub.registerClient(c)
	return c
}

func mustOfficeFrame(t *testing.T, c *Client) officeFrame {
	t.Helper()
	select {
	case raw := <-c.send:
		var frame officeFrame
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatalf("unmarshal office frame %q: %v", raw, err)
		}
		return frame
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for an office frame on %s", c.userID)
		return officeFrame{}
	}
}

func expectNoOfficeFrame(t *testing.T, c *Client) {
	t.Helper()
	select {
	case raw := <-c.send:
		t.Fatalf("unexpected office frame on %s: %s", c.userID, raw)
	case <-time.After(50 * time.Millisecond):
	}
}

func mustOfficeMove(t *testing.T, frame officeFrame, wantType string) officeUser {
	t.Helper()
	if frame.Type != wantType {
		t.Fatalf("frame type = %q, want %q", frame.Type, wantType)
	}
	var user officeUser
	if err := json.Unmarshal(frame.Payload, &user); err != nil {
		t.Fatalf("unmarshal move payload: %v", err)
	}
	return user
}

func officeJoinPayload(t *testing.T, x, y float64, dir string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(officeMovePayload{X: x, Y: y, Dir: dir})
	if err != nil {
		t.Fatalf("marshal join payload: %v", err)
	}
	return raw
}

// Joining sends the joiner the current room and tells existing members to
// spawn the new avatar. The snapshot carries positions, not connection counts.
func TestOfficeJoinSendsSnapshotAndNotifiesPeers(t *testing.T) {
	hub := NewHub()
	alice := newOfficeTestClient(hub, "alice")
	bob := newOfficeTestClient(hub, "bob")

	alice.handleOfficeJoin(officeJoinPayload(t, 100, 120, "down"))

	joined := mustOfficeMove(t, mustOfficeFrame(t, alice), "office_peer_join")
	if joined.UserID != "alice" || joined.X != 100 || joined.Y != 120 || joined.Dir != "down" {
		t.Fatalf("unexpected peer_join payload: %+v", joined)
	}
	state := mustOfficeFrame(t, alice)
	if state.Type != "office_state" {
		t.Fatalf("frame type = %q, want office_state", state.Type)
	}
	var snapshot struct {
		Members []officeUser `json:"members"`
	}
	if err := json.Unmarshal(state.Payload, &snapshot); err != nil {
		t.Fatalf("unmarshal office_state: %v", err)
	}
	if len(snapshot.Members) != 1 || snapshot.Members[0].UserID != "alice" {
		t.Fatalf("unexpected snapshot: %+v", snapshot.Members)
	}

	bob.handleOfficeJoin(officeJoinPayload(t, 200, 200, "left"))
	mustOfficeFrame(t, bob) // peer_join for bob
	bobState := mustOfficeFrame(t, bob)
	if bobState.Type != "office_state" {
		t.Fatalf("frame type = %q, want office_state", bobState.Type)
	}
	if err := json.Unmarshal(bobState.Payload, &snapshot); err != nil {
		t.Fatalf("unmarshal office_state: %v", err)
	}
	if len(snapshot.Members) != 2 {
		t.Fatalf("snapshot members = %d, want 2", len(snapshot.Members))
	}

	if got := mustOfficeMove(t, mustOfficeFrame(t, alice), "office_peer_join"); got.UserID != "bob" {
		t.Fatalf("alice saw peer_join for %q, want bob", got.UserID)
	}
}

func TestOfficeJoinRejectsInvalidPosition(t *testing.T) {
	hub := NewHub()
	alice := newOfficeTestClient(hub, "alice")

	alice.handleOfficeJoin(officeJoinPayload(t, -5, 120, "down"))

	frame := mustOfficeFrame(t, alice)
	if frame.Type != "office_error" {
		t.Fatalf("frame type = %q, want office_error", frame.Type)
	}
	if alice.officeJoined {
		t.Fatal("invalid join must not register the connection")
	}
	if members := hub.office.snapshot(officeTestWorkspace); len(members) != 0 {
		t.Fatalf("room should stay empty, got %+v", members)
	}
}

func TestOfficeMoveBroadcastsPositionAndRejectsInvalidFrames(t *testing.T) {
	hub := NewHub()
	alice := newOfficeTestClient(hub, "alice")
	bob := newOfficeTestClient(hub, "bob")
	alice.handleOfficeJoin(officeJoinPayload(t, 100, 120, "down"))
	bob.handleOfficeJoin(officeJoinPayload(t, 200, 200, "left"))
	// Drain join frames.
	for range 3 {
		mustOfficeFrame(t, alice)
	}
	mustOfficeFrame(t, bob)
	mustOfficeFrame(t, bob)

	alice.handleOfficeMove(officeJoinPayload(t, 240, 96, "right"))
	moved := mustOfficeMove(t, mustOfficeFrame(t, bob), "office_peer_move")
	if moved.UserID != "alice" || moved.X != 240 || moved.Y != 96 || moved.Dir != "right" {
		t.Fatalf("unexpected move payload: %+v", moved)
	}

	alice.handleOfficeMove(officeJoinPayload(t, officeWorldWidth+1, 96, "right"))
	alice.handleOfficeMove(officeJoinPayload(t, 96, -1, "right"))
	alice.handleOfficeMove(officeJoinPayload(t, 96, 96, "sideways"))
	expectNoOfficeFrame(t, bob)

	state := hub.office.snapshot(officeTestWorkspace)
	for _, member := range state {
		if member.UserID == "alice" && (member.X != 240 || member.Y != 96) {
			t.Fatalf("invalid moves must not be stored, alice at %+v", member)
		}
	}
}

func TestOfficeMoveWithoutJoinIsIgnored(t *testing.T) {
	hub := NewHub()
	alice := newOfficeTestClient(hub, "alice")
	bob := newOfficeTestClient(hub, "bob")
	bob.handleOfficeJoin(officeJoinPayload(t, 200, 200, "left"))
	mustOfficeFrame(t, bob) // peer_join for bob
	mustOfficeFrame(t, bob) // bob's snapshot

	alice.handleOfficeMove(officeJoinPayload(t, 10, 10, "up"))

	expectNoOfficeFrame(t, bob)
	if alice.officeJoined {
		t.Fatal("a move must not implicitly join the office")
	}
}

func TestOfficeMoveRateLimit(t *testing.T) {
	alice := &Client{}

	accepted := 0
	for range officeMoveBurst * 2 {
		if alice.allowOfficeMove() {
			accepted++
		}
	}
	// A little slack covers the token refill that elapses between calls; a
	// broken limiter would accept every one of the 2x burst attempts.
	if accepted < officeMoveBurst || accepted > officeMoveBurst+2 {
		t.Fatalf("accepted %d moves in one burst, want about %d", accepted, officeMoveBurst)
	}
}

// Two tabs of the same member are one avatar: the second join does not
// announce a peer, and the avatar disappears only when the last tab goes.
func TestOfficeDisconnectRemovesAvatarOnLastConnection(t *testing.T) {
	hub := NewHub()
	aliceTabOne := newOfficeTestClient(hub, "alice")
	aliceTabTwo := newOfficeTestClient(hub, "alice")
	bob := newOfficeTestClient(hub, "bob")

	aliceTabOne.handleOfficeJoin(officeJoinPayload(t, 100, 120, "down"))
	mustOfficeFrame(t, aliceTabOne)
	mustOfficeFrame(t, aliceTabOne)

	bob.handleOfficeJoin(officeJoinPayload(t, 200, 200, "left"))
	mustOfficeFrame(t, bob)
	mustOfficeFrame(t, bob)
	mustOfficeFrame(t, aliceTabOne) // peer_join for bob

	aliceTabTwo.handleOfficeJoin(officeJoinPayload(t, 100, 120, "down"))
	// Rejoin sends the second tab its snapshot but tells nobody else.
	if frame := mustOfficeFrame(t, aliceTabTwo); frame.Type != "office_state" {
		t.Fatalf("second tab got %q, want office_state only", frame.Type)
	}
	expectNoOfficeFrame(t, bob)

	hub.removeClient(aliceTabOne)
	expectNoOfficeFrame(t, bob)

	hub.removeClient(aliceTabTwo)
	leave := mustOfficeFrame(t, bob)
	if leave.Type != "office_peer_leave" {
		t.Fatalf("frame type = %q, want office_peer_leave", leave.Type)
	}
	var payload map[string]string
	if err := json.Unmarshal(leave.Payload, &payload); err != nil {
		t.Fatalf("unmarshal leave payload: %v", err)
	}
	if payload["user_id"] != "alice" {
		t.Fatalf("leave user_id = %q, want alice", payload["user_id"])
	}
}

func TestOfficeLeaveAnnouncesOnceAndAllowsRejoin(t *testing.T) {
	hub := NewHub()
	alice := newOfficeTestClient(hub, "alice")
	bob := newOfficeTestClient(hub, "bob")
	alice.handleOfficeJoin(officeJoinPayload(t, 100, 120, "down"))
	bob.handleOfficeJoin(officeJoinPayload(t, 200, 200, "left"))
	for range 3 {
		mustOfficeFrame(t, alice)
	}
	mustOfficeFrame(t, bob)
	mustOfficeFrame(t, bob)

	alice.handleOfficeLeave()
	if frame := mustOfficeFrame(t, bob); frame.Type != "office_peer_leave" {
		t.Fatalf("frame type = %q, want office_peer_leave", frame.Type)
	}
	alice.handleOfficeLeave()
	expectNoOfficeFrame(t, bob)
	if alice.officeJoined {
		t.Fatal("leave must clear the connection's office membership")
	}
	if len(alice.subscriptions) == 0 {
		t.Fatal("leave should not drop the connection's base subscriptions")
	}
	hub.mu.RLock()
	_, stillSubscribed := alice.subscriptions[sk(ScopeOffice, officeTestWorkspace)]
	hub.mu.RUnlock()
	if stillSubscribed {
		t.Fatal("leave must unsubscribe the office scope")
	}

	alice.handleOfficeJoin(officeJoinPayload(t, 64, 64, "up"))
	if frame := mustOfficeFrame(t, alice); frame.Type != "office_peer_join" {
		t.Fatalf("rejoin frame type = %q, want office_peer_join", frame.Type)
	}
	if frame := mustOfficeFrame(t, bob); frame.Type != "office_peer_join" {
		t.Fatalf("peer frame type = %q, want office_peer_join", frame.Type)
	}
}
