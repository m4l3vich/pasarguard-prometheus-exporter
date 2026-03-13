package panel

import (
	"context"
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
	httpClient *http.Client
	token      string
	mu         sync.Mutex
}

// NewClient creates a new PasarGuard Panel API client.
func NewClient(baseURL, username, password string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		username:   username,
		password:   password,
		httpClient: &http.Client{},
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

// doAuthenticated executes an HTTP request with the current Bearer token.
// On a 401 or 422 response it re-authenticates once and retries.
// The caller is responsible for closing the returned response body.
func (c *Client) doAuthenticated(ctx context.Context, req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	token := c.token
	c.mu.Unlock()

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusUnprocessableEntity {
		resp.Body.Close()

		slog.Debug("retrying after auth failure", "url", req.URL, "status", resp.StatusCode)

		if err := c.Authenticate(ctx); err != nil {
			return nil, fmt.Errorf("re-authenticate: %w", err)
		}

		c.mu.Lock()
		token = c.token
		c.mu.Unlock()

		// Rebuild the request because the original body may have been consumed.
		retryReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("rebuild request: %w", err)
		}
		retryReq.Header.Set("Authorization", "Bearer "+token)

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
