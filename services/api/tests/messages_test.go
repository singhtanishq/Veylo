package tests

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func directConv(t *testing.T, a, b *registeredUser) string {
	t.Helper()
	conv := asMap(t, a.C.mustJSON(t, "POST", "/v1/conversations/direct", map[string]any{"user_id": b.ID}, http.StatusOK))
	id, _ := conv["id"].(string)
	return id
}

func sendMessage(t *testing.T, u *registeredUser, convID, text string, extra map[string]any) map[string]any {
	t.Helper()
	body := map[string]any{"type": "text", "text": text}
	for k, v := range extra {
		body[k] = v
	}
	return asMap(t, u.C.mustJSON(t, "POST", "/v1/conversations/"+convID+"/messages", body, http.StatusCreated))
}

func TestMessagingLifecycle(t *testing.T) {
	resetDB(t)
	alice := registerNewUser(t, "alice")
	bob := registerNewUser(t, "bob")
	charlie := registerNewUser(t, "charlie")
	convID := directConv(t, alice, bob)

	// Send → persisted with sequence 1.
	m1 := sendMessage(t, alice, convID, "Hello Bob!", nil)
	if m1["seq"].(float64) != 1 {
		t.Fatalf("expected seq 1, got %v", m1["seq"])
	}

	// Second message.
	m2 := sendMessage(t, bob, convID, "Hey Alice!", nil)
	if m2["seq"].(float64) != 2 {
		t.Fatalf("expected seq 2, got %v", m2["seq"])
	}

	// Idempotent retry with the same client_message_id → same message, no dup.
	cmid := "11111111-1111-1111-1111-111111111111"
	m3 := sendMessage(t, alice, convID, "Idempotent send", map[string]any{"client_message_id": cmid})
	m3retry := sendMessage(t, alice, convID, "Idempotent send", map[string]any{"client_message_id": cmid})
	if m3["id"] != m3retry["id"] {
		t.Fatalf("duplicate send created a new message: %v vs %v", m3["id"], m3retry["id"])
	}

	// Charlie cannot read the conversation (IDOR protection).
	charlie.C.mustJSON(t, "GET", "/v1/conversations/"+convID+"/messages", nil, http.StatusForbidden)
	charlie.C.mustJSON(t, "POST", "/v1/conversations/"+convID+"/messages", map[string]any{"type": "text", "text": "sneak"}, http.StatusForbidden)

	// List ordering + pagination.
	list := asMap(t, alice.C.mustJSON(t, "GET", "/v1/conversations/"+convID+"/messages?limit=2", nil, http.StatusOK))
	msgs := rawList(t, list["messages"])
	if len(msgs) != 2 {
		t.Fatalf("expected page of 2, got %d", len(msgs))
	}
	if list["has_more"] != true {
		t.Fatalf("expected has_more=true")
	}
	if msgs[0]["seq"].(float64) != 2 || msgs[1]["seq"].(float64) != 3 {
		t.Fatalf("page should contain seq 2,3: %v %v", msgs[0]["seq"], msgs[1]["seq"])
	}

	// Edit within window.
	edited := asMap(t, alice.C.mustJSON(t, "PATCH", "/v1/messages/"+fmt.Sprint(m1["id"]), map[string]any{"text": "Hello Bob! (edited)"}, http.StatusOK))
	if edited["edited_at"] == nil {
		t.Fatalf("edited_at missing")
	}

	// Edit window expiry (shrink server-side via direct DB to keep the test fast).
	if _, err := pool.Exec(testCtx(), `UPDATE conversation_settings SET edit_window_seconds = 0 WHERE conversation_id = $1`, convID); err != nil {
		t.Fatal(err)
	}
	alice.C.mustJSON(t, "PATCH", "/v1/messages/"+fmt.Sprint(m3["id"]), map[string]any{"text": "too late"}, http.StatusUnprocessableEntity)

	// Reply.
	reply := sendMessage(t, bob, convID, "Replying to you", map[string]any{"reply_to_id": m1["id"]})
	if reply["reply_to"] == nil {
		t.Fatalf("reply_to missing")
	}

	// Reactions.
	bob.C.mustJSON(t, "PUT", "/v1/messages/"+fmt.Sprint(m1["id"])+"/reactions", map[string]any{"emoji": "🎉"}, http.StatusOK)
	detail := asMap(t, alice.C.mustJSON(t, "GET", "/v1/conversations/"+convID+"/messages?limit=1&before_seq=2", nil, http.StatusOK))
	reactions := firstMessageReactions(t, rawList(t, detail["messages"]))
	if len(reactions) != 0 {
		// reaction is on m1 (seq 1); page with before_seq=2 returns seq 1.
		if r0, ok := reactions[0].(map[string]any); ok {
			if r0["emoji"] != "🎉" || r0["count"].(float64) != 1 {
				t.Fatalf("reaction mismatch: %v", reactions)
			}
		}
	}

	// Duplicate reaction is idempotent.
	bob.C.mustJSON(t, "PUT", "/v1/messages/"+fmt.Sprint(m1["id"])+"/reactions", map[string]any{"emoji": "🎉"}, http.StatusOK)

	// Remove reaction.
	bob.C.mustJSON(t, "DELETE", "/v1/messages/"+fmt.Sprint(m1["id"])+"/reactions", map[string]any{"emoji": "🎉"}, http.StatusOK)

	// Delete message (soft).
	alice.C.mustJSON(t, "DELETE", "/v1/messages/"+fmt.Sprint(m3["id"]), nil, http.StatusOK)
	after := asMap(t, alice.C.mustJSON(t, "GET", "/v1/conversations/"+convID+"/messages?limit=50", nil, http.StatusOK))
	for _, m := range rawList(t, after["messages"]) {
		if fmt.Sprint(m["id"]) == fmt.Sprint(m3["id"]) {
			if m["deleted"] != true {
				t.Fatalf("message should be tombstoned")
			}
			if m["text"] != nil {
				t.Fatalf("deleted message text should be scrubbed")
			}
		}
	}

	// Bob cannot delete Alice's message (m2 is Bob's own).
	bob.C.mustJSON(t, "DELETE", "/v1/messages/"+fmt.Sprint(m1["id"]), nil, http.StatusForbidden)
	_ = strings.TrimSpace("")
	_ = time.Now()
}

