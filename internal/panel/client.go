package panel

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// Client communicates with the PasarGuard Panel API.
type Client struct {
	baseURL    string
	username   string
	password   string
	apiKey     string
	httpClient *http.Client
	token      string
	mu         sync.Mutex

	basicUser string
	basicPass string
}

// Credentials bundles the ways the Client can authenticate with the Panel.
//
// If APIKey is set, it's used for every request via the X-Api-Key header and
// Username/Password are ignored (API keys need no login/refresh step). Otherwise
// Username/Password are used to obtain and refresh a JWT via the OAuth2 password grant.
//
// BasicUser/BasicPass are independent of the above — they add HTTP Basic Auth on
// the transport level (e.g. for a reverse proxy in front of the Panel). Pass empty
// strings to disable.
type Credentials struct {
	APIKey    string
	Username  string
	Password  string
	BasicUser string
	BasicPass string
}

// NewClient creates a new PasarGuard Panel API client.
// tlsCfg may be nil for default TLS behavior.
func NewClient(baseURL string, creds Credentials, tlsCfg *tls.Config) *Client {
	httpClient := &http.Client{}
	if tlsCfg != nil {
		httpClient.Transport = &http.Transport{TLSClientConfig: tlsCfg}
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		username:   creds.Username,
		password:   creds.Password,
		apiKey:     creds.APIKey,
		httpClient: httpClient,
		basicUser:  creds.BasicUser,
		basicPass:  creds.BasicPass,
	}
}

func (c *Client) setBasicAuth(req *http.Request) {
	if c.basicUser != "" {
		req.SetBasicAuth(c.basicUser, c.basicPass)
	}
}

// Authenticate obtains a new JWT access token from the panel.
func (c *Client) Authenticate(ctx context.Context) error {
	form := url.Values{
		"username":   {c.username},
		"password":   {c.password},
		"grant_type": {"password"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/admin/token",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.setBasicAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("authenticate: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return fmt.Errorf("authenticate: decode token: %w", err)
	}

	c.mu.Lock()
	c.token = tok.AccessToken
	c.mu.Unlock()

	return nil
}

// applyAuth sets the Authorization/X-Api-Key and (optional) Basic Auth headers
// on req according to the configured Credentials.
func (c *Client) applyAuth(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	} else {
		c.mu.Lock()
		token := c.token
		c.mu.Unlock()
		req.Header.Set("Authorization", "Bearer "+token)
	}
	c.setBasicAuth(req)
}

// doAuthenticated executes an HTTP request with the configured credentials.
// In username/password mode, a 401 or 422 response triggers one re-authentication
// and retry. API keys have no login/refresh step, so in API-key mode a 401/422 is
// returned as-is (the caller reports it as a request failure).
// The caller is responsible for closing the returned response body.
func (c *Client) doAuthenticated(ctx context.Context, req *http.Request) (*http.Response, error) {
	c.applyAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if c.apiKey == "" && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusUnprocessableEntity) {
		resp.Body.Close()

		slog.Debug("retrying after auth failure", "url", req.URL, "status", resp.StatusCode)

		if err := c.Authenticate(ctx); err != nil {
			return nil, fmt.Errorf("re-authenticate: %w", err)
		}

		// Rebuild the request because the original body may have been consumed.
		retryReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("rebuild request: %w", err)
		}
		c.applyAuth(retryReq)

		resp, err = c.httpClient.Do(retryReq)
		if err != nil {
			return nil, err
		}
	}

	return resp, nil
}

// GetUsers retrieves all users from the panel using pagination.
func (c *Client) GetUsers(ctx context.Context) ([]User, error) {
	var all []User
	offset := 0
	const limit = 100

	for {
		u := fmt.Sprintf("%s/api/users?offset=%d&limit=%d", c.baseURL, offset, limit)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, fmt.Errorf("get users: %w", err)
		}

		resp, err := c.doAuthenticated(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("get users: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("get users: unexpected status %d: %s", resp.StatusCode, string(body))
		}

		var page usersResponse
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("get users: decode: %w", err)
		}
		resp.Body.Close()

		all = append(all, page.Users...)

		if len(all) >= page.Total {
			break
		}
		offset += limit
	}

	return all, nil
}

// GetNodes retrieves all nodes from the panel using pagination.
func (c *Client) GetNodes(ctx context.Context) ([]Node, error) {
	var all []Node
	offset := 0
	const limit = 100

	for {
		u := fmt.Sprintf("%s/api/nodes?offset=%d&limit=%d", c.baseURL, offset, limit)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, fmt.Errorf("get nodes: %w", err)
		}

		resp, err := c.doAuthenticated(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("get nodes: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("get nodes: unexpected status %d: %s", resp.StatusCode, string(body))
		}

		var page nodesResponse
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("get nodes: decode: %w", err)
		}
		resp.Body.Close()

		all = append(all, page.Nodes...)

		if len(all) >= page.Total {
			break
		}
		offset += limit
	}

	return all, nil
}
