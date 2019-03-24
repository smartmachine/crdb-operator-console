package main

import (
	"flag"
	"github.com/gin-gonic/gin"
	"github.com/smartmachine/crdb-operator/pkg/apis/db/v1alpha1"
	crdbclient "github.com/smartmachine/crdb-operator/pkg/client/clientset/versioned"
	informers "github.com/smartmachine/crdb-operator/pkg/client/informers/externalversions"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	log "k8s.io/klog"
	"os"
	"path/filepath"
)

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

	informer.Run(stopper)

	r := gin.Default()
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
			"hasSynced": informer.HasSynced(),
		})
	})

	r.Run()

}

func homeDir() string {
	return os.Getenv("HOME")
}