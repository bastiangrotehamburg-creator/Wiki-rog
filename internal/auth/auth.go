// Package auth stellt das gruppenbasierte Zugriffsmodell mit Login bereit:
//   - Nutzer/Passwort (PBKDF2-Hash, Standardbibliothek)
//   - Gruppen -> Collection-Zuordnung (aus access.json, enthält Tokens)
//   - Nutzer zur Laufzeit verwaltbar (persistiert in users.json)
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
	"sort"
	"strconv"
	"strings"
	"sync"
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

// PublicUser ist die Darstellung ohne Passwort-Hash (für die Nutzerliste).
type PublicUser struct {
	Username string   `json:"username"`
	Groups   []string `json:"groups"`
	Admin    bool     `json:"admin"`
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

type usersFile struct {
	Users []User `json:"users"`
}

// Session ist der Inhalt eines gültigen Cookies.
type Session struct {
	Username string   `json:"u"`
	Groups   []string `json:"g"`
	Admin    bool     `json:"a"`
	Exp      int64    `json:"e"`
}

// Service kapselt Access-Konfiguration, Nutzerverwaltung und Session-Handling.
type Service struct {
	groups    map[string]Group
	secret    []byte
	ttl       time.Duration
	secure    bool
	usersPath string

	mu    sync.RWMutex
	users map[string]User // key = lowercased username
}

// Load liest die Access-Konfiguration (Gruppen + Seed-Nutzer). Fehlt die Datei,
// wird (nil, nil) zurückgegeben -> Auth ist deaktiviert. Nutzer werden aus
// usersPath geladen, sofern vorhanden; sonst aus access.json übernommen und dort
// erstmalig gespeichert.
func Load(accessPath, usersPath, sessionSecret string, ttlHours int, secure bool) (*Service, error) {
	data, err := os.ReadFile(accessPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var af accessFile
	if err := json.Unmarshal(data, &af); err != nil {
		return nil, fmt.Errorf("Access-Konfiguration %s: %w", accessPath, err)
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
	if ttlHours <= 0 {
		ttlHours = 12
	}

	s := &Service{
		groups:    af.Groups,
		secret:    secret,
		ttl:       time.Duration(ttlHours) * time.Hour,
		secure:    secure,
		usersPath: usersPath,
		users:     map[string]User{},
	}

	// Nutzer laden: users.json hat Vorrang; sonst aus access.json übernehmen.
	loadedFromStore := false
	if usersPath != "" {
		if raw, err := os.ReadFile(usersPath); err == nil {
			var uf usersFile
			if err := json.Unmarshal(raw, &uf); err != nil {
				return nil, fmt.Errorf("Nutzerdatei %s: %w", usersPath, err)
			}
			for _, u := range uf.Users {
				s.users[strings.ToLower(u.Username)] = u
			}
			loadedFromStore = true
		}
	}
	if !loadedFromStore {
		for _, u := range af.Users {
			s.users[strings.ToLower(u.Username)] = u
		}
		// Seed dauerhaft ablegen (best effort).
		if err := s.persistUsers(); err != nil {
			fmt.Fprintf(os.Stderr, "WARNUNG: Nutzerdatei konnte nicht geschrieben werden (%v) – "+
				"neue Nutzer überleben keinen Neustart.\n", err)
		}
	}

	if len(s.users) == 0 {
		return nil, fmt.Errorf("keine Nutzer vorhanden – lege mindestens einen Admin in %s an", accessPath)
	}
	return s, nil
}

// persistUsers schreibt die Nutzer atomar nach usersPath (Aufrufer hält Lock,
// oder es ist der Seed-Fall vor Nebenläufigkeit).
func (s *Service) persistUsers() error {
	if s.usersPath == "" {
		return nil
	}
	list := make([]User, 0, len(s.users))
	for _, u := range s.users {
		list = append(list, u)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Username < list[j].Username })
	raw, err := json.MarshalIndent(usersFile{Users: list}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.usersPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.usersPath)
}

// Authenticate prüft Nutzer/Passwort.
func (s *Service) Authenticate(username, password string) (User, bool) {
	s.mu.RLock()
	u, ok := s.users[strings.ToLower(strings.TrimSpace(username))]
	s.mu.RUnlock()
	if !ok {
		_ = VerifyPassword("pbkdf2_sha256$210000$AAAA$AAAA", password) // Timing-Angleich
		return User{}, false
	}
	if !VerifyPassword(u.PasswordHash, password) {
		return User{}, false
	}
	return u, true
}

// ListUsers liefert alle Nutzer ohne Passwort-Hash.
func (s *Service) ListUsers() []PublicUser {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PublicUser, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, PublicUser{Username: u.Username, Groups: u.Groups, Admin: u.Admin})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out
}

// UpsertUser legt einen Nutzer an oder ändert ihn. Leeres Passwort bei einem
// bestehenden Nutzer behält das alte Passwort.
func (s *Service) UpsertUser(username, password string, groups []string, admin bool) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return fmt.Errorf("Benutzername darf nicht leer sein")
	}
	key := strings.ToLower(username)

	// nur bekannte Gruppen zulassen
	clean := make([]string, 0, len(groups))
	for _, g := range groups {
		g = strings.TrimSpace(g)
		if _, ok := s.groups[g]; ok {
			clean = append(clean, g)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	existing, exists := s.users[key]
	hash := ""
	if exists {
		hash = existing.PasswordHash
	}
	if strings.TrimSpace(password) != "" {
		h, err := HashPassword(password)
		if err != nil {
			return err
		}
		hash = h
	} else if !exists {
		return fmt.Errorf("neuer Nutzer braucht ein Passwort")
	}

	// Verhindern, dass der letzte Admin seine Rechte verliert.
	if exists && existing.Admin && !admin && s.countAdminsLocked(key) == 0 {
		return fmt.Errorf("mindestens ein Admin muss erhalten bleiben")
	}

	s.users[key] = User{Username: username, PasswordHash: hash, Groups: clean, Admin: admin}
	return s.persistUsers()
}

// DeleteUser entfernt einen Nutzer (nie den letzten Admin).
func (s *Service) DeleteUser(username string) error {
	key := strings.ToLower(strings.TrimSpace(username))
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[key]
	if !ok {
		return fmt.Errorf("Nutzer nicht gefunden")
	}
	if u.Admin && s.countAdminsLocked(key) == 0 {
		return fmt.Errorf("der letzte Admin kann nicht gelöscht werden")
	}
	delete(s.users, key)
	return s.persistUsers()
}

// countAdminsLocked zählt Admins, ignoriert dabei den Nutzer exceptKey.
func (s *Service) countAdminsLocked(exceptKey string) int {
	n := 0
	for k, u := range s.users {
		if k == exceptKey {
			continue
		}
		if u.Admin {
			n++
		}
	}
	return n
}

// GroupNames liefert die verfügbaren Gruppennamen (für die Nutzerverwaltung).
func (s *Service) GroupNames() []string {
	out := make([]string, 0, len(s.groups))
	for name := range s.groups {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
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
