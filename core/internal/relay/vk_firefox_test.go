package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestFirefoxLoginOperations(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var request struct {
				ID     int
				Method string
				Params map[string]json.RawMessage
			}
			if conn.ReadJSON(&request) != nil {
				return
			}
			result := any(map[string]any{})
			switch request.Method {
			case "storage.getCookies":
				result = map[string]any{"cookies": []any{
					map[string]any{"name": "remixsid", "domain": ".vk.ru", "path": "/", "httpOnly": true, "secure": true, "expiry": 1900000000, "value": map[string]any{"type": "string", "value": "fixture-session"}},
					map[string]any{"name": "foreign", "domain": "example.com", "value": map[string]any{"type": "string", "value": "must-not-save"}},
				}}
			case "storage.setCookie":
				var cookie struct {
					Value  struct{ Type, Value string }
					Expiry float64
				}
				if json.Unmarshal(request.Params["cookie"], &cookie) != nil || cookie.Value.Type != "string" || cookie.Value.Value != "fixture-session" || cookie.Expiry != 1900000000 {
					t.Error("restored cookie lost its value or expiry")
				}
			case "script.evaluate":
				result = map[string]any{"type": "success", "result": map[string]any{"type": "string", "value": "https://vk.ru/"}}
			case "browsingContext.getTree":
				result = map[string]any{"contexts": []any{map[string]any{"context": "login", "url": "https://vk.ru/"}}}
			case "browsingContext.navigate", "storage.deleteCookies", "browser.close":
			default:
				t.Errorf("unexpected Firefox command %s", request.Method)
			}
			if conn.WriteJSON(map[string]any{"id": request.ID, "type": "success", "result": result}) != nil {
				return
			}
		}
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c := devTools{conn: conn, firefox: true, context: "login", wait: time.Second}
	if _, err := c.call("Network.setCookies", map[string]any{"cookies": []map[string]any{{"name": "remixsid", "value": "fixture-session", "domain": ".vk.ru", "path": "/", "secure": true, "httpOnly": true, "expires": float64(1900000000)}}}); err != nil {
		t.Fatal(err)
	}
	result, err := c.call("Network.getCookies", map[string]any{"urls": vkCookieURLs})
	if err != nil {
		t.Fatal(err)
	}
	var cookies struct{ Cookies []browserCookie }
	if json.Unmarshal(result, &cookies) != nil || len(cookies.Cookies) != 1 || cookies.Cookies[0].Value != "fixture-session" || !cookies.Cookies[0].HTTPOnly {
		t.Fatalf("unsafe or incomplete cookie conversion: %s", result)
	}
	if location, err := c.evaluate("location.href"); err != nil || location != "https://vk.ru/" {
		t.Fatalf("evaluate: %q %v", location, err)
	}
	if pages, err := c.tabs(nil, ""); err != nil || len(pages) != 1 || pages[0].Context != "login" {
		t.Fatalf("tabs: %v %v", pages, err)
	}
	for _, method := range []string{"Page.navigate", "Network.clearBrowserCookies", "Browser.close"} {
		if _, err := c.call(method, map[string]any{"url": "https://vk.ru/"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFirefoxBrowser(t *testing.T) {
	if os.Getenv("RAWLINE_TEST_FIREFOX") != "1" {
		t.Skip("requires a headless Firefox installation")
	}
	path := firefoxPath()
	if path == "" {
		t.Fatal("Firefox is not installed")
	}
	t.Setenv("MOZ_HEADLESS", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command, browser, err := startFirefox(ctx, path, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = browser.conn.Close(); _ = command.Process.Kill(); _ = command.Wait() }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><title>Rawline fixture</title><body>Local login fixture</body></html>"))
	}))
	defer server.Close()
	if _, err := browser.call("Page.navigate", map[string]any{"url": server.URL}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	title := ""
	for time.Now().Before(deadline) {
		value, err := browser.evaluate("document.title")
		if err != nil {
			t.Fatal(err)
		}
		title = value
		if value == "Rawline fixture" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("Firefox did not navigate")
		case <-time.After(50 * time.Millisecond):
		}
	}
	if title != "Rawline fixture" {
		t.Fatal("Firefox did not load the local fixture")
	}
	if _, err := browser.call("Network.setCookies", map[string]any{"cookies": []map[string]any{{"name": "remixsid", "value": "fixture-session", "domain": ".vk.ru", "path": "/", "secure": true, "httpOnly": true}}}); err != nil {
		t.Fatal(err)
	}
	data, err := browser.call("Network.getCookies", map[string]any{"urls": vkCookieURLs})
	if err != nil || !strings.Contains(string(data), "fixture-session") {
		t.Fatalf("Firefox cannot read its private session: %s %v", data, err)
	}
	if _, err := browser.call("Network.clearBrowserCookies", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	data, err = browser.call("Network.getCookies", map[string]any{"urls": vkCookieURLs})
	if err != nil || strings.Contains(string(data), "fixture-session") {
		t.Fatal("Firefox did not clear its private session")
	}
	_, _ = browser.call("Browser.close", map[string]any{})
}
