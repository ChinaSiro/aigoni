package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"sync"
	"time"
)

const cookieName = "aigoni_session"

const (
	// maxLoginFailures 达到该失败次数后，锁定登录一段窗口。
	maxLoginFailures = 5
	// loginBlockDuration 达到失败阈值后的锁定时长。
	loginBlockDuration = 15 * time.Minute
)

type session struct {
	expiresAt time.Time
	csrfToken string
}

type loginState struct {
	failures     int
	blockedUntil time.Time
}

type Manager struct {
	password string
	// secure 为 true 时会话 Cookie 携带 Secure 标记，供 HTTPS 部署使用。
	secure   bool
	sessions map[string]session
	failures map[string]loginState
	mu       sync.RWMutex
}

type Session struct {
	ExpiresAt time.Time
	CSRFToken string
}

func NewManager(password string, secure bool) *Manager {
	return &Manager{
		password: password,
		secure:   secure,
		sessions: map[string]session{},
		failures: map[string]loginState{},
	}
}

// LoginBlocked 返回指定远端地址是否处于登录锁定状态，以及剩余的锁定时长。
func (m *Manager) LoginBlocked(remoteAddr string) (bool, time.Duration) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, ok := m.failures[loginKey(remoteAddr)]
	if !ok {
		return false, 0
	}
	if remaining := time.Until(state.blockedUntil); remaining > 0 {
		return true, remaining
	}
	return false, 0
}

// Login 校验密码并创建会话。密码比较使用恒定时间比较，避免时序侧信道。
// 失败会累计限速状态；成功会清空该地址的失败记录。
func (m *Manager) Login(w http.ResponseWriter, remoteAddr, password string) bool {
	key := loginKey(remoteAddr)
	now := time.Now()

	m.mu.Lock()
	state := m.failures[key]
	if now.Before(state.blockedUntil) {
		m.mu.Unlock()
		return false
	}
	if !constantTimeEqual(password, m.password) {
		state.failures++
		if state.failures >= maxLoginFailures {
			state.blockedUntil = now.Add(loginBlockDuration)
			state.failures = 0
		}
		m.failures[key] = state
		m.mu.Unlock()
		return false
	}
	delete(m.failures, key)
	m.mu.Unlock()

	token := randomToken()
	m.mu.Lock()
	m.sessions[token] = session{
		expiresAt: now.Add(24 * time.Hour),
		csrfToken: randomToken(),
	}
	m.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   m.secure,
		Expires:  now.Add(24 * time.Hour),
	})
	return true
}

func (m *Manager) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil {
		m.mu.Lock()
		delete(m.sessions, cookie.Value)
		m.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1})
}

func (m *Manager) Authenticated(r *http.Request) bool {
	_, ok := m.Session(r)
	return ok
}

func (m *Manager) Session(r *http.Request) (Session, bool) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return Session{}, false
	}

	m.mu.RLock()
	current, ok := m.sessions[cookie.Value]
	m.mu.RUnlock()
	if !ok || time.Now().After(current.expiresAt) {
		if ok {
			m.mu.Lock()
			delete(m.sessions, cookie.Value)
			m.mu.Unlock()
		}
		return Session{}, false
	}
	return Session{ExpiresAt: current.expiresAt, CSRFToken: current.csrfToken}, true
}

func (m *Manager) ValidCSRFToken(r *http.Request, token string) bool {
	session, ok := m.Session(r)
	return ok && token != "" && constantTimeEqual(token, session.CSRFToken)
}

func randomToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().String()))
	}
	return hex.EncodeToString(buf)
}

// loginKey 把 RemoteAddr 归一为纯 IP 作为限速键；空地址统一用一个固定键。
func loginKey(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil && host != "" {
		return host
	}
	if remoteAddr != "" {
		return remoteAddr
	}
	return "unknown"
}

// constantTimeEqual 先做固定长度 SHA-256 摘要再恒定时间比较，
// 避免字符串长度差异带来的时序泄露。
func constantTimeEqual(a, b string) bool {
	digest := func(value string) []byte {
		sum := sha256.Sum256([]byte(value))
		return sum[:]
	}
	return subtle.ConstantTimeCompare(digest(a), digest(b)) == 1
}
