package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/mstgnz/cdn/pkg/httpx"
	"github.com/mstgnz/cdn/service"
)

type WebSocketHandler interface {
	HandleWebSocket(w http.ResponseWriter, r *http.Request) error
	MonitorStats(w http.ResponseWriter, r *http.Request) error
}

type webSocketHandler struct {
	clients    map[*websocket.Conn]bool
	clientsMux sync.RWMutex
	stats      *service.StatsService
}

type MonitoringStats struct {
	Timestamp     time.Time        `json:"timestamp"`
	ActiveUploads int              `json:"active_uploads"`
	UploadSpeed   float64          `json:"upload_speed"`
	CacheHitRate  float64          `json:"cache_hit_rate"`
	CPUUsage      float64          `json:"cpu_usage"`
	MemoryUsage   float64          `json:"memory_usage"`
	DiskUsage     map[string]int64 `json:"disk_usage"`
	Errors        []string         `json:"errors"`
}

func NewWebSocketHandler(stats *service.StatsService) WebSocketHandler {
	return &webSocketHandler{
		clients: make(map[*websocket.Conn]bool),
		stats:   stats,
	}
}

// HandleWebSocket upgrades the connection and streams stats every 5 seconds.
func (h *webSocketHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request) error {
	// A handshake fasthttp's upgrader refused was answered 426 by gofiber;
	// checking first keeps coder/websocket's own error texts out of the contract.
	if !validHandshake(r) {
		// fasthttp's upgrader reset the response on refusal, dropping headers
		// set earlier such as nosniff; the limiter's came after and stay.
		if x := httpx.From(w); x != nil {
			x.ResetHeaders()
		}
		httpx.Text(w, http.StatusUpgradeRequired, httpx.TextUpgradeRequired)
		return nil
	}
	// The server's read and write timeouts would outlive the hijack and cut the
	// stream after a minute; fasthttp cleared them before handing the conn over.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})

	// gofiber allowed every origin; the token check has already run.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return nil // Accept has answered the client
	}

	// Register client
	h.clientsMux.Lock()
	h.clients[c] = true
	h.clientsMux.Unlock()

	ctx, cancel := context.WithCancel(context.Background())

	// Cleanup on disconnect: the TCP connection is dropped without a close
	// frame, as the fasthttp version did.
	defer func() {
		cancel()
		h.clientsMux.Lock()
		delete(h.clients, c)
		h.clientsMux.Unlock()
		_ = c.CloseNow()
	}()

	// Read and discard so control frames are answered and a disconnect is seen;
	// the old handler ignored whatever the client sent.
	go func() {
		defer cancel()
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	}()

	// Start monitoring loop
	for {
		stats := h.collectStats()
		statsJSON, err := json.Marshal(stats)
		if err != nil {
			continue
		}

		if err := c.Write(ctx, websocket.MessageText, statsJSON); err != nil {
			break
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second): // Update every 5 seconds
		}
	}

	return nil
}

// validHandshake is the handshake coder/websocket accepts. It is slightly
// stricter than fasthttp's upgrader (exact version "13", a valid 16-byte key),
// so a handshake no browser sends gets 426 here where fiber switched.
func validHandshake(r *http.Request) bool {
	keys := r.Header.Values("Sec-WebSocket-Key")
	if len(keys) != 1 {
		return false
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(keys[0]))
	return r.ProtoAtLeast(1, 1) && r.Method == http.MethodGet &&
		headerHasToken(r.Header, "Connection", "upgrade") &&
		headerHasToken(r.Header, "Upgrade", "websocket") &&
		r.Header.Get("Sec-WebSocket-Version") == "13" &&
		err == nil && len(key) == 16
}

func headerHasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

// IsUpgrade is gofiber's IsWebSocketUpgrade check that gates /ws.
func IsUpgrade(r *http.Request) bool {
	return headerHasToken(r.Header, "Connection", "upgrade") &&
		headerHasToken(r.Header, "Upgrade", "websocket")
}

// MonitorStats returns current monitoring stats
func (h *webSocketHandler) MonitorStats(w http.ResponseWriter, r *http.Request) error {
	stats := h.collectStats()
	return service.Response(w, http.StatusOK, true, "Current monitoring stats", stats)
}

// collectStats gathers all monitoring statistics
func (h *webSocketHandler) collectStats() MonitoringStats {
	stats := MonitoringStats{
		Timestamp: time.Now(),
	}

	// Get stats from StatsService
	if h.stats != nil {
		stats.ActiveUploads = h.stats.GetActiveUploads()
		stats.UploadSpeed = h.stats.GetUploadSpeed()
		stats.CacheHitRate = h.stats.GetCacheHitRate()
		stats.CPUUsage = h.stats.GetCPUUsage()
		stats.MemoryUsage = h.stats.GetMemoryUsage()
		stats.DiskUsage = h.stats.GetDiskUsage()
		stats.Errors = h.stats.GetRecentErrors()
	}

	return stats
}
