// Command seed creates deterministic demo accounts, contacts, conversations,
// message history, reactions, receipts and notifications for local
// development. Development-only credentials; never run in production.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/singhtanishq/veylo/services/api/internal/platform/database"
	"github.com/singhtanishq/veylo/services/api/internal/platform/passwords"
)

// Demo credentials are documented in the README (development only).
const demoPassword = "VeyloDemo!2026"

type demoUser struct {
	username, display, email, about string
}

var demoUsers = []demoUser{
	{"alice", "Alice Morgan", "alice@example.test", "Designing calm interfaces ☕"},
	{"bob", "Bob Chen", "bob@example.test", "Building things that ship."},
	{"charlie", "Charlie Alvarez", "charlie@example.test", "Coffee first, code later."},
	{"david", "David Okafor", "david@example.test", "Runner. Reader. Real-timer."},
}

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://veylo:veylo_local_dev@localhost:5432/veylo?sslmode=disable"
	}
	storagePath := os.Getenv("STORAGE_PATH")
	if storagePath == "" {
		storagePath = "./storage/uploads"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fatal("connect", err)
	}
	defer pool.Close()

	if _, err := database.Migrate(ctx, pool, "./migrations"); err != nil {
		fatal("migrate", err)
	}

	ids, created, err := upsertUsers(ctx, pool)
	if err != nil {
		fatal("seed users", err)
	}
	if err := seedContacts(ctx, pool, ids); err != nil {
		fatal("seed contacts", err)
	}
	convDM, err := seedDirectConversations(ctx, pool, ids, storagePath)
	if err != nil {
		fatal("seed direct conversations", err)
	}
	convGroup, err := seedGroup(ctx, pool, ids)
	if err != nil {
		fatal("seed group", err)
	}
	if err := seedNotifications(ctx, pool, ids); err != nil {
		fatal("seed notifications", err)
	}
	if err := seedExtras(ctx, pool, ids); err != nil {
		fatal("seed extras", err)
	}

	_ = convDM
	_ = convGroup

	if created {
		fmt.Println("demo data seeded.")
	} else {
		fmt.Println("demo accounts already exist — refreshed profile data only.")
	}
	fmt.Println("demo password (development only):", demoPassword)
}

func fatal(stage string, err error) {
	fmt.Fprintf(os.Stderr, "seed: %s: %v\n", stage, err)
	os.Exit(1)
}

func upsertUsers(ctx context.Context, pool *pgxpool.Pool) (map[string]uuid.UUID, bool, error) {
	hash, err := passwords.Hash(demoPassword)
	if err != nil {
		return nil, false, err
	}
	ids := make(map[string]uuid.UUID, len(demoUsers))
	createdAny := false
	for _, u := range demoUsers {
		var id uuid.UUID
		var status string
		err := pool.QueryRow(ctx,
			`SELECT id, account_status FROM users WHERE username = $1`, u.username).Scan(&id, &status)
		if err == pgx.ErrNoRows {
			err = pool.QueryRow(ctx, `
				INSERT INTO users (username, username_display, email, password_hash, account_status, email_verified_at)
				VALUES ($1, $2, $3, $4, 'active', now())
				RETURNING id
			`, u.username, u.display, u.email, hash).Scan(&id)
			if err != nil {
				return nil, false, err
			}
			createdAny = true
		} else if err != nil {
			return nil, false, err
		}
		// Ensure profile + prefs exist regardless of when the account was made.
		if _, err := pool.Exec(ctx, `
			INSERT INTO user_profiles (user_id, display_name, about)
			VALUES ($1, $2, $3)
			ON CONFLICT (user_id) DO UPDATE SET display_name = EXCLUDED.display_name, about = EXCLUDED.about
		`, id, u.display, u.about); err != nil {
			return nil, false, err
		}
		for _, stmt := range []string{
			`INSERT INTO user_preferences (user_id) VALUES ($1) ON CONFLICT DO NOTHING`,
			`INSERT INTO user_privacy_settings (user_id) VALUES ($1) ON CONFLICT DO NOTHING`,
		} {
			if _, err := pool.Exec(ctx, stmt, id); err != nil {
				return nil, false, err
			}
		}
		ids[u.username] = id
	}
	return ids, createdAny, nil
}

