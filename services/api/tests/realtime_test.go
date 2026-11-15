package tests

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestRealtimeDirectDeliveryAndReceipts(t *testing.T) {
	resetDB(t)
	alice := registerNewUser(t, "alice")
	bob := registerNewUser(t, "bob")
	convID := directConv(t, alice, bob)

	wa := alice.ws(t)
	defer wa.close()
	wb := bob.ws(t)
	defer wb.close()

	// Alice sends over WS.
	wa.send(t, "message.send", map[string]any{
		"conversation_id":   convID,
		"text":              "Hello over WebSocket!",
		"client_message_id": "22222222-2222-2222-2222-222222222222",
	})

	// Alice gets a message.sent ack.
	ack := wa.waitFor(t, 5*time.Second, "message.sent")
	msg := envData(t, ack)["message"].(map[string]any)
	if msg["text"] != "Hello over WebSocket!" {
		t.Fatalf("ack payload wrong: %v", msg)
	}
	if msg["seq"].(float64) != 1 {
		t.Fatalf("expected seq 1, got %v", msg["seq"])
	}

	// Bob receives message.created without any REST polling.
	created := wb.waitFor(t, 5*time.Second, "message.created")
	bmsg := envData(t, created)["message"].(map[string]any)
	if bmsg["id"] != msg["id"] {
		t.Fatalf("bob got different message id: %v vs %v", bmsg["id"], msg["id"])
	}

	// Bob acknowledges delivery.
	wb.send(t, "receipt.deliver", map[string]any{"conversation_id": convID, "max_seq": 1})
	// Alice learns about delivery.
	delivered := wa.waitFor(t, 5*time.Second, "receipt.delivered")
	if envData(t, delivered)["user_id"] != bob.ID {
		t.Fatalf("delivered receipt from wrong user: %v", delivered)
	}

	// Bob reads.
	wb.send(t, "receipt.read", map[string]any{"conversation_id": convID, "max_seq": 1})
	readEvt := wa.waitFor(t, 5*time.Second, "receipt.read")
	if envData(t, readEvt)["max_seq"].(float64) != 1 {
		t.Fatalf("read receipt wrong: %v", readEvt)
	}

	// Alice's unread count for the conversation from her perspective stays 0
	// (her own message), and REST list reflects receipts for her message.
	list := asMap(t, alice.C.mustJSON(t, "GET", "/v1/conversations/"+convID+"/messages", nil, 200))
	items := rawList(t, list["messages"])
	rc, _ := items[0]["receipts"].(map[string]any)
	if rc == nil {
		t.Fatalf("receipts missing on own message: %v", items[0])
	}
	if rc["delivered_count"].(float64) < 1 || rc["read_count"].(float64) < 1 {
		t.Fatalf("receipts not counted: %v", rc)
	}
}

func TestRealtimeTypingAndPresence(t *testing.T) {
	resetDB(t)
	alice := registerNewUser(t, "alice")
	bob := registerNewUser(t, "bob")
	convID := directConv(t, alice, bob)

	wa := alice.ws(t)
	defer wa.close()
	wb := bob.ws(t)
	defer wb.close()

	// Alice types → Bob sees typing.start.
	wa.send(t, "typing.start", map[string]any{"conversation_id": convID})
	typing := wb.waitFor(t, 5*time.Second, "typing.start")
	if envData(t, typing)["user_id"] != alice.ID {
		t.Fatalf("typing event wrong user: %v", typing)
	}
	wa.send(t, "typing.stop", map[string]any{"conversation_id": convID})
	wb.waitFor(t, 5*time.Second, "typing.stop")

	// Presence audience filtering: a stranger's connection produces no events.
	charlie := registerNewUser(t, "charlie")
	wc := charlie.ws(t)
	time.Sleep(300 * time.Millisecond)

	// Alice and Charlie are NOT contacts yet — no presence event should arrive.
	// (Their eventual conversation exists but presence requires contacts.)

	// Establish contact: alice requests, charlie accepts.
	req := asMap(t, alice.C.mustJSON(t, "POST", "/v1/contacts/requests", map[string]any{"user_id": charlie.ID}, http.StatusCreated))
	charlie.C.mustJSON(t, "POST", "/v1/contacts/requests/"+fmt.Sprint(req["id"])+"/accept", nil, http.StatusOK)

	// Charlie reconnects (fresh first connection) → presence.online reaches alice.
	wc.close()
	time.Sleep(300 * time.Millisecond) // allow unregister to settle
	wc2 := charlie.ws(t)
	defer wc2.close()
	env := wa.waitFor(t, 6*time.Second, "presence.online")
	if envData(t, env)["user_id"] != charlie.ID {
		t.Fatalf("presence.online wrong user: %v", env)
	}
}

func TestRealtimeOfflineSync(t *testing.T) {
	resetDB(t)
	alice := registerNewUser(t, "alice")
	bob := registerNewUser(t, "bob")
	convID := directConv(t, alice, bob)

	// Bob is offline: messages exchanged while he's away.
	wa := alice.ws(t)
	defer wa.close()

	wa.send(t, "message.send", map[string]any{
		"conversation_id": convID, "text": "offline msg 1", "client_message_id": "33333333-3333-3333-3333-333333333331",
	})
	wa.waitFor(t, 5*time.Second, "message.sent")
	wa.send(t, "message.send", map[string]any{
		"conversation_id": convID, "text": "offline msg 2", "client_message_id": "33333333-3333-3333-3333-333333333332",
	})
	wa.waitFor(t, 5*time.Second, "message.sent")

	// Bob connects now; the messages must be retrievable via REST sync
	// (PostgreSQL is the source of truth).
	list := asMap(t, bob.C.mustJSON(t, "GET", "/v1/conversations/"+convID+"/messages", nil, 200))
	items := rawList(t, list["messages"])
	if len(items) != 2 {
		t.Fatalf("expected 2 messages after reconnect sync, got %d", len(items))
	}

	// Bob's WS connection receives no historical replay; he pulls via REST and
	// then acks read via WS.
	wb := bob.ws(t)
	defer wb.close()
	wb.send(t, "receipt.read", map[string]any{"conversation_id": convID, "max_seq": 2})
	delivered := wa.waitFor(t, 5*time.Second, "receipt.read")
	if envData(t, delivered)["max_seq"].(float64) != 2 {
		t.Fatalf("read cursor not advanced: %v", delivered)
	}
}

func TestRealtimeMultiSessionSync(t *testing.T) {
	resetDB(t)
	alice := registerNewUser(t, "alice")
	bob := registerNewUser(t, "bob")
	convID := directConv(t, alice, bob)

	// Alice logged in from two browsers (two sessions).
	wa1 := alice.ws(t)
	defer wa1.close()
	wa2 := alice.ws(t) // second session
	defer wa2.close()
	wb := bob.ws(t)
	defer wb.close()

	// Alice sends from session 1.
	wa1.send(t, "message.send", map[string]any{
		"conversation_id": convID, "text": "two-device test", "client_message_id": "44444444-4444-4444-4444-444444444444",
	})

	// Session 2 receives message.created (sender's other devices get fanout).
	env := wa2.waitFor(t, 5*time.Second, "message.created")
	if m, ok := envData(t, env)["message"].(map[string]any); !ok || m["text"] != "two-device test" {
		t.Fatalf("second session missing fanout: %v", env)
	}

	// Bob receives it too.
	benv := wb.waitFor(t, 5*time.Second, "message.created")
	if benv == nil {
		t.Fatal("bob missing message")
	}
	_ = fmt.Sprint()
}
