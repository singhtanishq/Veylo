package tests

import (
	"fmt"
	"net/http"
	"testing"
)

func TestGroupsLifecycle(t *testing.T) {
	resetDB(t)
	alice := registerNewUser(t, "alice")
	bob := registerNewUser(t, "bob")
	charlie := registerNewUser(t, "charlie")
	david := registerNewUser(t, "david")

	// Create group with Bob + Charlie.
	conv := asMap(t, alice.C.mustJSON(t, "POST", "/v1/conversations/group", map[string]any{
		"title":       "Launch Crew",
		"description": "Shipping together",
		"member_ids":  []string{bob.ID, charlie.ID},
	}, http.StatusCreated))
	convID := fmt.Sprint(conv["id"])

	// System message recorded.
	msgs := asMap(t, alice.C.mustJSON(t, "GET", "/v1/conversations/"+convID+"/messages", nil, http.StatusOK))
	items := rawList(t, msgs["messages"])
	if len(items) != 1 || items[0]["type"] != "system" {
		t.Fatalf("expected system message, got %v", items)
	}

	// Creator is owner; members are members.
	details := bob.C.mustJSON(t, "GET", "/v1/conversations/"+convID, nil, http.StatusOK)
	members := rawList(t, asMap(t, details)["members"])
	if len(members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(members))
	}

	// Member (bob) cannot edit group without admin rights.
	bob.C.mustJSON(t, "PATCH", "/v1/conversations/"+convID, map[string]any{"title": "Hacked"}, http.StatusForbidden)

	// Owner edits.
	alice.C.mustJSON(t, "PATCH", "/v1/conversations/"+convID, map[string]any{"title": "Launch Crew v2"}, http.StatusOK)

	// Promote bob to admin.
	alice.C.mustJSON(t, "PATCH", "/v1/conversations/"+convID+"/members/"+bob.ID+"/role", map[string]any{"role": "admin"}, http.StatusOK)

	// Admin adds david.
	alice.C.mustJSON(t, "POST", "/v1/conversations/"+convID+"/members", map[string]any{"user_ids": []string{david.ID}}, http.StatusOK)

	// Group message reaches all four.
	sendMessage(t, bob, convID, "Welcome david!", nil)
	for _, u := range []*registeredUser{alice, bob, charlie, david} {
		convs := u.conversations(t)
		found := false
		for _, c := range convs {
			if fmt.Sprint(c["id"]) == convID {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s missing group conversation", u.Username)
		}
	}

	// Member (charlie) cannot remove david (needs admin).
	charlie.C.mustJSON(t, "DELETE", "/v1/conversations/"+convID+"/members/"+david.ID, nil, http.StatusForbidden)

	// Admin removes david → system message.
	alice.C.mustJSON(t, "DELETE", "/v1/conversations/"+convID+"/members/"+david.ID, nil, http.StatusOK)

	// Removed member loses access.
	david.C.mustJSON(t, "GET", "/v1/conversations/"+convID+"/messages", nil, http.StatusForbidden)

	// Leave flow: charlie leaves; alice (owner) leaves → ownership transfers.
	charlie.C.mustJSON(t, "DELETE", "/v1/conversations/"+convID+"/members/me", nil, http.StatusOK)
	alice.C.mustJSON(t, "DELETE", "/v1/conversations/"+convID+"/members/me", nil, http.StatusOK)

	// Bob is now owner.
	details = bob.C.mustJSON(t, "GET", "/v1/conversations/"+convID, nil, http.StatusOK)
	var bobRole string
	for _, m := range rawList(t, asMap(t, details)["members"]) {
		if fmt.Sprint(m["id"]) == bob.ID {
			bobRole, _ = m["role"].(string)
		}
	}
	if bobRole != "owner" {
		t.Fatalf("ownership should transfer to bob, got %s", bobRole)
	}
}

func TestGroupInvitations(t *testing.T) {
	resetDB(t)
	alice := registerNewUser(t, "alice")
	bob := registerNewUser(t, "bob")
	conv := asMap(t, alice.C.mustJSON(t, "POST", "/v1/conversations/group", map[string]any{
		"title": "Invite Only", "member_ids": []string{},
	}, http.StatusCreated))
	convID := fmt.Sprint(conv["id"])

	// Invite bob.
	inv := asMap(t, alice.C.mustJSON(t, "POST", "/v1/conversations/"+convID+"/invitations", map[string]any{
		"user_ids": []string{bob.ID},
	}, http.StatusCreated))
	invited, _ := inv["invited"].([]any)
	if len(invited) != 1 {
		t.Fatalf("expected 1 invitation")
	}

	// Bob has no access yet.
	bob.C.mustJSON(t, "GET", "/v1/conversations/"+convID+"/messages", nil, http.StatusForbidden)

	// Pending invitation listed for bob.
	alice.C.mustJSON(t, "GET", "/v1/conversations/"+convID+"/invitations", nil, http.StatusOK)

	// Bob accepts → becomes member.
	invitations := bob.C.mustJSON(t, "GET", "/v1/notifications", nil, http.StatusOK)
	_ = invitations
	// Find invitation id via conversation invitations list (owner view).
	list := alice.C.mustJSON(t, "GET", "/v1/conversations/"+convID+"/invitations", nil, http.StatusOK)
	items := rawList(t, list)
	if len(items) != 1 {
		t.Fatalf("expected pending invitation")
	}
	invID := fmt.Sprint(items[0]["id"])
	accepted := asMap(t, bob.C.mustJSON(t, "POST", "/v1/conversations/"+convID+"/invitations/"+invID+"/accept", nil, http.StatusOK))
	if accepted["id"] == nil {
		t.Fatalf("expected conversation after accepting, got %v", accepted)
	}

	// Bob can now read.
	bob.C.mustJSON(t, "GET", "/v1/conversations/"+convID+"/messages", nil, http.StatusOK)
}
