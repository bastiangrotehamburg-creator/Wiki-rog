package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHashVerify(t *testing.T) {
	h, err := HashPassword("geheim123")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(h, "geheim123") {
		t.Fatal("korrektes Passwort wurde abgelehnt")
	}
	if VerifyPassword(h, "falsch") {
		t.Fatal("falsches Passwort wurde akzeptiert")
	}
	if VerifyPassword("kaputt", "geheim123") {
		t.Fatal("ungültiger Hash wurde akzeptiert")
	}
}

func testService(t *testing.T) *Service {
	t.Helper()
	h, _ := HashPassword("pw")
	svc := &Service{
		groups: map[string]Group{
			"all": {Collection: "bookstack-all"},
			"hr":  {Collection: "bookstack-hr"},
			"it":  {Collection: "bookstack-it"},
		},
		users: map[string]User{
			"alice": {Username: "alice", PasswordHash: h, Groups: []string{"hr", "all"}},
		},
		secret: []byte("test-secret-0123456789"),
		ttl:    3600 * 1e9, // 1h
	}
	return svc
}

func TestAuthenticateAndCollections(t *testing.T) {
	svc := testService(t)
	u, ok := svc.Authenticate("Alice", "pw") // Groß/Klein egal
	if !ok {
		t.Fatal("Login sollte gelingen")
	}
	if _, bad := svc.Authenticate("alice", "wrong"); bad {
		t.Fatal("falsches Passwort sollte fehlschlagen")
	}
	if _, bad := svc.Authenticate("mallory", "pw"); bad {
		t.Fatal("unbekannter Nutzer sollte fehlschlagen")
	}
	cols := svc.CollectionsFor(u.Groups)
	if len(cols) != 2 {
		t.Fatalf("erwartet 2 Collections, bekam %v", cols)
	}
}

func TestSessionRoundTrip(t *testing.T) {
	svc := testService(t)
	u := svc.users["alice"]
	cookie := svc.IssueCookie(u)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(cookie)
	sess, ok := svc.SessionFrom(r)
	if !ok || sess.Username != "alice" {
		t.Fatalf("Session sollte gültig sein, bekam ok=%v", ok)
	}

	// Manipuliertes Cookie muss abgelehnt werden.
	bad := httptest.NewRequest(http.MethodGet, "/", nil)
	bad.AddCookie(&http.Cookie{Name: cookieName, Value: cookie.Value + "x"})
	if _, ok := svc.SessionFrom(bad); ok {
		t.Fatal("manipuliertes Cookie wurde akzeptiert")
	}
}
