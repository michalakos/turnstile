package health

import (
	"net/http"

	"github.com/redis/go-redis/v9"
)

type Handler struct {
	redisClient *redis.Client
}

func New(redisClient *redis.Client) *Handler {
	return &Handler{redisClient: redisClient}
}

func (h *Handler) Live(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	if err := h.redisClient.Ping(r.Context()).Err(); err != nil {
		http.Error(w, "redis unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}
