package realtime

import (
	"encoding/json"
	"sync"
	"time"
)

// ScopeOffice is the room an /office page subscribes to. Membership is the
// workspace the connection was authenticated for, so a client can only ever
// join its own workspace's office.
const ScopeOffice = "office"

// Logical world size in pixels. Clients render a 16px tile grid on a camera;
// the server only needs the bounds to reject nonsensical coordinates.
const (
	officeWorldWidth  = 896
	officeWorldHeight = 544
)

// Movement rate limit per connection: a burst for the first frames after a
// tab regains focus, then a steady refill that comfortably covers the
// client's 8Hz uplink.
const (
	officeMoveBurst      = 24
	officeMovePerSecond  = 12
	officeMaxFrameLength = 120
)

type officeMovePayload struct {
	X   float64 `json:"x"`
	Y   float64 `json:"y"`
	Dir string  `json:"dir"`
}

// officeUser is the server's memory of one member in one workspace office.
// Connections are refcounted so two tabs of the same member are one avatar:
// the last move wins and the avatar disappears only when the last tab leaves.
type officeUser struct {
	UserID string  `json:"user_id"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Dir    string  `json:"dir"`
	conns  int
}

type officeRegistry struct {
	mu    sync.Mutex
	rooms map[string]map[string]*officeUser // workspaceID -> userID -> user
}

func newOfficeRegistry() *officeRegistry {
	return &officeRegistry{rooms: make(map[string]map[string]*officeUser)}
}

func (r *officeRegistry) join(workspaceID, userID string, p officeMovePayload) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	room, ok := r.rooms[workspaceID]
	if !ok {
		room = make(map[string]*officeUser)
		r.rooms[workspaceID] = room
	}
	user, exists := room[userID]
	if !exists {
		user = &officeUser{UserID: userID}
		room[userID] = user
	}
	user.conns++
	// A rejoin (new tab, reconnect) keeps the previous position unless the
	// client sent one, so the avatar does not teleport on refresh.
	if p.X != 0 || p.Y != 0 {
		user.X, user.Y = p.X, p.Y
	}
	if p.Dir != "" {
		user.Dir = p.Dir
	}
	return !exists
}

// leave decrements the member's connection count and reports whether the
// avatar should disappear (last tab closed).
func (r *officeRegistry) leave(workspaceID, userID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	room, ok := r.rooms[workspaceID]
	if !ok {
		return false
	}
	user, ok := room[userID]
	if !ok {
		return false
	}
	user.conns--
	if user.conns > 0 {
		return false
	}
	delete(room, userID)
	if len(room) == 0 {
		delete(r.rooms, workspaceID)
	}
	return true
}

func (r *officeRegistry) move(workspaceID, userID string, p officeMovePayload) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	user, ok := r.rooms[workspaceID][userID]
	if !ok {
		return false
	}
	user.X, user.Y, user.Dir = p.X, p.Y, p.Dir
	return true
}

func (r *officeRegistry) snapshot(workspaceID string) []officeUser {
	r.mu.Lock()
	defer r.mu.Unlock()
	room := r.rooms[workspaceID]
	out := make([]officeUser, 0, len(room))
	for _, user := range room {
		out = append(out, *user)
	}
	return out
}

func validOfficeMove(p officeMovePayload) bool {
	if p.X < 0 || p.X > officeWorldWidth || p.Y < 0 || p.Y > officeWorldHeight {
		return false
	}
	switch p.Dir {
	case "", "up", "down", "left", "right":
		return true
	default:
		return false
	}
}

// allowOfficeMove applies the connection's token bucket. Only the read pump
// calls this, so the fields need no lock.
func (c *Client) allowOfficeMove() bool {
	now := time.Now()
	if c.officeTokens == 0 {
		c.officeTokens = officeMoveBurst
		c.officeRefilledAt = now
	}
	elapsed := now.Sub(c.officeRefilledAt).Seconds()
	c.officeTokens += elapsed * officeMovePerSecond
	if c.officeTokens > officeMoveBurst {
		c.officeTokens = officeMoveBurst
	}
	c.officeRefilledAt = now
	if c.officeTokens < 1 {
		return false
	}
	c.officeTokens--
	return true
}

func (c *Client) officeError(reason string) {
	c.sendJSON(map[string]any{
		"type":    "office_error",
		"payload": map[string]string{"error": reason},
	})
}

// handleOfficeJoin registers the member in their workspace's office and sends
// them the current room. Peers receive the join so they can spawn the avatar
// without waiting for its first move.
func (c *Client) handleOfficeJoin(raw json.RawMessage) {
	if c.workspaceID == "" {
		c.officeError("no_workspace")
		return
	}
	var p officeMovePayload
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil || !validOfficeMove(p) {
			c.officeError("invalid_position")
			return
		}
	}
	// A rejoin from the same connection (heartbeat re-announce) must not
	// refcount the avatar twice; treat it as a position update.
	if c.officeJoined {
		if (p.X != 0 || p.Y != 0) && !c.hub.office.move(c.workspaceID, c.userID, p) {
			c.officeJoined = false
		} else if p.X != 0 || p.Y != 0 {
			c.hub.broadcastOffice(c.workspaceID, map[string]any{
				"type": "office_peer_move",
				"payload": officeUser{
					UserID: c.userID,
					X:      p.X,
					Y:      p.Y,
					Dir:    p.Dir,
				},
			})
		}
		c.sendJSON(map[string]any{
			"type":    "office_state",
			"payload": map[string]any{"members": c.hub.office.snapshot(c.workspaceID)},
		})
		return
	}

	c.hub.subscribe(c, ScopeOffice, c.workspaceID)
	c.officeJoined = true
	newMember := c.hub.office.join(c.workspaceID, c.userID, p)
	if newMember {
		c.hub.broadcastOffice(c.workspaceID, map[string]any{
			"type": "office_peer_join",
			"payload": officeUser{
				UserID: c.userID,
				X:      p.X,
				Y:      p.Y,
				Dir:    p.Dir,
			},
		})
	}
	c.sendJSON(map[string]any{
		"type":    "office_state",
		"payload": map[string]any{"members": c.hub.office.snapshot(c.workspaceID)},
	})
}

// handleOfficeMove updates the member's position and relays it. Moves from a
// connection that never joined are ignored rather than silently creating an
// avatar: the client must have completed the join handshake.
func (c *Client) handleOfficeMove(raw json.RawMessage) {
	if !c.allowOfficeMove() {
		return
	}
	var p officeMovePayload
	if len(raw) == 0 || len(raw) > officeMaxFrameLength {
		return
	}
	if err := json.Unmarshal(raw, &p); err != nil || !validOfficeMove(p) {
		return
	}
	if !c.hub.office.move(c.workspaceID, c.userID, p) {
		return
	}
	c.hub.broadcastOffice(c.workspaceID, map[string]any{
		"type": "office_peer_move",
		"payload": officeUser{
			UserID: c.userID,
			X:      p.X,
			Y:      p.Y,
			Dir:    p.Dir,
		},
	})
}

// handleOfficeLeave drops the member from the room immediately; disconnects
// do the same from removeClient, so an explicit leave is only a fast path.
func (c *Client) handleOfficeLeave() {
	if !c.officeJoined {
		return
	}
	c.officeJoined = false
	c.hub.unsubscribe(c, ScopeOffice, c.workspaceID)
	if c.hub.office.leave(c.workspaceID, c.userID) {
		c.hub.broadcastOffice(c.workspaceID, map[string]any{
			"type":    "office_peer_leave",
			"payload": map[string]string{"user_id": c.userID},
		})
	}
}

// leaveOffice is called from removeClient. It reports whether the avatar
// should be removed so the caller can broadcast after releasing the hub lock.
func (c *Client) leaveOffice() bool {
	if c.workspaceID == "" {
		return false
	}
	return c.hub.office.leave(c.workspaceID, c.userID)
}

func (h *Hub) broadcastOffice(workspaceID string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.BroadcastToScope(ScopeOffice, workspaceID, data)
}
