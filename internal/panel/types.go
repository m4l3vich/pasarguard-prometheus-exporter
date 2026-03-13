package panel

import "time"

// User represents a PasarGuard Panel user.
type User struct {
	Username string     `json:"username"`
	Status   string     `json:"status"`
	OnlineAt *time.Time `json:"online_at"`
}

// Node represents a PasarGuard Panel node.
type Node struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	APIPort  int    `json:"api_port"`
	APIKey   string `json:"api_key"`
	ServerCA string `json:"server_ca"`
	Status   string `json:"status"`
}

// usersResponse is the paginated response wrapper for users.
type usersResponse struct {
	Users []User `json:"users"`
	Total int    `json:"total"`
}

// nodesResponse is the paginated response wrapper for nodes.
type nodesResponse struct {
	Nodes []Node `json:"nodes"`
	Total int    `json:"total"`
}

// tokenResponse is the authentication token response.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}
