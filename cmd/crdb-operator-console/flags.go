package main

import (
	"flag"
	log "k8s.io/klog"
	"os"
	"path/filepath"
)

type ProgramFlags struct {
	master        *string
	kubeconfig    *string
	staticPath    *string
	websocketPath *string
	listenAddress *string
}

func parseFlags() *ProgramFlags {
	pFlags := &ProgramFlags{}

	log.InitFlags(nil)
	err := flag.Set("logtostderr", "true")
	if err != nil {
		log.Fatalf("error setting logging flags %v", err)
	}

	if home := homeDir(); home != "" {
		pFlags.kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		pFlags.kubeconfig = flag.String("kubeconfig", "", "(optional if inside cluster) absolute path to the kubeconfig file")
	}
	pFlags.master = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")

	pFlags.staticPath = flag.String("staticPath", "/console", "The context path of the console webapp")
	pFlags.websocketPath = flag.String("websocketPath", "/ws", "The context path of the websocket service")
	pFlags.listenAddress = flag.String("listenAddress", ":8080", "The listen address of the webserver")

	flag.Parse()
	return pFlags
}

func homeDir() string {
	return os.Getenv("HOME")
}
