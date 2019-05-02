package main

import (
	"encoding/json"
	"github.com/smartmachine/crdb-operator-console/pkg/controller"
	"github.com/smartmachine/crdb-operator-console/pkg/web"
	log "k8s.io/klog"
	"os"
	"os/signal"
	"time"
)

//go:generate statik -c "Console React Webapp" -Z -f -p webapp -src ../../../crdb-console/build -dest ../../pkg

var ctrl *controller.Controller
var listener *web.Listener

func main() {

	pFlags := parseFlags()

	controllerKillChan := make(chan struct{})
	webserverKillChan := make(chan struct{})

	defer close(controllerKillChan)
	defer close(webserverKillChan)

	ctrl = controller.NewController(*pFlags.master, *pFlags.kubeconfig, ControllerCallback)
	listener = web.NewListener(*pFlags.staticPath, *pFlags.websocketPath, *pFlags.listenAddress, OnMessageCallback, OnConnectCallback)

	go ctrl.Run(1, controllerKillChan)
	go listener.Run(webserverKillChan)

	exitFunc(controllerKillChan, webserverKillChan)

}

func ControllerCallback(record *controller.Record) {
	jsonBytes, err := json.Marshal(record)
	if err != nil {
		log.Errorf("error marshalling json: %v", err)
	}
	listener.Hub.Broadcast <- jsonBytes
}

func OnConnectCallback(client *web.Client) {
	for _, record := range ctrl.ListCRDBs() {
		recordJSON, err := json.Marshal(record)
		if err != nil {
			log.Error("cannot marshal *Record to json: %v, %v", record, err)
		}
		client.Send <- recordJSON
	}
}

func OnMessageCallback(client *web.Client, message []byte) {
	var result map[string]interface{}
	err := json.Unmarshal(message, &result)
	if err != nil {
		log.Error("message is not json, cannot unmarshal")
		return
	}
	cmd, ok := result["cmd"].(string)
	if !ok {
		log.Error("cannot parse cmd field of command message")
		return
	}
	switch cmd {
	case "detail":
		key, ok := result["key"].(string)
		if !ok {
			log.Error("cannot parse key field of command message")
			return
		}
		record, err := ctrl.EmitObject(key)
		if err != nil {
			log.Errorf("cannot find object with key %s: %v", key, err)
		}
		recordJSON, err := json.Marshal(record)
		if err != nil {
			log.Errorf("cannot marshal *Record to json: %v, %v", record, err)
		}
		client.Send <- recordJSON
	default:
		log.Errorf("unknown command type received: %s", cmd)

	}
}

func exitFunc(killChan ...chan struct{}) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
	for _, channel := range killChan {
		channel <- struct{}{}
	}
	time.Sleep(time.Second * 5)
	log.Infof("... Exiting.")
}
