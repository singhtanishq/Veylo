package tests

import (
	"net/http"
	"testing"
)

func TestDiscoverySearch(t *testing.T) {
	resetDB(t)
	alice := registerNewUser(t, "alice")
	registerNewUser(t, "bob")
	registerNewUser(t, "charlie")

	// Username search with @ prefix.
	results := alice.C.mustJSON(t, "GET", "/v1/users/search?q=@bob", nil, http.StatusOK)
	items := rawList(t, results)
	if len(items) != 1 || items[0]["username"] != "bob" {
		t.Fatalf("username search: %v", items)
	}

	// Fuzzy display-name search.
	results = alice.C.mustJSON(t, "GET", "/v1/users/search?q=Charlie", nil, http.StatusOK)
	items = rawList(t, results)
	if len(items) != 1 {
		t.Fatalf("display search: %v", items)
	}

	// Email search works when discoverable (default on).
	results = alice.C.mustJSON(t, "GET", "/v1/users/search?q=bob@example.test", nil, http.StatusOK)
	items = rawList(t, results)
	if len(items) != 1 {
		t.Fatalf("email search: %v", items)
	}
	// Email value itself must not leak.
	if email, ok := items[0]["email"]; ok && email != nil {
		t.Fatalf("email leaked in public search result")
	}
}

func TestDiscoveryPrivacy(t *testing.T) {
	resetDB(t)
	alice := registerNewUser(t, "alice")
	bob := registerNewUser(t, "bob")

	// Bob disables email discovery.
	bob.C.mustJSON(t, "PATCH", "/v1/settings/privacy", map[string]any{"discoverable_by_email": false}, http.StatusOK)

	// Email search now misses.
	results := alice.C.mustJSON(t, "GET", "/v1/users/search?q="+bob.Email, nil, http.StatusOK)
	if len(rawList(t, results)) != 0 {
		t.Fatalf("email search should be private")
	}

	// Username search still finds him.
	results = alice.C.mustJSON(t, "GET", "/v1/users/search?q=@bob", nil, http.StatusOK)
	if len(rawList(t, results)) != 1 {
		t.Fatalf("username search should still work")
	}

	// Unverified phone numbers are never discoverable.
	bob.C.mustJSON(t, "POST", "/v1/users/me/phone", map[string]any{"phone": "+919876543210"}, http.StatusOK)
	results = alice.C.mustJSON(t, "GET", "/v1/users/search?q=%2B919876543210", nil, http.StatusOK)
	if len(rawList(t, results)) != 0 {
		t.Fatalf("unverified phone must not be discoverable")
	}

	// Presence hidden when visibility = nobody.
	bob.C.mustJSON(t, "PATCH", "/v1/settings/privacy", map[string]any{
		"online_visibility": "nobody", "last_seen_visibility": "nobody",
	}, http.StatusOK)
	profile := asMap(t, alice.C.mustJSON(t, "GET", "/v1/users/"+bob.ID, nil, http.StatusOK))
	if online, _ := profile["online"].(bool); online {
		t.Fatalf("presence should be hidden")
	}
	if profile["last_seen_at"] != nil {
		t.Fatalf("last_seen should be hidden")
	}
}

func TestUsernameAvailability(t *testing.T) {
	resetDB(t)
	u := registerNewUser(t, "takenuser")

	// Availability checks are authenticated (anti-enumeration).
	newClient().mustJSON(t, "GET", "/v1/users/username/availability?username=freshname", nil, http.StatusUnauthorized)

	av := asMap(t, u.C.mustJSON(t, "GET", "/v1/users/username/availability?username=takenuser", nil, http.StatusOK))
	if av["available"] != false {
		t.Fatalf("taken username reported available")
	}
	av = asMap(t, u.C.mustJSON(t, "GET", "/v1/users/username/availability?username=freshname", nil, http.StatusOK))
	if av["available"] != true {
		t.Fatalf("fresh username reported taken")
	}
}

func TestProfileUpdate(t *testing.T) {
	resetDB(t)
	u := registerNewUser(t, "editor")

	u.C.mustJSON(t, "PATCH", "/v1/users/me", map[string]any{
		"display_name": "Edited Name", "about": "Hello, I am edited.",
	}, http.StatusOK)

	me := asMap(t, u.C.mustJSON(t, "GET", "/v1/users/me", nil, http.StatusOK))
	if me["display_name"] != "Edited Name" {
		t.Fatalf("display_name not updated: %v", me)
	}
	if me["about"] != "Hello, I am edited." {
		t.Fatalf("about not updated: %v", me)
	}

	// Username update with uniqueness check.
	u.C.mustJSON(t, "PATCH", "/v1/users/me", map[string]any{"username": "editor2"}, http.StatusOK)

	// Duplicate username rejected.
	registerNewUser(t, "other")
	u.C.mustJSON(t, "PATCH", "/v1/users/me", map[string]any{"username": "other"}, http.StatusConflict)

	// Invalid username rejected.
	u.C.mustJSON(t, "PATCH", "/v1/users/me", map[string]any{"username": "BAD NAME!"}, http.StatusUnprocessableEntity)
}