func seedContacts(ctx context.Context, pool *pgxpool.Pool, ids map[string]uuid.UUID) error {
	pairs := [][2]string{
		{"alice", "bob"}, {"alice", "charlie"}, {"bob", "charlie"}, {"alice", "david"},
	}
	for _, p := range pairs {
		a, b := ids[p[0]], ids[p[1]]
		if _, err := pool.Exec(ctx, `
			INSERT INTO contacts (owner_id, contact_id) VALUES ($1,$2), ($2,$1) ON CONFLICT DO NOTHING
		`, a, b); err != nil {
			return err
		}
	}
	return nil
}

func ensureDirect(ctx context.Context, pool *pgxpool.Pool, a, b uuid.UUID) (uuid.UUID, error) {
	ka, kb := a.String(), b.String()
	if ka > kb {
		ka, kb = kb, ka
	}
	pairKey := ka + ":" + kb

	var convID uuid.UUID
	err := pool.QueryRow(ctx,
		`SELECT conversation_id FROM direct_conversation_keys WHERE pair_key = $1`, pairKey).Scan(&convID)
	if err == nil {
		return convID, nil
	}
	if err != pgx.ErrNoRows {
		return uuid.Nil, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, pairKey); err != nil {
		return uuid.Nil, err
	}
	if err := tx.QueryRow(ctx,
		`SELECT conversation_id FROM direct_conversation_keys WHERE pair_key = $1`, pairKey).Scan(&convID); err == nil {
		return convID, tx.Commit(ctx)
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO conversations (type, created_by) VALUES ('direct', $1) RETURNING id`, a).Scan(&convID); err != nil {
		return uuid.Nil, err
	}
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO conversation_members (conversation_id, user_id, role) VALUES ($1,$2,'member'), ($1,$3,'member')`, []any{convID, a, b}},
		{`INSERT INTO direct_conversation_keys (conversation_id, pair_key) VALUES ($1,$2)`, []any{convID, pairKey}},
		{`INSERT INTO conversation_sequences (conversation_id) VALUES ($1)`, []any{convID}},
		{`INSERT INTO conversation_settings (conversation_id) VALUES ($1)`, []any{convID}},
		{`INSERT INTO message_read_cursors (conversation_id, user_id) VALUES ($1,$2), ($1,$3)`, []any{convID, a, b}},
	} {
		if _, err := tx.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			return uuid.Nil, err
		}
	}
	return convID, tx.Commit(ctx)
}

// addMessage inserts a message with proper seq and cursor bookkeeping.
func addMessage(ctx context.Context, pool *pgxpool.Pool, convID, senderID uuid.UUID,
	msgType, text string, createdAt time.Time, replyTo *uuid.UUID) (uuid.UUID, int64, error) {

	var seq int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO conversation_sequences (conversation_id, last_seq) VALUES ($1, 1)
		ON CONFLICT (conversation_id) DO UPDATE SET last_seq = conversation_sequences.last_seq + 1
		RETURNING last_seq
	`, convID).Scan(&seq); err != nil {
		return uuid.Nil, 0, err
	}
	var id uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO messages (conversation_id, sender_id, type, text, seq, reply_to_id, created_at)
		VALUES ($1, $2, $3, NULLIF($4,''), $5, $6, $7)
		RETURNING id
	`, convID, senderID, msgType, text, seq, replyTo, createdAt).Scan(&id); err != nil {
		return uuid.Nil, 0, err
	}
	if _, err := pool.Exec(ctx,
		`UPDATE conversations SET last_message_at = GREATEST(COALESCE(last_message_at, to_timestamp(0)), $2) WHERE id = $1`,
		convID, createdAt); err != nil {
		return uuid.Nil, 0, err
	}
	return id, seq, nil
}

