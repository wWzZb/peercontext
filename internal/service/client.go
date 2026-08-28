package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"time"

	"github.com/wWzZb/peercontext/internal/failure"
)

type ControlClient struct {
	http *http.Client
}

func NewControlClient(socketPath string) *ControlClient {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}
	return &ControlClient{http: &http.Client{Transport: transport, Timeout: 10 * time.Minute}}
}

func (c *ControlClient) Do(ctx context.Context, action string, input, output any) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	commandBody, _ := json.Marshal(Command{Action: action, Payload: payload})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://peerctx/control", bytes.NewReader(commandBody))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var reply Reply
	if err := json.NewDecoder(response.Body).Decode(&reply); err != nil {
		return err
	}
	if !reply.OK {
		if reply.Error == nil {
			return failure.New("peerctx_error", "PeerContext service rejected the command.", false)
		}
		return reply.Error
	}
	if output == nil || len(reply.Data) == 0 {
		return nil
	}
	return json.Unmarshal(reply.Data, output)
}
