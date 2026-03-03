package handlers

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"
)

type LoginPageData struct {
	Title   string
	Next    string
	Error   string
	Message string
	User    string
}

func (a *App) LoginPage(w http.ResponseWriter, r *http.Request) {
	next := safeReturnPath(r.URL.Query().Get("next"))
	if next == "" {
		next = "/calendar"
	}

	if _, ok := validateUISessionCookie(r, a.Config.BasicAuthUser, a.Config.BasicAuthPass, time.Now().UTC()); ok {
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	if _, ok := basicAuthUserIfValid(r, a.Config.BasicAuthUser, a.Config.BasicAuthPass); ok {
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}

	data := LoginPageData{
		Title:   "Login",
		Next:    next,
		Error:   strings.TrimSpace(r.URL.Query().Get("err")),
		Message: strings.TrimSpace(r.URL.Query().Get("msg")),
		User:    strings.TrimSpace(a.Config.BasicAuthUser),
	}
	if err := a.Renderer.Render(w, "login.html", data); err != nil {
		http.Error(w, "failed to render login", http.StatusInternalServerError)
	}
}

func (a *App) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/login?err=invalid+form+body", http.StatusSeeOther)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := strings.TrimSpace(r.FormValue("password"))
	next := safeReturnPath(r.FormValue("next"))
	if next == "" {
		next = "/calendar"
	}

	if subtle.ConstantTimeCompare([]byte(username), []byte(a.Config.BasicAuthUser)) != 1 || subtle.ConstantTimeCompare([]byte(password), []byte(a.Config.BasicAuthPass)) != 1 {
		data := LoginPageData{
			Title: "Login",
			Next:  next,
			Error: "Invalid username or password",
			User:  strings.TrimSpace(a.Config.BasicAuthUser),
		}
		w.WriteHeader(http.StatusUnauthorized)
		_ = a.Renderer.Render(w, "login.html", data)
		return
	}

	token, expiresAt, err := issueUISessionToken(a.Config.BasicAuthUser, a.Config.BasicAuthPass, time.Now().UTC())
	if err != nil {
		http.Redirect(w, r, "/login?err=failed+to+create+session", http.StatusSeeOther)
		return
	}
	setUISessionCookie(w, token, expiresAt, a.Config.SessionSecure)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (a *App) Logout(w http.ResponseWriter, r *http.Request) {
	clearUISessionCookie(w, a.Config.SessionSecure)
	http.Redirect(w, r, "/login?msg=Logged+out", http.StatusSeeOther)
}
