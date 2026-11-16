package relay

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func firefoxPath() string {
	for _, name := range []string{"firefox", "firefox-esr"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	paths := []string{"/usr/bin/firefox", "/usr/bin/firefox-esr", "/snap/bin/firefox", "/Applications/Firefox.app/Contents/MacOS/firefox"}
	for _, root := range []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)")} {
		if root != "" {
			paths = append(paths, filepath.Join(root, "Mozilla Firefox", "firefox.exe"))
		}
	}
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func isFirefox(path string) bool {
	return strings.Contains(strings.ToLower(filepath.Base(path)), "firefox")
}

func firefoxProfileRoot() string {
	// Ubuntu's snap has its own /tmp; a profile there would be invisible to Firefox.
	if runtime.GOOS == "linux" {
		if home, err := os.UserHomeDir(); err == nil {
			root := filepath.Join(home, "snap", "firefox", "common")
			if info, err := os.Stat(root); err == nil && info.IsDir() {
				return root
			}
		}
	}
	return ""
}

func startFirefox(ctx context.Context, path, profile string) (*exec.Cmd, *devTools, error) {
	command := exec.CommandContext(ctx, path, "--no-remote", "--new-instance", "--profile", profile, "--remote-debugging-port=0", "about:blank")
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, nil, err
	}
	if err = command.Start(); err != nil {
		return nil, nil, fmt.Errorf("Не удалось открыть Firefox для входа VK")
	}
	success := false
	defer func() {
		if !success {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	endpoints := make(chan string, 1)
	go func() {
		defer close(endpoints)
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			const prefix = "WebDriver BiDi listening on "
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			endpoint, err := url.Parse(strings.TrimSpace(strings.TrimPrefix(line, prefix)))
			if err == nil && endpoint.Scheme == "ws" && endpoint.Hostname() == "127.0.0.1" && endpoint.Port() != "" && endpoint.User == nil && endpoint.RawQuery == "" {
				endpoint.Path = "/session"
				select {
				case endpoints <- endpoint.String():
				default:
				}
			}
		}
	}()
	var endpoint string
	select {
	case endpoint = <-endpoints:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-time.After(30 * time.Second):
	}
	if endpoint == "" {
		return nil, nil, fmt.Errorf("Firefox не открыл WebDriver BiDi; обновите браузер")
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, endpoint, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("Не удалось подключиться к окну Firefox")
	}
	c := &devTools{conn: conn, wait: 20 * time.Second, firefox: true}
	if _, err = c.rawCall("session.new", map[string]any{"capabilities": map[string]any{}}); err == nil {
		var pages []browserTab
		pages, err = c.tabs(nil, "")
		if err == nil && len(pages) > 0 {
			c.context = pages[0].Context
		} else if err == nil {
			err = fmt.Errorf("Окно Firefox не найдено")
		}
	}
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	success = true
	return command, c, nil
}

func (c *devTools) tabs(client *http.Client, endpoint string) ([]browserTab, error) {
	if !c.firefox {
		return browserTabs(client, endpoint)
	}
	result, err := c.rawCall("browsingContext.getTree", map[string]any{"maxDepth": 0})
	var tree struct {
		Contexts []struct{ Context, URL string } `json:"contexts"`
	}
	if err == nil {
		err = json.Unmarshal(result, &tree)
	}
	var pages []browserTab
	for _, entry := range tree.Contexts {
		pages = append(pages, browserTab{Type: "page", URL: entry.URL, Context: entry.Context})
	}
	return pages, err
}

// Firefox removed CDP; translate only the login window's existing operations to WebDriver BiDi.
func (c *devTools) firefoxCall(method string, params any) (json.RawMessage, error) {
	p, _ := params.(map[string]any)
	switch method {
	case "Network.enable", "Network.setExtraHTTPHeaders", "Network.setUserAgentOverride":
		return json.RawMessage(`{}`), nil
	case "Page.navigate":
		return c.rawCall("browsingContext.navigate", map[string]any{"context": c.context, "url": p["url"], "wait": "none"})
	case "Runtime.evaluate":
		return c.rawCall("script.evaluate", map[string]any{"expression": p["expression"], "target": map[string]any{"context": c.context}, "awaitPromise": true})
	case "Browser.close":
		return c.rawCall("browser.close", map[string]any{})
	case "Network.clearBrowserCookies":
		return c.rawCall("storage.deleteCookies", map[string]any{})
	case "Network.setCookies":
		for _, cookie := range p["cookies"].([]map[string]any) {
			value := map[string]any{"name": cookie["name"], "domain": cookie["domain"], "path": cookie["path"], "secure": cookie["secure"], "httpOnly": cookie["httpOnly"], "value": map[string]any{"type": "string", "value": cookie["value"]}}
			if expiry, ok := cookie["expires"]; ok {
				value["expiry"] = expiry
			}
			if _, err := c.rawCall("storage.setCookie", map[string]any{"cookie": value}); err != nil {
				return nil, err
			}
		}
		return json.RawMessage(`{}`), nil
	case "Network.getCookies":
		result, err := c.rawCall("storage.getCookies", map[string]any{})
		if err != nil {
			return nil, err
		}
		var data struct {
			Cookies []struct {
				Name, Domain, Path string
				Secure, HTTPOnly   bool
				Expiry             float64
				Value              struct{ Type, Value string }
			}
		}
		if err = json.Unmarshal(result, &data); err != nil {
			return nil, err
		}
		cookies := []browserCookie{}
		for _, cookie := range data.Cookies {
			if cookie.Value.Type == "string" && trustedVK("https://"+strings.TrimPrefix(cookie.Domain, ".")+"/") {
				cookies = append(cookies, browserCookie{Name: cookie.Name, Value: cookie.Value.Value, Domain: cookie.Domain, Path: cookie.Path, Secure: cookie.Secure, HTTPOnly: cookie.HTTPOnly, Expires: cookie.Expiry})
			}
		}
		return json.Marshal(map[string]any{"cookies": cookies})
	}
	return nil, fmt.Errorf("Неподдерживаемая операция окна Firefox")
}
