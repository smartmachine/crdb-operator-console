package web

import log "k8s.io/klog"

type OnMessageCallback func(client *Client, message []byte)
type OnConnectCallback func(client *Client)

type Hub struct{
	clients    map[*Client]bool
	Broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	onMessage  OnMessageCallback
	onConnect  OnConnectCallback

}

func newHub(onMessage OnMessageCallback, onConnect OnConnectCallback) *Hub {
	return &Hub{
		Broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
		onMessage:  onMessage,
		onConnect:  onConnect,
	}
}

func (h *Hub) run(killChan chan struct{}) {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
			}
		case message := <-h.Broadcast:
			for client := range h.clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(h.clients, client)
				}
			}
		case <- killChan:
			log.Info("Shutting down hub ...")
			break
		}
	}
}
