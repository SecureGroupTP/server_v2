package server

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool {
		return true
	},
}

func (g routeGroup) registerWebSocketRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			g.logger.Warn("websocket upgrade failed", "error", err)
			return
		}
		defer conn.Close()

		for {
			msgType, payload, readErr := conn.ReadMessage()
			if readErr != nil {
				return
			}

			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if writeErr := conn.WriteMessage(msgType, payload); writeErr != nil {
				return
			}
		}
	})
}
