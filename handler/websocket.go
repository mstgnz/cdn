package handler

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	"github.com/mstgnz/cdn/service"
)

type WebSocketHandler interface {
	HandleWebSocket(c *websocket.Conn) error
	MonitorStats(c *fiber.Ctx) error
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

// HandleWebSocket handles WebSocket connections
func (h *webSocketHandler) HandleWebSocket(c *websocket.Conn) error {
	// Register client
	h.clientsMux.Lock()
	h.clients[c] = true
	h.clientsMux.Unlock()

	// Cleanup on disconnect
	defer func() {
		h.clientsMux.Lock()
		delete(h.clients, c)
		h.clientsMux.Unlock()
		c.Close()
	}()

	// Start monitoring loop
	for {
		stats := h.collectStats()
		statsJSON, err := json.Marshal(stats)
		if err != nil {
			continue
		}

		if err := c.WriteMessage(websocket.TextMessage, statsJSON); err != nil {
			break
		}

		time.Sleep(time.Second * 5) // Update every 5 seconds
	}

	return nil
}

// MonitorStats returns current monitoring stats
func (h *webSocketHandler) MonitorStats(c *fiber.Ctx) error {
	stats := h.collectStats()
	return service.Response(c, fiber.StatusOK, true, "Current monitoring stats", stats)
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
