package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/RobertLMcCrary/D2L-MCP/internal/config"
)

type LoginOptions struct {
	Headless bool
	Timeout  time.Duration
}

func Login(ctx context.Context, paths config.Paths, host string, options LoginOptions) (Status, error) {
	if options.Timeout == 0 {
		options.Timeout = 5 * time.Minute
	}

	if err := os.MkdirAll(paths.Dir, 0o700); err != nil {
		return Status{}, err
	}

	release, err := acquireLock(ctx, paths.BrowserProfile+".lock")
	if err != nil {
		return Status{}, err
	}
	defer release()

	if err := os.MkdirAll(paths.BrowserProfile, 0o700); err != nil {
		return Status{}, err
	}

	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserDataDir(paths.BrowserProfile),
		chromedp.Flag("headless", options.Headless),
		chromedp.Flag("disable-background-networking", false),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
	)

	allocator, cancelAllocator := chromedp.NewExecAllocator(ctx, allocatorOptions...)
	defer cancelAllocator()

	browser, cancelBrowser := chromedp.NewContext(allocator)
	defer cancelBrowser()

	browser, cancelTimeout := context.WithTimeout(browser, options.Timeout)
	defer cancelTimeout()

	var mu sync.Mutex
	var captured string

	store := func(candidate string) {
		candidate = strings.TrimSpace(strings.TrimPrefix(candidate, "Bearer "))

		if _, err := Validate(candidate); err == nil {
			mu.Lock()
			captured = candidate
			mu.Unlock()
		}
	}

	chromedp.ListenTarget(browser, func(event any) {
		request, ok := event.(*network.EventRequestWillBeSent)
		if !ok {
			return
		}

		for key, value := range request.Request.Headers {
			if strings.EqualFold(key, "Authorization") {
				store(fmt.Sprint(value))
			}
		}
	})

	loginURL := strings.TrimRight(host, "/") + "/d2l/home"

	if err := chromedp.Run(
		browser,
		network.Enable(),
		chromedp.Navigate(loginURL),
	); err != nil {
		return Status{}, fmt.Errorf("launch browser: %w", err)
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		mu.Lock()
		token := captured
		mu.Unlock()

		if token == "" {
			token = evaluateToken(browser)
		}

		if token != "" {
			if err := Save(paths, token); err != nil {
				return Status{}, err
			}

			status, err := Validate(token)
			status.Source = paths.Token

			return status, err
		}

		select {
		case <-browser.Done():
			if errors.Is(browser.Err(), context.DeadlineExceeded) {
				return Status{}, errors.New("timed out waiting for Brightspace login")
			}
			return Status{}, browser.Err()
		case <-ticker.C:
		}
	}
}

func acquireLock(ctx context.Context, path string) (func(), error) {
	for {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
			_ = file.Close()

			return func() { _ = os.Remove(path) }, nil
		}

		if !os.IsExist(err) {
			return nil, err
		}

		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > 10*time.Minute {
			_ = os.Remove(path)
			continue
		}

		timer := time.NewTimer(250 * time.Millisecond)

		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func SilentRefresh(ctx context.Context, paths config.Paths, host string) (Status, error) {
	if os.Getenv("D2L_NO_AUTO_LOGIN") != "" {
		return Status{}, errors.New("automatic login is disabled")
	}

	if info, err := os.Stat(paths.BrowserProfile); err != nil || !info.IsDir() {
		return Status{}, errors.New("no saved browser profile")
	}

	return Login(ctx, paths, host, LoginOptions{
		Headless: true,
		Timeout:  30 * time.Second,
	})
}

func evaluateToken(ctx context.Context) string {
	// Brightspace stores per-scope tokens in D2L.Fetch.Tokens. The fallback
	// XSRF exchange follows the flow discovered and documented by d2l-cli:
	// https://github.com/Aaryan-Kapoor/d2l-cli/blob/v0.2.2/src/d2l/commands/auth_cmd.py
	script := `(async () => {
		const raw = localStorage.getItem("D2L.Fetch.Tokens");
		if (raw) {
			try {
				const value = JSON.parse(raw);
				let best = "";
				let bestExp = 0;
				for (const item of Object.values(value)) {
					if (!item || typeof item !== "object" || typeof item.access_token !== "string") continue;
					try {
						let encoded = item.access_token.split(".")[1].replace(/-/g, "+").replace(/_/g, "/");
						while (encoded.length % 4) encoded += "=";
						const payload = JSON.parse(atob(encoded));
						if ((payload.exp || 0) > bestExp) {
							best = item.access_token;
							bestExp = payload.exp || 0;
						}
					} catch (_) {}
				}
				if (best) return best;
			} catch (_) {}
		}
		try {
			let csrf = localStorage.getItem("XSRF.Token");
			if (!csrf) {
				const xsrfResponse = await fetch("/d2l/lp/auth/xsrf-tokens", {credentials:"include"});
				const xsrf = await xsrfResponse.json();
				csrf = xsrf.referrerToken;
				if (csrf) localStorage.setItem("XSRF.Token", csrf);
			}
			if (!csrf) return "";
			const tokenResponse = await fetch("/d2l/lp/auth/oauth2/token", {
				method:"POST", credentials:"include",
				headers:{"Content-Type":"application/x-www-form-urlencoded","X-Csrf-Token":csrf},
				body:"scope=*:*:*"
			});
			const value = await tokenResponse.json();
			return value.access_token || value.token || "";
		} catch (_) { return ""; }
	})()`

	var raw json.RawMessage

	err := chromedp.Run(ctx, chromedp.ActionFunc(func(actionCtx context.Context) error {
		result, exception, err := runtime.Evaluate(script).
			WithAwaitPromise(true).
			WithReturnByValue(true).
			Do(actionCtx)
		if err != nil {
			return err
		}

		if exception != nil {
			return errors.New(exception.Text)
		}

		raw = json.RawMessage(result.Value)

		return nil
	}))
	if err != nil {
		return ""
	}

	var token string

	_ = json.Unmarshal(raw, &token)

	if _, err := Validate(token); err != nil {
		return ""
	}

	return token
}
