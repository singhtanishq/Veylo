package tests

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestContactRequests(t *testing.T) {
	resetDB(t)
	alice := registerNewUser(t, "alice")
	bob := registerNewUser(t, "bob")

	// Alice requests Bob.
	req := asMap(t, alice.C.mustJSON(t, "POST", "/v1/contacts/requests", map[string]any{"user_id": bob.ID}, http.StatusCreated))
	reqID, _ := req["id"].(string)

	// Incoming requests visible to Bob.
	incoming := bob.C.mustJSON(t, "GET", "/v1/contacts/requests?direction=incoming", nil, http.StatusOK)
	if len(rawList(t, incoming)) != 1 {
		t.Fatalf("expected 1 incoming request")
	}

	// Bob accepts.
	bob.C.mustJSON(t, "POST", "/v1/contacts/requests/"+reqID+"/accept", nil, http.StatusOK)

	// Both sides see each other as contacts.
	for _, u := range []*registeredUser{alice, bob} {
		contacts := u.C.mustJSON(t, "GET", "/v1/contacts", nil, http.StatusOK)
		if len(rawList(t, contacts)) != 1 {
			t.Fatalf("expected 1 contact for %s", u.Username)
		}
	}

	// Re-request rejected with conflict.
	alice.C.mustJSON(t, "POST", "/v1/contacts/requests", map[string]any{"user_id": bob.ID}, http.StatusConflict)

	// Remove contact.
	alice.C.mustJSON(t, "DELETE", "/v1/contacts/"+bob.ID, nil, http.StatusOK)
	contacts := bob.C.mustJSON(t, "GET", "/v1/contacts", nil, http.StatusOK)
	if len(rawList(t, contacts)) != 0 {
		t.Fatalf("contact removal should be mutual")
	}
}

func TestContactRequestsMutualAutoAccept(t *testing.T) {
	resetDB(t)
	alice := registerNewUser(t, "alice")
	bob := registerNewUser(t, "bob")

	// Alice → Bob, then Bob → Alice: second request auto-accepts.
	alice.C.mustJSON(t, "POST", "/v1/contacts/requests", map[string]any{"user_id": bob.ID}, http.StatusCreated)
	resp := asMap(t, bob.C.mustJSON(t, "POST", "/v1/contacts/requests", map[string]any{"user_id": alice.ID}, http.StatusOK))
	if resp["status"] != "accepted" {
		t.Fatalf("expected auto-accept, got %v", resp)
	}
	contacts := alice.C.mustJSON(t, "GET", "/v1/contacts", nil, http.StatusOK)
	if len(rawList(t, contacts)) != 1 {
		t.Fatalf("expected mutual contacts")
	}
}

func TestBlocking(t *testing.T) {
	resetDB(t)
	alice := registerNewUser(t, "alice")
	bob := registerNewUser(t, "bob")

	// Alice blocks Bob.
	alice.C.mustJSON(t, "POST", "/v1/contacts/blocks", map[string]any{"user_id": bob.ID}, http.StatusOK)

	// New conversations blocked in both directions.
	alice.C.mustJSON(t, "POST", "/v1/conversations/direct", map[string]any{"user_id": bob.ID}, http.StatusForbidden)
	bob.C.mustJSON(t, "POST", "/v1/conversations/direct", map[string]any{"user_id": alice.ID}, http.StatusForbidden)

	// Bob's search does not surface Alice (she blocked him → anti-harassment).
	results := bob.C.mustJSON(t, "GET", "/v1/users/search?q=@alice", nil, http.StatusOK)
	if len(rawList(t, results)) != 0 {
		t.Fatalf("blocked user must not appear in search")
	}

	// Block list shows the entry.
	blocks := alice.C.mustJSON(t, "GET", "/v1/contacts/blocks", nil, http.StatusOK)
	if len(rawList(t, blocks)) != 1 {
		t.Fatalf("expected 1 blocked user")
	}

	// Unblock restores normal behavior.
	alice.C.mustJSON(t, "DELETE", "/v1/contacts/blocks/"+bob.ID, nil, http.StatusOK)
	conv := asMap(t, alice.C.mustJSON(t, "POST", "/v1/conversations/direct", map[string]any{"user_id": bob.ID}, http.StatusOK))
	if conv["id"] == nil {
		t.Fatalf("expected conversation after unblock")
	}
}

func TestDirectConversationDeduplication(t *testing.T) {
	resetDB(t)
	alice := registerNewUser(t, "alice")
	bob := registerNewUser(t, "bob")

	// Create twice → same conversation.
	c1 := asMap(t, alice.C.mustJSON(t, "POST", "/v1/conversations/direct", map[string]any{"user_id": bob.ID}, http.StatusOK))
	c2 := asMap(t, bob.C.mustJSON(t, "POST", "/v1/conversations/direct", map[string]any{"user_id": alice.ID}, http.StatusOK))
	if c1["id"] != c2["id"] {
		t.Fatalf("duplicate direct conversations: %s vs %s", c1["id"], c2["id"])
	}

	// Concurrent creation also deduplicates.
	var ids []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cc := newClient()
			cc.mustJSON(t, "POST", "/v1/auth/login", map[string]any{
				"identifier": alice.Email, "password": alice.Password,
			}, http.StatusOK)
			data := asMap(t, cc.mustJSON(t, "POST", "/v1/conversations/direct", map[string]any{"user_id": bob.ID}, http.StatusOK))
			mu.Lock()
			ids = append(ids, fmt.Sprint(data["id"]))
			mu.Unlock()
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if id != fmt.Sprint(c1["id"]) {
			t.Fatalf("concurrent creation produced duplicate conversation %s", id)
		}
	}
	_ = time.Now()
}
