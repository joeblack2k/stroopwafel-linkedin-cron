package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"linkedin-cron/internal/config"
)

const (
	uiSessionCookieName = "lc_session"
	uiSessionTTL        = 14 * 24 * time.Hour
)

func UIAuthMiddleware(cfg config.Config, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if sessionUser, ok := validateUISessionCookie(r, cfg.BasicAuthUser, cfg.BasicAuthPass, time.Now().UTC()); ok {
				ctx := context.WithValue(r.Context(), contextKeyAuthMethod, "session")
				ctx = context.WithValue(ctx, contextKeyAuthUser, sessionUser)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if authUser, ok := basicAuthUserIfValid(r, cfg.BasicAuthUser, cfg.BasicAuthPass); ok {
				ctx := context.WithValue(r.Context(), contextKeyAuthMethod, "basic")
				ctx = context.WithValue(ctx, contextKeyAuthUser, authUser)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			logger.LogAttrs(
				r.Context(),
				slog.LevelWarn,
				"ui auth failed",
				slog.String("component", "http"),
				slog.String("path", r.URL.Path),
			)
			http.Redirect(w, r, buildLoginRedirectURL(r), http.StatusSeeOther)
		})
	}
}

func basicAuthUserIfValid(r *http.Request, expectedUser, expectedPass string) (string, bool) {
	user, pass, ok := r.BasicAuth()
	if !ok {
		return "", false
	}
	if subtle.ConstantTimeCompare([]byte(user), []byte(expectedUser)) != 1 {
		return "", false
	}
	if subtle.ConstantTimeCompare([]byte(pass), []byte(expectedPass)) != 1 {
		return "", false
	}
	return user, true
}

func issueUISessionToken(username, password string, now time.Time) (string, time.Time, error) {
	nonce, err := randomHex(12)
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := now.UTC().Add(uiSessionTTL)
	payload := username + "|" + strconv.FormatInt(expiresAt.Unix(), 10) + "|" + nonce

	sig := signUISessionPayload(payload, username, password)
	encodedPayload := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return encodedPayload + "." + sig, expiresAt, nil
}

func validateUISessionCookie(r *http.Request, expectedUser, expectedPass string, now time.Time) (string, bool) {
	cookie, err := r.Cookie(uiSessionCookieName)
	if err != nil {
		return "", false
	}
	return parseAndValidateUISessionToken(cookie.Value, expectedUser, expectedPass, now)
}

func parseAndValidateUISessionToken(token, expectedUser, expectedPass string, now time.Time) (string, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}

	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", false
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	payload := string(payloadBytes)

	expectedSig := signUISessionPayload(payload, expectedUser, expectedPass)
	if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(expectedSig)) != 1 {
		return "", false
	}

	payloadParts := strings.Split(payload, "|")
	if len(payloadParts) != 3 {
		return "", false
	}

	username := strings.TrimSpace(payloadParts[0])
	if subtle.ConstantTimeCompare([]byte(username), []byte(expectedUser)) != 1 {
		return "", false
	}

	expUnix, err := strconv.ParseInt(strings.TrimSpace(payloadParts[1]), 10, 64)
	if err != nil {
		return "", false
	}
	if now.UTC().Unix() > expUnix {
		return "", false
	}

	if strings.TrimSpace(payloadParts[2]) == "" {
		return "", false
	}

	return username, true
}

func signUISessionPayload(payload, username, password string) string {
	mac := hmac.New(sha256.New, []byte("linkedin-cron-session|"+username+"|"+password))
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func randomHex(byteLength int) (string, error) {
	if byteLength <= 0 {
		byteLength = 12
	}
	buffer := make([]byte, byteLength)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

func setUISessionCookie(w http.ResponseWriter, token string, expiresAt time.Time, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     uiSessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

func clearUISessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     uiSessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

func buildLoginRedirectURL(r *http.Request) string {
	next := r.URL.Path
	if rawQuery := strings.TrimSpace(r.URL.RawQuery); rawQuery != "" {
		next += "?" + rawQuery
	}
	next = safeReturnPath(next)
	if next == "" {
		next = "/calendar"
	}
	return "/login?next=" + url.QueryEscape(next)
}
