package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/linear"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func (a *App) showOAuthDialog() tea.Cmd {
	if a.dialog != nil && a.dialog.Visible() {
		return nil
	}
	missing := a.authMissingAccounts
	if len(missing) == 0 {
		return a.toast.ShowInfo("All Linear accounts are authenticated")
	}
	if len(missing) == 1 {
		return func() tea.Msg { return messages.StartOAuth{Account: missing[0]} }
	}
	options := make([]string, 0, len(missing))
	a.oauthAccountValues = a.oauthAccountValues[:0]
	for _, acct := range missing {
		if acct == "" {
			continue
		}
		options = append(options, acct)
		a.oauthAccountValues = append(a.oauthAccountValues, acct)
	}
	if len(options) == 0 {
		return a.toast.ShowInfo("No Linear accounts require auth")
	}
	a.dialog = common.NewSelectDialog("oauth-account", "Linear OAuth", "Select account:", options)

	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	return nil
}

func (a *App) startOAuth(account string) tea.Cmd {
	if a.linearConfig == nil {
		return a.toast.ShowError("Linear config missing")
	}
	cfg := a.linearConfig.OAuth
	if cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURI == "" {
		return a.toast.ShowError("Linear OAuth config missing")
	}

	state := newStateToken()
	authURL, err := linear.BuildOAuthURL(cfg, state)
	if err != nil {
		return a.toast.ShowError(err.Error())
	}

	return func() tea.Msg {
		codeCh := make(chan string, 1)
		errCh := make(chan error, 1)

		redirectURL, err := url.Parse(cfg.RedirectURI)
		if err != nil {
			return messages.OAuthCompleted{Account: account, Err: err}
		}
		host := redirectURL.Host
		if !strings.Contains(host, ":") {
			if redirectURL.Scheme == "http" {
				host = net.JoinHostPort(host, "80")
			} else {
				host = net.JoinHostPort(host, "443")
			}
		}
		path := redirectURL.Path
		if path == "" {
			path = "/"
		}

		mux := http.NewServeMux()
		server := &http.Server{Addr: host, Handler: mux}
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()
			if query.Get("state") != state {
				http.Error(w, "Invalid state", http.StatusBadRequest)
				return
			}
			code := query.Get("code")
			if code == "" {
				http.Error(w, "Missing code", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte("Authorization complete. You can close this window."))
			select {
			case codeCh <- code:
			default:
			}
		})

		go func() {
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				select {
				case errCh <- err:
				default:
				}
			}
		}()

		if err := openURLNow(authURL); err != nil {
			return messages.OAuthCompleted{Account: account, Err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		defer func() {
			_ = server.Shutdown(context.Background())
		}()

		select {
		case code := <-codeCh:
			token, err := linear.ExchangeOAuthCode(ctx, cfg, code)
			if err != nil {
				return messages.OAuthCompleted{Account: account, Err: err}
			}
			return messages.OAuthCompleted{Account: account, Token: token.AccessToken}
		case err := <-errCh:
			return messages.OAuthCompleted{Account: account, Err: err}
		case <-ctx.Done():
			return messages.OAuthCompleted{Account: account, Err: fmt.Errorf("oauth timed out")}
		}
	}
}

func newStateToken() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
