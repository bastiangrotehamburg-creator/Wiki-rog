// Package auth stellt das gruppenbasierte Zugriffsmodell mit Login bereit:
//   - Nutzer/Passwort (PBKDF2-Hash, Standardbibliothek)
//   - Gruppen -> Collection-Zuordnung
//   - signierte Session-Cookies (HMAC-SHA256)
//
// Ist keine Access-Konfiguration vorhanden, ist Auth deaktiviert und die WebUI
// läuft offen (eine Collection, kein Login).
package auth

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	pbkdf2Iter   = 210000
	pbkdf2KeyLen = 32
	cookieName   = "wr_session"
)

// Group ordnet einer Gruppe eine Vektor-Collection zu. Optional kann ein
// eigenes BookStack-Token hinterlegt werden, damit der Admin-Button diese
// Gruppe direkt aus der WebUI (neu) einlesen kann.
type Group struct {
	Collection  string `json:"collection"`
	TokenID     string `json:"bookstack_token_id,omitempty"`
	TokenSecret string `json:"bookstack_token_secret,omitempty"`
}

// User ist ein Login-Konto mit Passwort-Hash und Gruppenzugehörigkeit.
type User struct {
	Username     string   `json:"username"`
	PasswordHash string   `json:"password_hash"`
	Groups       []string `json:"groups"`
	Admin        bool     `json:"admin"`
}

// GroupTarget beschreibt eine (neu) einlesbare Gruppe inkl. Token.
type GroupTarget struct {
	Group       string
	Collection  string
	TokenID     string
	TokenSecret string
}

type accessFile struct {
	Groups map[string]Group `json:"groups"`
	Users  []User           `json:"users"`
}

// Session ist der Inhalt eines gültigen Cookies.
type Session struct {
	Username string   `json:"u"`
	Groups   []string `json:"g"`
	Admin    bool     `json:"a"`
	Exp      int64    `json:"e"`
}

// Service kapselt Access-Konfiguration und Session-Handling.
type Service struct {
	groups map[string]Group
	users  map[string]User
	secret []byte
	ttl    time.Duration
	secure bool
}

// Load liest die Access-Konfiguration. Fehlt die Datei, wird (nil, nil)
// zurückgegeben -> Auth ist deaktiviert.
func Load(path, sessionSecret string, ttlHours int, secure bool) (*Service, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var af accessFile
	if err := json.Unmarshal(data, &af); err != nil {
		return nil, fmt.Errorf("Access-Konfiguration %s: %w", path, err)
	}
	if len(af.Users) == 0 {
		return nil, fmt.Errorf("Access-Konfiguration %s enthält keine Nutzer", path)
	}

	secret := []byte(sessionSecret)
	if len(secret) == 0 {
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, err
		}
		fmt.Fprintln(os.Stderr, "WARNUNG: SESSION_SECRET nicht gesetzt – zufälliges Secret erzeugt. "+
			"Sessions überstehen keinen Neustart und funktionieren nicht mit mehreren Replicas.")
	}

	users := make(map[string]User, len(af.Users))
	for _, u := range af.Users {
		users[strings.ToLower(u.Username)] = u
	}
	if ttlHours <= 0 {
		ttlHours = 12
	}
	return &Service{
		groups: af.Groups,
		users:  users,
		secret: secret,
		ttl:    time.Duration(ttlHours) * time.Hour,
		secure: secure,
	}, nil
}

// Authenticate prüft Nutzer/Passwort.
func (s *Service) Authenticate(username, password string) (User, bool) {
	u, ok := s.users[strings.ToLower(strings.TrimSpace(username))]
	if !ok {
		// Dummy-Verify gegen Timing-Unterschiede
		_ = VerifyPassword("pbkdf2_sha256$210000$AAAA$AAAA", password)
		return User{}, false
	}
	if !VerifyPassword(u.PasswordHash, password) {
		return User{}, false
	}
	return u, true
}

// CollectionsFor liefert die (deduplizierten) Collections der genannten Gruppen.
func (s *Service) CollectionsFor(groups []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, g := range groups {
		grp, ok := s.groups[g]
		if !ok || grp.Collection == "" || seen[grp.Collection] {
			continue
		}
		seen[grp.Collection] = true
		out = append(out, grp.Collection)
	}
	return out
}

// GroupTargets liefert alle Gruppen mit hinterlegtem BookStack-Token
// (für die Neuindizierung per Admin-Button).
func (s *Service) GroupTargets() []GroupTarget {
	var out []GroupTarget
	for name, g := range s.groups {
		if g.TokenID == "" || g.Collection == "" {
			continue
		}
		out = append(out, GroupTarget{
			Group:       name,
			Collection:  g.Collection,
			TokenID:     g.TokenID,
			TokenSecret: g.TokenSecret,
		})
	}
	return out
}

// --- Sessions (signiertes Cookie) -----------------------------------------

func (s *Service) sign(payload []byte) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// IssueCookie erzeugt ein signiertes Session-Cookie für den Nutzer.
func (s *Service) IssueCookie(u User) *http.Cookie {
	sess := Session{Username: u.Username, Groups: u.Groups, Admin: u.Admin, Exp: time.Now().Add(s.ttl).Unix()}
	payload, _ := json.Marshal(sess)
	enc := base64.RawURLEncoding.EncodeToString(payload)
	value := enc + "." + s.sign([]byte(enc))
	return &http.Cookie{
		Name:     cookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(s.ttl),
	}
}

// ClearCookie liefert ein Cookie, das die Session löscht.
func (s *Service) ClearCookie() *http.Cookie {
	return &http.Cookie{
		Name: cookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: s.secure, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	}
}

// SessionFrom validiert das Cookie der Anfrage.
func (s *Service) SessionFrom(r *http.Request) (*Session, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil, false
	}
	enc, sig, ok := strings.Cut(c.Value, ".")
	if !ok {
		return nil, false
	}
	expected := s.sign([]byte(enc))
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return nil, false
	}
	var sess Session
	if err := json.Unmarshal(payload, &sess); err != nil {
		return nil, false
	}
	if time.Now().Unix() > sess.Exp {
		return nil, false
	}
	return &sess, true
}

// --- Passwort-Hashing (PBKDF2-HMAC-SHA256) ---------------------------------

// HashPassword erzeugt einen Hash im Format pbkdf2_sha256$iter$salt$hash.
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	dk, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iter, pbkdf2KeyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2_sha256$%d$%s$%s",
		pbkdf2Iter,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(dk),
	), nil
}

// VerifyPassword prüft ein Passwort gegen einen gespeicherten Hash.
func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2_sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter <= 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}