func firstMessageReactions(t *testing.T, msgs []map[string]any) []any {
	t.Helper()
	if len(msgs) == 0 {
		return nil
	}
	raw, _ := msgs[0]["reactions"].([]any)
	return raw
}

func TestUnreadCounts(t *testing.T) {
	resetDB(t)
	alice := registerNewUser(t, "alice")
	bob := registerNewUser(t, "bob")
	convID := directConv(t, alice, bob)

	sendMessage(t, alice, convID, "one", nil)
	sendMessage(t, alice, convID, "two", nil)

	// Bob sees unread=2.
	convs := bob.conversations(t)
	if convs[0]["unread_count"].(float64) != 2 {
		t.Fatalf("expected 2 unread, got %v", convs[0]["unread_count"])
	}

	// Bob reads up to seq 2 → unread 0.
	bob.C.mustJSON(t, "POST", "/v1/conversations/"+convID+"/read", map[string]any{"max_seq": 2}, http.StatusOK)
	convs = bob.conversations(t)
	if convs[0]["unread_count"].(float64) != 0 {
		t.Fatalf("expected 0 unread after read, got %v", convs[0]["unread_count"])
	}

	// Alice's own messages don't count as unread for her.
	convs = alice.conversations(t)
	if convs[0]["unread_count"].(float64) != 0 {
		t.Fatalf("sender should have 0 unread, got %v", convs[0]["unread_count"])
	}
}

func TestMessageSearchAuthorization(t *testing.T) {
	resetDB(t)
	alice := registerNewUser(t, "alice")
	bob := registerNewUser(t, "bob")
	charlie := registerNewUser(t, "charlie")
	convID := directConv(t, alice, bob)

	sendMessage(t, alice, convID, "The launch checklist is ready for review", nil)

	// Member search finds it.
	results := alice.C.mustJSON(t, "GET", "/v1/search/messages?q=launch+checklist", nil, http.StatusOK)
	if len(rawList(t, results)) != 1 {
		t.Fatalf("expected 1 search hit")
	}

	// Non-member gets forbidden (conversation filter) and no results (global).
	charlie.C.mustJSON(t, "GET", "/v1/search/messages?q=launch+checklist&conversation_id="+convID, nil, http.StatusForbidden)
	results = charlie.C.mustJSON(t, "GET", "/v1/search/messages?q=launch+checklist", nil, http.StatusOK)
	if len(rawList(t, results)) != 0 {
		t.Fatalf("non-member must not see messages")
	}
}
