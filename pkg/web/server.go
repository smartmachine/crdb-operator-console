package web

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/rakyll/statik/fs"
	_ "github.com/smartmachine/crdb-operator-console/pkg/webapp"
	log "k8s.io/klog"
	"net/http"
	"time"
)


type Listener struct {
	server *http.Server
	engine *gin.Engine
	Hub    *Hub
}

func NewListener(staticPath string, websocketPath string, listenAddress string, onMessage OnMessageCallback, onConnect OnConnectCallback) (listener *Listener) {
	listener = &Listener{}

	// Try to load static webapp as filesystem
	statikFS, err := fs.New()
	if err != nil {
		log.Fatal(err)
	}

	// Create Hub
	listener.Hub = newHub(onMessage, onConnect)

	// Instantiate Engine
	gin.SetMode(gin.ReleaseMode)
	listener.engine = gin.Default()

	// Add webapp
	listener.engine.StaticFS(staticPath, statikFS)

	// Add WebSocket Handler
	listener.engine.GET(websocketPath, func(c *gin.Context) {
		serveWs(listener.Hub, c.Writer, c.Request)
	})

	listener.server = &http.Server{
		Addr: listenAddress,
		Handler: listener.engine,
	}

	return
}

func (listener *Listener) Run(killChan chan struct{}) {
	hubChan := make(chan struct{})
	go listener.Hub.run(hubChan)
	go listener.shutdown(killChan, hubChan)
	if err:= listener.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen: %v", err)
	}
}

func (listener *Listener) shutdown(killChan chan struct{}, hubChan chan struct{}) {
	<- killChan
	hubChan <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 5 * time.Second)
	defer cancel()
	log.Info("Stopping webserver ...")
	if err := listener.server.Shutdown(ctx); err != nil {
		log.Fatal("Server Shutdown:", err)
	}
}
