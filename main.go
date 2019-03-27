package main

import (
	"flag"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rakyll/statik/fs"
	_ "github.com/smartmachine/crdb-operator-console/webapp"
	"github.com/smartmachine/crdb-operator/pkg/apis/db/v1alpha1"
	crdbclient "github.com/smartmachine/crdb-operator/pkg/client/clientset/versioned"
	informers "github.com/smartmachine/crdb-operator/pkg/client/informers/externalversions"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	log "k8s.io/klog"
	"net/http"
	"os"
	"path/filepath"
)

//go:generate statik -c "Console React Webapp" -Z -f -p webapp -src ../crdb-console/build

func main() {
	log.InitFlags(nil)

	err := flag.Set("logtostderr", "true")
	var kubeconfig *string
	if home := homeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "(optional if inside cluster) absolute path to the kubeconfig file")
	}
	master := flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags(*master, *kubeconfig)
	if err != nil {
		log.Fatalf("Error building kubeconfig: %v", err.Error())
	}

	// create the clientset
	clientset, err := crdbclient.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating crdb client: %v", err.Error())
	}

	clusters, err := clientset.DbV1alpha1().CockroachDBs("crdb").List(metav1.ListOptions{})
	if err != nil && errors.IsNotFound(err) {
		log.Fatal("No CockroachDB CRD deployed to cluster.")
	} else if err != nil {
		log.Fatalf("Error looking up crdb clusters: %v", err.Error())
	}

	for _, cluster := range clusters.Items {
		log.Infof("Found cluster %s with nodes %+v", cluster.Name, cluster.Status.Nodes)
	}

	factory := informers.NewSharedInformerFactory(clientset, 0)
	informer := factory.Db().V1alpha1().CockroachDBs().Informer()
	stopper := make(chan struct{})
	defer close(stopper)

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			mObj := obj.(*v1alpha1.CockroachDB)
			log.Infof("Added new CockroachDB Cluster: %s", mObj.Name)
		},
		UpdateFunc: func(old interface{}, new interface{}) {
			oldDB := old.(*v1alpha1.CockroachDB)
			newDB := new.(*v1alpha1.CockroachDB)
			if oldDB.ResourceVersion == newDB.ResourceVersion {
				log.Infof("Received heartbeat event for CockroachDB Cluster: %s", newDB.Name)
				return
			}
		},
		DeleteFunc: func(obj interface{}) {
			mObj := obj.(*v1alpha1.CockroachDB)
			log.Infof("Deleted CockroachDB Cluster: %s", mObj.Name)
		},
	})

	go informer.Run(stopper)

	statikFS, err := fs.New()
	if err != nil {
		log.Fatal(err)
	}

	r := gin.Default()

	r.StaticFS("/console", statikFS)
	r.GET("/ws", func(c *gin.Context) {
		wshandler(c.Writer, c.Request)
	})

	err = r.Run()
	if err != nil {
		log.Fatal(err)
	}

}

var wsupgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func wshandler(w http.ResponseWriter, r *http.Request) {
	conn, err := wsupgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Printf("Failed to set websocket upgrade: %+v", err)
		return
	}

	for {
		t, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		err = conn.WriteMessage(t, msg)
		if err != nil {
			break
		}
	}
}

func homeDir() string {
	return os.Getenv("HOME")
}