func seedDirectConversations(ctx context.Context, pool *pgxpool.Pool, ids map[string]uuid.UUID, storagePath string) (map[string]uuid.UUID, error) {
	out := map[string]uuid.UUID{}
	now := time.Now().UTC().Add(-2 * time.Hour)

	// Alice ↔ Bob: a realistic exchange with reactions, a reply and receipts.
	dmAB, err := ensureDirect(ctx, pool, ids["alice"], ids["bob"])
	if err != nil {
		return nil, err
	}
	out["alice-bob"] = dmAB

	// Idempotency: skip message seeding when this conversation already has
	// history (re-running the seed must not duplicate the timeline).
	var existingMsgs int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM messages WHERE conversation_id = $1`, dmAB).Scan(&existingMsgs); err != nil {
		return nil, err
	}
	if existingMsgs > 0 {
		return out, nil
	}

	type seeded struct {
		msgID uuid.UUID
		seq   int64
	}
	var msgs []seeded

	step := 0
	add := func(sender string, text string, replyTo *uuid.UUID) (uuid.UUID, int64, error) {
		step++
		return addMessage(ctx, pool, dmAB, ids[sender], "text", text, now.Add(time.Duration(step)*3*time.Minute), replyTo)
	}

	if _, _, err := add("bob", "Hey Alice! Did you see the new design system specs?", nil); err != nil {
		return nil, err
	}
	m1, s1, err := add("alice", "Just finished reviewing them — the spacing tokens are so much cleaner now ✨", nil)
	if err != nil {
		return nil, err
	}
	msgs = append(msgs, seeded{m1, s1})
	if _, _, err := add("bob", "Agreed. Want to pair on the conversation list today?", nil); err != nil {
		return nil, err
	}
	m2, s2, err := add("alice", "Yes! Let's do 3pm. I'll bring the motion prototypes.", &m1)
	if err != nil {
		return nil, err
	}
	msgs = append(msgs, seeded{m2, s2})
	m3, s3, err := add("bob", "Perfect. Also — welcome aboard Veylo 🎉", nil)
	if err != nil {
		return nil, err
	}
	msgs = append(msgs, seeded{m3, s3})

	// Demo image attachment from Bob (branded gradient banner).
	if img, err := generateSamplePNG(); err == nil {
		sum := sha256.Sum256(img)
		key := fmt.Sprintf("uploads/demo/%s/design-review.png", uuid.NewString())
		dir := filepath.Join(storagePath, filepath.Dir(key))
		if mkErr := os.MkdirAll(dir, 0o755); mkErr == nil {
			if writeErr := os.WriteFile(filepath.Join(storagePath, key), img, 0o644); writeErr == nil {
				var attID uuid.UUID
				if attErr := pool.QueryRow(ctx, `
					INSERT INTO attachments (owner_id, storage_key, file_name, mime_type, kind, size_bytes, sha256, width, height)
					VALUES ($1, $2, 'design-review.png', 'image/png', 'image', $3, $4, 640, 360)
					RETURNING id
				`, ids["bob"], key, len(img), hex.EncodeToString(sum[:])).Scan(&attID); attErr == nil {
					mImg, sImg, mErr := addMessage(ctx, pool, dmAB, ids["bob"], "image",
						"", now.Add(time.Duration(step+1)*3*time.Minute), nil)
					if mErr == nil {
						if _, err := pool.Exec(ctx, `
							INSERT INTO message_attachments (message_id, attachment_id, position) VALUES ($1, $2, 0)
						`, mImg, attID); err == nil {
							msgs = append(msgs, seeded{mImg, sImg})
							step++
							s3 = sImg // keep cursor math including the image
						}
					}
				}
			}
		}
	}

	// Reactions on two of Alice's messages.
	if _, err := pool.Exec(ctx, `
		INSERT INTO message_reactions (message_id, user_id, emoji) VALUES ($1,$2,'👍'), ($3,$2,'🎉')
		ON CONFLICT DO NOTHING
	`, msgs[0].msgID, ids["bob"], msgs[2].msgID); err != nil {
		return nil, err
	}
	// Alice's messages are delivered+read by Bob; Bob's read by Alice.
	for _, m := range msgs {
		if _, err := pool.Exec(ctx, `
			INSERT INTO message_receipts (message_id, user_id, delivered_at, read_at)
			VALUES ($1, (SELECT id FROM users WHERE username='bob'), now(), now())
			ON CONFLICT DO NOTHING
		`, m.msgID); err != nil {
			return nil, err
		}
	}
	if _, err := pool.Exec(ctx, `
		UPDATE message_read_cursors SET delivered_seq = $3, read_seq = $3
		WHERE conversation_id = $1 AND user_id = $2
	`, dmAB, ids["bob"], s3); err != nil {
		return nil, err
	}

	// Alice ↔ Charlie: shorter thread, one unread message for Alice.
	dmAC, err := ensureDirect(ctx, pool, ids["alice"], ids["charlie"])
	if err != nil {
		return nil, err
	}
	out["alice-charlie"] = dmAC
	var acMsgs int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM messages WHERE conversation_id = $1`, dmAC).Scan(&acMsgs); err != nil {
		return nil, err
	}
	if acMsgs == 0 {
		if _, _, err := addMessage(ctx, pool, dmAC, ids["charlie"], "text",
			"Morning! The standup notes are in the doc — tl;dr we're on track 🚀",
			now.Add(20*time.Minute), nil); err != nil {
			return nil, err
		}
		if _, _, err := addMessage(ctx, pool, dmAC, ids["alice"], "text",
			"Great news. I'll review the doc after lunch.", now.Add(25*time.Minute), nil); err != nil {
			return nil, err
		}
		// One unread from Charlie for Alice.
		if _, _, err := addMessage(ctx, pool, dmAC, ids["charlie"], "text",
			"Oh also — taking the team to coffee later, you in? ☕",
			now.Add(30*time.Minute), nil); err != nil {
			return nil, err
		}
		// Advance Charlie's read cursor so the last message counts as unread only for Alice.
		if _, err := pool.Exec(ctx, `
		UPDATE message_read_cursors SET delivered_seq = 2, read_seq = 2
		WHERE conversation_id = $1 AND user_id = $2
	`, dmAC, ids["charlie"]); err != nil {
			return nil, err
		}
		if _, err := pool.Exec(ctx, `
		UPDATE message_read_cursors SET delivered_seq = 1, read_seq = 1
		WHERE conversation_id = $1 AND user_id = $2
	`, dmAC, ids["alice"]); err != nil {
			return nil, err
		}
	}

	// Bob ↔ Charlie for graph completeness.
	dmBC, err := ensureDirect(ctx, pool, ids["bob"], ids["charlie"])
	if err != nil {
		return nil, err
	}
	out["bob-charlie"] = dmBC
	var bcMsgs int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM messages WHERE conversation_id = $1`, dmBC).Scan(&bcMsgs); err != nil {
		return nil, err
	}
	if bcMsgs == 0 {
		if _, _, err := addMessage(ctx, pool, dmBC, ids["bob"], "text",
			"Pushed the websocket reconnect fix — exponential backoff works like a charm now.",
			now.Add(40*time.Minute), nil); err != nil {
			return nil, err
		}
	}

	return out, nil
}

func seedGroup(ctx context.Context, pool *pgxpool.Pool, ids map[string]uuid.UUID) (uuid.UUID, error) {
	// "Project Aurora" with all four demo users.
	var convID uuid.UUID
	err := pool.QueryRow(ctx, `SELECT id FROM conversations WHERE type='group' AND title='Project Aurora'`).Scan(&convID)
	if err == nil {
		return convID, nil
	}
	if err != pgx.ErrNoRows {
		return uuid.Nil, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)

	if err := tx.QueryRow(ctx, `
		INSERT INTO conversations (type, title, description, created_by)
		VALUES ('group', 'Project Aurora', 'Everything about the Aurora launch — updates, links and decisions.', $1)
		RETURNING id
	`, ids["alice"]).Scan(&convID); err != nil {
		return uuid.Nil, err
	}
	members := []uuid.UUID{ids["alice"], ids["bob"], ids["charlie"], ids["david"]}
	for i, m := range members {
		role := "member"
		if i == 0 {
			role = "owner"
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO conversation_members (conversation_id, user_id, role) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING
		`, convID, m, role); err != nil {
			return uuid.Nil, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO message_read_cursors (conversation_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING
		`, convID, m); err != nil {
			return uuid.Nil, err
		}
	}
	for _, stmt := range []string{
		`INSERT INTO conversation_sequences (conversation_id) VALUES ($1)`,
		`INSERT INTO conversation_settings (conversation_id) VALUES ($1)`,
	} {
		if _, err := tx.Exec(ctx, stmt, convID); err != nil {
			return uuid.Nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}

	now := time.Now().UTC().Add(-3 * time.Hour)
	step := 0
	add := func(sender, text string) error {
		step++
		_, _, err := addMessage(ctx, pool, convID, ids[sender], "text", text, now.Add(time.Duration(step)*7*time.Minute), nil)
		return err
	}

	// System message (no sender).
	var seq int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO conversation_sequences (conversation_id, last_seq) VALUES ($1, 1)
		ON CONFLICT (conversation_id) DO UPDATE SET last_seq = conversation_sequences.last_seq + 1
		RETURNING last_seq
	`, convID).Scan(&seq); err != nil {
		return uuid.Nil, err
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO messages (conversation_id, type, text, seq, created_at)
		VALUES ($1, 'system', 'Alice Morgan created the group "Project Aurora"', $2, $3)
	`, convID, seq, now); err != nil {
		return uuid.Nil, err
	}

	if err := add("alice", "Welcome to Project Aurora! 🚀 Goal: ship the beta by end of month."); err != nil {
		return uuid.Nil, err
	}
	if err := add("bob", "Realtime pipeline is ready — presence, typing and receipts all tested."); err != nil {
		return uuid.Nil, err
	}
	if err := add("charlie", "Onboarding flow drafted. Feedback welcome before Thursday."); err != nil {
		return uuid.Nil, err
	}
	if err := add("david", "I'll take the performance pass on the message list this week."); err != nil {
		return uuid.Nil, err
	}
	if err := add("alice", "Amazing pace everyone. Demo dry-run Friday @bob?"); err != nil {
		return uuid.Nil, err
	}

	// A reaction from David.
	var lastMsgID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM messages WHERE conversation_id = $1 ORDER BY seq DESC LIMIT 1`, convID).Scan(&lastMsgID); err != nil {
		return uuid.Nil, err
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO message_reactions (message_id, user_id, emoji) VALUES ($1,$2,'🔥') ON CONFLICT DO NOTHING
	`, lastMsgID, ids["david"]); err != nil {
		return uuid.Nil, err
	}

	return convID, nil
}

func seedNotifications(ctx context.Context, pool *pgxpool.Pool, ids map[string]uuid.UUID) error {
	seed := []struct {
		user, actor, typ, title, body string
	}{
		{"alice", "charlie", "contact_request", "New contact request", "Charlie Alvarez wants to connect."},
		{"bob", "alice", "contact_accepted", "Contact added", "Alice Morgan accepted your request."},
		{"alice", "bob", "new_message", "Bob Chen", "Perfect. Also — welcome aboard Veylo 🎉"},
	}
	for _, s := range seed {
		if _, err := pool.Exec(ctx, `
			INSERT INTO notifications (user_id, type, title, body, actor_id)
			VALUES ($1, $2, $3, $4, $5)
		`, ids[s.user], s.typ, s.title, s.body, ids[s.actor]); err != nil {
			return err
		}
	}
	return nil
}

// seedExtras adds a pending contact request and a short David DM so every
// settings/contact surface has realistic content.
func seedExtras(ctx context.Context, pool *pgxpool.Pool, ids map[string]uuid.UUID) error {
	// Pending incoming request for David from Charlie.
	if _, err := pool.Exec(ctx, `
		INSERT INTO contact_requests (requester_id, addressee_id, message)
		VALUES ($1, $2, 'Hey David! We met at the meetup — would love to stay in touch.')
		ON CONFLICT (requester_id, addressee_id) DO NOTHING
	`, ids["charlie"], ids["david"]); err != nil {
		return err
	}
	// David also has a pending outgoing request to nobody yet; fine.

	// Short Alice <-> David DM: one read message + one unread for Alice.
	dmAD, err := ensureDirect(ctx, pool, ids["alice"], ids["david"])
	if err != nil {
		return err
	}
	var adMsgs int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM messages WHERE conversation_id = $1`, dmAD).Scan(&adMsgs); err != nil {
		return err
	}
	if adMsgs > 0 {
		return nil
	}
	now := time.Now().UTC().Add(-45 * time.Minute)
	if _, _, err := addMessage(ctx, pool, dmAD, ids["david"], "text",
		"Alice! The launch checklist link stopped working — got a spare?", now, nil); err != nil {
		return err
	}
	if _, _, err := addMessage(ctx, pool, dmAD, ids["alice"], "text",
		"Sending it over in a sec — congrats on the release 🎉", now.Add(3*time.Minute), nil); err != nil {
		return err
	}
	if _, _, err := addMessage(ctx, pool, dmAD, ids["david"], "text",
		"Thanks! Also — coffee when you're back in town? ☕", now.Add(6*time.Minute), nil); err != nil {
		return err
	}
	// Alice read up to seq 2; David's message (seq 3) stays unread for Alice.
	if _, err := pool.Exec(ctx, `
		UPDATE message_read_cursors SET delivered_seq = 2, read_seq = 2
		WHERE conversation_id = $1 AND user_id = $2
	`, dmAD, ids["alice"]); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `
		UPDATE message_read_cursors SET delivered_seq = 3, read_seq = 3
		WHERE conversation_id = $1 AND user_id = $2
	`, dmAD, ids["david"]); err != nil {
		return err
	}
	return nil
}

// generateSamplePNG produces a small branded image for demo attachments.
func generateSamplePNG() ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, 640, 360))
	// Vertical gradient violet -> indigo.
	for y := 0; y < 360; y++ {
		t := float64(y) / 359
		r := uint8(0x5b - t*0x2a)
		g := uint8(0x4f - t*0x1a)
		b := uint8(0xc4 - t*0x30)
		for x := 0; x < 640; x++ {
			img.Set(x, y, color.RGBA{r, g, b, 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
