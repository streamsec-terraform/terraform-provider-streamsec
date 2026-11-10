package client

import (
	"context"
	"errors"
	"net/http"
)

// CloudAccountsPath is the public API's path to the workspace's cloud accounts.
const CloudAccountsPath = "/openapi/integrations/cloud-accounts"

type Client struct {
	Token     string
	Workspace string
	Host      string
	// HTTPClient makes the client's calls; nil uses a client with a timeout.
	HTTPClient *http.Client
}

func NewClient(host, username, password, workspace_id *string, apiToken *string) (*Client, error) {

	c := Client{
		Host: *host,
	}

	c.Token = ""
	if workspace_id != nil {
		c.Workspace = *workspace_id
	}

	if apiToken != nil && *apiToken != "" {
		c.Token = *apiToken
		return &c, nil
	}
	err := c.authenticate(username, password)

	if err != nil {
		return nil, err
	}

	return &c, nil
}

func (c *Client) authenticate(username, password *string) error {
	var answer struct {
		Result struct {
			Data struct {
				AccessToken string `json:"access_token"`
			} `json:"data"`
		} `json:"result"`
	}
	err := c.DoAPIRequest(context.Background(), http.MethodPost, "/trpc/auth.login", map[string]interface{}{
		"email":    username,
		"password": password}, &answer)

	if err != nil {
		return err
	}

	if answer.Result.Data.AccessToken == "" {
		return errors.New("oops")
	}

	c.Token = answer.Result.Data.AccessToken

	return nil
}

// ListAccounts returns the workspace's cloud accounts, each one a JSON object.
func (c *Client) ListAccounts(ctx context.Context) ([]interface{}, error) {
	var accounts []interface{}
	if err := c.DoAPIRequest(ctx, http.MethodGet, CloudAccountsPath, nil, &accounts); err != nil {
		return nil, err
	}
	return accounts, nil
}

// CloudAccountPath is the public API's path to the cloud account with Stream Security id id.
func CloudAccountPath(id string) string {
	return CloudAccountsPath + "/" + escapeSegments(id)
}
