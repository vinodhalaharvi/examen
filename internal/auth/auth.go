// Package auth implements a minimal Google OAuth login flow with HMAC-signed
// cookie sessions. No interfaces, no third-party session stores — the cookie
// itself carries the (small) session payload, signed so the user can't forge it.
//
// Configuration via environment variables:
//
//	GOOGLE_OAUTH_CLIENT_ID      OAuth client ID from Google Cloud Console
//	GOOGLE_OAUTH_CLIENT_SECRET  OAuth client secret (never logged)
//	GOOGLE_OAUTH_REDIRECT_URL   Callback URL — must match Google's config
//	EXAMEN_COOKIE_KEY           Hex-encoded HMAC key (>= 32 bytes recommended)
//
// Generate a cookie key with:
//
//	openssl rand -hex 32
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	cookieName  = "examen_session"
	stateCookie = "examen_oauth_state"
	sessionTTL  = 7 * 24 * time.Hour // 7 days
	stateTTL    = 10 * time.Minute   // OAuth state cookie lifetime
)

// Config holds the OAuth + cookie configuration.
type Config struct {
	OAuth     *oauth2.Config
	CookieKey []byte // HMAC key for signing session cookies
	Insecure  bool   // if true, cookies don't require HTTPS (dev only)
}

// LoadConfig reads configuration from the environment. Returns an error if
// any required variable is missing.
func LoadConfig() (*Config, error) {
	clientID := os.Getenv("GOOGLE_OAUTH_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET")
	redirectURL := os.Getenv("GOOGLE_OAUTH_REDIRECT_URL")
	cookieKeyHex := os.Getenv("EXAMEN_COOKIE_KEY")

	if clientID == "" || clientSecret == "" || redirectURL == "" {
		return nil, fmt.Errorf("missing one of GOOGLE_OAUTH_CLIENT_ID, GOOGLE_OAUTH_CLIENT_SECRET, GOOGLE_OAUTH_REDIRECT_URL")
	}
	if cookieKeyHex == "" {
		return nil, fmt.Errorf("EXAMEN_COOKIE_KEY not set; generate with: openssl rand -hex 32")
	}
	cookieKey, err := hex.DecodeString(cookieKeyHex)
	if err != nil {
		return nil, fmt.Errorf("EXAMEN_COOKIE_KEY must be hex-encoded: %w", err)
	}
	if len(cookieKey) < 32 {
		return nil, fmt.Errorf("EXAMEN_COOKIE_KEY too short (need >= 32 bytes); generate with: openssl rand -hex 32")
	}

	insecure := os.Getenv("EXAMEN_INSECURE_COOKIES") == "1"

	return &Config{
		OAuth: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     google.Endpoint,
			Scopes: []string{
				"https://www.googleapis.com/auth/userinfo.email",
				"https://www.googleapis.com/auth/userinfo.profile",
			},
		},
		CookieKey: cookieKey,
		Insecure:  insecure,
	}, nil
}

// =============================================================================
// SESSION
// =============================================================================

// Session is what we encode into the signed cookie. Keep it small.
type Session struct {
	StudentID string    `json:"sid"`     // canonical user id (we use email)
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Picture   string    `json:"picture"`
	IssuedAt  time.Time `json:"iat"`
	ExpiresAt time.Time `json:"exp"`
}

// Encode serializes and HMAC-signs a Session into a cookie value.
func (c *Config) Encode(s Session) string {
	payload, _ := json.Marshal(s)
	encoded := base64.RawURLEncoding.EncodeToString(payload)

	mac := hmac.New(sha256.New, c.CookieKey)
	mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return encoded + "." + sig
}

// Decode verifies the HMAC signature and parses the session.
// Returns an error if signature is invalid, payload is malformed, or expired.
func (c *Config) Decode(cookieValue string) (Session, error) {
	parts := strings.SplitN(cookieValue, ".", 2)
	if len(parts) != 2 {
		return Session{}, fmt.Errorf("malformed cookie")
	}
	encoded, sig := parts[0], parts[1]

	mac := hmac.New(sha256.New, c.CookieKey)
	mac.Write([]byte(encoded))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return Session{}, fmt.Errorf("invalid signature")
	}

	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return Session{}, err
	}
	var s Session
	if err := json.Unmarshal(payload, &s); err != nil {
		return Session{}, err
	}
	if time.Now().After(s.ExpiresAt) {
		return Session{}, fmt.Errorf("session expired")
	}
	return s, nil
}

