package hostwatch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// Heartbeat tells an external watcher that hostwatch is alive, so that its
// silence is noticed from outside the host.
type Heartbeat interface {
	Beat(ctx context.Context, up bool, msg string) error
}

// PushHeartbeat calls an Uptime Kuma style push URL with status and msg.
type PushHeartbeat struct {
	url    *url.URL
	client *http.Client
}

func NewPushHeartbeat(raw string) (*PushHeartbeat, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("heartbeat URL does not parse")
	}
	return &PushHeartbeat{
		url: u,
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				TLSHandshakeTimeout:   3 * time.Second,
				ResponseHeaderTimeout: 5 * time.Second,
				IdleConnTimeout:       60 * time.Second,
				MaxIdleConns:          2,
			},
		},
	}, nil
}

func (h *PushHeartbeat) Beat(ctx context.Context, up bool, msg string) error {
	u := *h.url
	q := u.Query()
	q.Set("status", map[bool]string{true: "up", false: "down"}[up])
	if len(msg) > 200 {
		msg = msg[:200]
	}
	q.Set("msg", msg)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return errors.New("heartbeat: cannot build request")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		// *url.Error prints the full URL, and the push token is in its path.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return fmt.Errorf("heartbeat request failed: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("heartbeat rejected with HTTP %d", resp.StatusCode)
	}
	return nil
}
