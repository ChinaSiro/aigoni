package auth

import (
	"net/http/httptest"
	"testing"
)

func TestSessionContainsCSRFToken(t *testing.T) {
	manager := NewManager("secret", false)
	response := httptest.NewRecorder()
	if !manager.Login(response, "127.0.0.1:1234", "secret") {
		t.Fatal("Login returned false")
	}

	request := httptest.NewRequest("GET", "/", nil)
	request.AddCookie(response.Result().Cookies()[0])
	session, ok := manager.Session(request)
	if !ok {
		t.Fatal("Session not found")
	}
	if session.CSRFToken == "" {
		t.Fatal("CSRF token is empty")
	}
	if !manager.ValidCSRFToken(request, session.CSRFToken) {
		t.Fatal("ValidCSRFToken rejected session token")
	}
	if manager.ValidCSRFToken(request, "wrong") {
		t.Fatal("ValidCSRFToken accepted invalid token")
	}
}

func TestLoginFailureLocksAndSuccessResets(t *testing.T) {
	manager := NewManager("secret-pass", false)
	response := httptest.NewRecorder()

	// 连续失败达到阈值后锁定。
	for i := range maxLoginFailures {
		if manager.Login(response, "10.0.0.1:9999", "wrong") {
			t.Fatalf("Login %d accepted wrong password", i+1)
		}
	}
	if blocked, _ := manager.LoginBlocked("10.0.0.1:9999"); !blocked {
		t.Fatal("expected login to be blocked after failures")
	}
	// 锁定期间即使密码正确也拒绝。
	if manager.Login(response, "10.0.0.1:9999", "secret-pass") {
		t.Fatal("Login succeeded while blocked")
	}

	// 不同地址不受影响。
	if !manager.Login(response, "10.0.0.2:9999", "secret-pass") {
		t.Fatal("unrelated address should be able to log in")
	}
	// 成功登录后失败计数清空，不再处于锁定。
	if blocked, _ := manager.LoginBlocked("10.0.0.2:9999"); blocked {
		t.Fatal("successful login should clear block state")
	}
}

func TestCookieSecureFlag(t *testing.T) {
	manager := NewManager("secret-pass", true)
	response := httptest.NewRecorder()
	if !manager.Login(response, "10.0.0.1:9999", "secret-pass") {
		t.Fatal("Login returned false")
	}
	cookie := response.Result().Cookies()[0]
	if !cookie.Secure {
		t.Fatal("expected Secure flag on cookie when configured")
	}
}

func TestValidCSRFTokenRejectsSimilarToken(t *testing.T) {
	manager := NewManager("secret-pass", false)
	response := httptest.NewRecorder()
	if !manager.Login(response, "10.0.0.1:9999", "secret-pass") {
		t.Fatal("Login returned false")
	}
	request := httptest.NewRequest("GET", "/", nil)
	request.AddCookie(response.Result().Cookies()[0])
	session, ok := manager.Session(request)
	if !ok {
		t.Fatal("Session not found")
	}
	// 仅长度相近的 token 也必须被拒绝，证明比较不是普通前缀/字符串匹配。
	// 翻转最后一个 hex 字符，保证构造出的 token 必然与原 token 不同（hex 全 0 除外）。
	replace := "a"
	if session.CSRFToken[len(session.CSRFToken)-1] == 'a' {
		replace = "b"
	}
	similar := session.CSRFToken[:len(session.CSRFToken)-1] + replace
	if manager.ValidCSRFToken(request, similar) {
		t.Fatal("ValidCSRFToken accepted a similar-but-different token")
	}
	if manager.ValidCSRFToken(request, "") {
		t.Fatal("ValidCSRFToken accepted empty token")
	}
}

func TestLoginBlockedWithWrongMethod(t *testing.T) {
	// 确保 Login 在锁定态下不会创建 Session Cookie。
	manager := NewManager("secret-pass", false)
	response := httptest.NewRecorder()
	for range maxLoginFailures {
		manager.Login(response, "10.0.0.1:9999", "wrong")
	}
	response = httptest.NewRecorder()
	if manager.Login(response, "10.0.0.1:9999", "secret-pass") {
		t.Fatal("Login succeeded while blocked")
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("blocked login must not set a session cookie")
	}
}
