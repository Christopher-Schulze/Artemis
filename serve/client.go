package serve

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"

	"github.com/Christopher-Schulze/Artemis/security"
	"github.com/coder/websocket"
)

// Client is a serialized authenticated connection to the existing steering owner.
// Keep ClientID across connections to retain access; a new identity cannot inherit sessions.
type Client struct {
	lease            chan struct{}
	connection       *websocket.Conn
	identity         string
	sequence         uint64
	requestPrefix    string
	serverInstanceID string
	serverProcessID  int
}

func Dial(ctx context.Context, endpoint, token, clientID string) (*Client, error) {
	if ctx == nil {
		return nil, fmt.Errorf("control context required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed == nil {
		return nil, fmt.Errorf("invalid control endpoint")
	}
	address, addressErr := netip.ParseAddr(parsed.Hostname())
	if addressErr != nil || !address.IsLoopback() || (parsed.Scheme != "ws" && parsed.Scheme != "wss") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("control endpoint must be a loopback ws/wss URL without credentials or query")
	}
	if token == "" {
		return nil, fmt.Errorf("control bearer token required")
	}
	headers := http.Header{"Authorization": {"Bearer " + token}}
	if clientID != "" {
		headers.Set(ClientIDHeader, clientID)
	}
	connection, response, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPHeader: headers, HTTPClient: &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}})
	if err != nil {
		return nil, fmt.Errorf("connect control owner: %w", err)
	}
	identity := response.Header.Get(ClientIDHeader)
	if identity == "" {
		return nil, errorsWithClose(connection, fmt.Errorf("control owner did not issue a client identity"))
	}
	connection.SetReadLimit(8 << 20)
	client := &Client{connection: connection, identity: identity, lease: make(chan struct{}, 1), requestPrefix: rand.Text()}
	var version VersionResponse
	if err := client.Call(ctx, CmdVersion, nil, &version); err != nil {
		return nil, errorsWithClose(connection, err)
	}
	if majorVersion(version.Protocol) != majorVersion(ProtocolVersion) {
		return nil, errorsWithClose(connection, fmt.Errorf("incompatible control protocol %q", version.Protocol))
	}
	client.serverInstanceID = version.InstanceID
	client.serverProcessID = version.ProcessID
	return client, nil
}

func errorsWithClose(connection *websocket.Conn, err error) error {
	if closeErr := connection.CloseNow(); closeErr != nil {
		return fmt.Errorf("%w; close connection: %v", err, closeErr)
	}
	return err
}

func (c *Client) ClientID() string { return c.identity }

// ServerInstanceID is the identity observed during the authenticated handshake.
func (c *Client) ServerInstanceID() string { return c.serverInstanceID }

// ServerProcessID is the process observed during the authenticated handshake.
func (c *Client) ServerProcessID() int { return c.serverProcessID }

func (c *Client) Close() error { return c.connection.Close(websocket.StatusNormalClosure, "") }

// Call never retries a command after dispatch; a lost response may represent a real effect.
func (c *Client) Call(ctx context.Context, command Command, params, result any) error {
	_, err := c.CallLabeled(ctx, command, params, result)
	return err
}

// CallLabeled is Call that also returns the response trust label; it is nil
// for results that are not page-derived.
func (c *Client) CallLabeled(ctx context.Context, command Command, params, result any) (*security.ContentTrust, error) {
	if ctx == nil {
		return nil, fmt.Errorf("control context required")
	}
	select {
	case c.lease <- struct{}{}:
		defer func() { <-c.lease }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	c.sequence++
	request, err := MarshalTyped(c.requestPrefix+"-"+strconv.FormatUint(c.sequence, 10), command, params)
	if err != nil {
		return nil, err
	}
	request.Version = ProtocolVersion
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if err := c.connection.Write(ctx, websocket.MessageText, data); err != nil {
		return nil, fmt.Errorf("dispatch %s: %w", command, err)
	}
	for {
		_, data, err := c.connection.Read(ctx)
		if err != nil {
			return nil, fmt.Errorf("receive %s outcome (do not blindly replay effects): %w", command, err)
		}
		var response Response
		if err := json.Unmarshal(data, &response); err != nil {
			return nil, fmt.Errorf("decode control outcome: %w", err)
		}
		if response.ID == "" && !response.OK && response.Error != nil {
			return nil, DecodeTypedResult(&response, result)
		}
		if response.ID != request.ID {
			continue
		}
		return response.Trust, DecodeTypedResult(&response, result)
	}
}