// SetSessionCookie writes a signed session cookie on the response.
func (c *Config) SetSessionCookie(w http.ResponseWriter, s Session) {
	if s.IssuedAt.IsZero() {
		s.IssuedAt = time.Now()
	}
	if s.ExpiresAt.IsZero() {
		s.ExpiresAt = s.IssuedAt.Add(sessionTTL)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    c.Encode(s),
		Path:     "/",
		HttpOnly: true,
		Secure:   !c.Insecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

// ClearSessionCookie removes the session cookie.
func (c *Config) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   !c.Insecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// SessionFrom reads and verifies the session cookie. Returns ok=false if the
// request has no valid session.
func (c *Config) SessionFrom(r *http.Request) (Session, bool) {
	ck, err := r.Cookie(cookieName)
	if err != nil {
		return Session{}, false
	}
	s, err := c.Decode(ck.Value)
	if err != nil {
		return Session{}, false
	}
	return s, true
}

// =============================================================================
// HANDLERS
// =============================================================================

// Login redirects the user to Google's consent page. Generates a random state
// token, stores it in a short-lived cookie, includes it in the redirect URL —
// on callback we verify the two match (CSRF defense).
func (c *Config) Login(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken()
	if err != nil {
		http.Error(w, "failed to generate state", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   !c.Insecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(stateTTL.Seconds()),
	})
	url := c.OAuth.AuthCodeURL(state, oauth2.AccessTypeOnline)
	http.Redirect(w, r, url, http.StatusFound)
}

// Callback handles the redirect back from Google. Exchanges the auth code for
// an access token, fetches the user's profile, sets a session cookie, and
// redirects to /.
func (c *Config) Callback(w http.ResponseWriter, r *http.Request) {
	// Verify state matches the one we issued
	stateCookieVal, err := r.Cookie(stateCookie)
	if err != nil {
		http.Error(w, "missing oauth state cookie", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("state") != stateCookieVal.Value {
		http.Error(w, "oauth state mismatch", http.StatusBadRequest)
		return
	}
	// Clear state cookie regardless of outcome
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   !c.Insecure,
		MaxAge:   -1,
	})

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		http.Error(w, "oauth error: "+errParam, http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	// Exchange the code for a token
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	tok, err := c.OAuth.Exchange(ctx, code)
	if err != nil {
		http.Error(w, "token exchange failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Fetch the user's profile from Google's userinfo endpoint
	user, err := fetchUserInfo(ctx, c.OAuth.Client(ctx, tok))
	if err != nil {
		http.Error(w, "failed to fetch profile: "+err.Error(), http.StatusBadGateway)
		return
	}
	if !user.EmailVerified {
		http.Error(w, "email not verified by Google", http.StatusForbidden)
		return
	}

	// Mint a session
	c.SetSessionCookie(w, Session{
		StudentID: user.Email,
		Email:     user.Email,
		Name:      user.Name,
		Picture:   user.Picture,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

// Logout clears the session cookie and redirects to /.
func (c *Config) Logout(w http.ResponseWriter, r *http.Request) {
	c.ClearSessionCookie(w)
	http.Redirect(w, r, "/", http.StatusFound)
}

// Me returns the current session as JSON, or 401 if not signed in.
// The UI uses this to populate the header.
func (c *Config) Me(w http.ResponseWriter, r *http.Request) {
	sess, ok := c.SessionFrom(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"authenticated": false})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"authenticated": true,
		"student_id":    sess.StudentID,
		"email":         sess.Email,
		"name":          sess.Name,
		"picture":       sess.Picture,
	})
}

// =============================================================================
// MIDDLEWARE
// =============================================================================

// Require wraps a handler to require authentication. Unauthenticated requests
// to API paths get 401 JSON; everything else gets redirected to /login.
func (c *Config) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := c.SessionFrom(r); ok {
			next.ServeHTTP(w, r)
			return
		}
		// API requests get JSON 401; humans get redirected
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not authenticated"})
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	})
}

// =============================================================================
// HELPERS
// =============================================================================

type googleUserInfo struct {
	Sub           string `json:"sub"` // Google's stable user id
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

// fetchUserInfo calls Google's userinfo endpoint with the OAuth client.
func fetchUserInfo(ctx context.Context, client *http.Client) (googleUserInfo, error) {
	const url = "https://www.googleapis.com/oauth2/v3/userinfo"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return googleUserInfo{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return googleUserInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return googleUserInfo{}, fmt.Errorf("userinfo returned %d", resp.StatusCode)
	}
	var u googleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return googleUserInfo{}, err
	}
	return u, nil
}

// randomToken returns a 32-byte URL-safe random string for OAuth state.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
