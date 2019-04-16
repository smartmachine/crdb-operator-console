package controller

import (
	"fmt"
	"github.com/smartmachine/crdb-operator/pkg/apis/db/v1alpha1"
	crdbclient "github.com/smartmachine/crdb-operator/pkg/client/clientset/versioned"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
	log "k8s.io/klog"
	"time"
)

type Controller struct {
	clientset *crdbclient.Clientset
	HasSynced bool
	callback  func (record *Record)

	informer  cache.Controller
	indexer   cache.Indexer
	queue     workqueue.RateLimitingInterface
}

type RecordType string

const DELETE RecordType = "delete"
const RECORD RecordType = "record"

type Record struct {
	Crud   RecordType                 `json:"crud"`
	Key    string                     `json:"key"`
	Data   interface{}                `json:"data,omitempty"`
}

func NewController(master string, kubeconfig string, callback func(record *Record)) (controller *Controller) {

	crdbc := &Controller{
		clientset: clientset(master, kubeconfig),
		callback:  callback,
	}

	crdbListWatcher := cache.NewListWatchFromClient(crdbc.clientset.DbV1alpha1().RESTClient(), "cockroachdbs", v1.NamespaceAll, fields.Everything())
	crdbc.queue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	crdbc.indexer, crdbc.informer = cache.NewIndexerInformer(crdbListWatcher, &v1alpha1.CockroachDB{}, 0,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				key, err := cache.MetaNamespaceKeyFunc(obj)
				if err == nil {
					crdbc.queue.Add(key)
				}
			},
			UpdateFunc: func(oldobj interface{}, newobj interface{}) {
				if newobj.(*v1alpha1.CockroachDB).GetDeletionTimestamp() == nil &&
					oldobj.(*v1alpha1.CockroachDB).GetResourceVersion() != newobj.(*v1alpha1.CockroachDB).GetResourceVersion() {
					key, err := cache.MetaNamespaceKeyFunc(newobj)
					if err == nil {
						crdbc.queue.Add(key)
					}
				}
			},
			DeleteFunc: func(obj interface{}) {
				key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
				if err == nil {
					crdbc.queue.Add(key)
				}
			},
		}, cache.Indexers{},
	)

	return crdbc
}

func clientset(master string, kubeconfig string) *crdbclient.Clientset {
	config, err := clientcmd.BuildConfigFromFlags(master, kubeconfig)
	if err != nil {
		log.Fatalf("Error building kubeconfig: %v", err.Error())
	}

	// create the clientset
	clientset, err := crdbclient.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating crdb client: %v", err.Error())
	}
	return clientset
}


// Run starts the CRDB Controller and blocks until killed
func (crdbc *Controller) Run(threadiness int, killChan chan struct{}) {

	shutdown := make(chan struct{})
	defer close(shutdown)
	defer runtime.HandleCrash()
	defer crdbc.queue.ShutDown()

	go crdbc.informer.Run(shutdown)

	log.Info("Waiting for initial CockroachDB sync")

	// Wait for store to sync up before processing crdbs
	if !cache.WaitForCacheSync(killChan, crdbc.informer.HasSynced) {
		runtime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}

	crdbc.HasSynced = true

	for i := 0; i < threadiness; i++ {
		go wait.Until(crdbc.runCRDBQueueWorker, time.Second, shutdown)
	}

	log.Info("Initial CRDB sync complete")
	<-killChan
	log.Info("Stopping controller ...")
	shutdown <- struct{}{}
}


func (crdbc *Controller) runCRDBQueueWorker() {
	for crdbc.processNextCRDB() {
	}
}

func (crdbc *Controller) processNextCRDB() bool {
	// Wait until there is a new item in the working queue
	key, quit := crdbc.queue.Get()
	if quit {
		return false
	}
	// Tell the queue that we are done with processing this key. This unblocks the key for other workers
	// This allows safe parallel processing because two crdbs with the same key are never processed in
	// parallel.
	defer crdbc.queue.Done(key)

	// Invoke the method containing the business logic
	err := crdbc.processCRDB(key.(string))
	// Handle the error if something went wrong during the execution of the business logic
	crdbc.handleErr(err, key)
	return true
}

func (crdbc *Controller) processCRDB(key string) error {
	obj, exists, err := crdbc.indexer.GetByKey(key)
	if err != nil {
		log.Infof("Fetching object with key %s from store failed with %v", key, err)
		return err
	}

	record := &Record{ Key: key }

	if exists {
		crdb, ok := obj.(*v1alpha1.CockroachDB)
		if !ok {
			return  fmt.Errorf("unable to convert type %t, object %+v to object of type v1alpha1/CockroachDB", obj, obj)
		}
		record.Crud = RECORD
		record.Data = *crdb
	} else {
		record.Crud = DELETE
	}

	crdbc.handleCRDB(record)
	return nil
}

// handleErr checks if an error happened and makes sure we will retry later.
func (crdbc *Controller) handleErr(err error, key interface{}) {
	if err == nil {
		// Forget about the #AddRateLimited history of the key on every successful synchronization.
		// This ensures that future processing of updates for this key is not delayed because of
		// an outdated error history.
		crdbc.queue.Forget(key)
		return
	}

	// This controller retries 5 times if something goes wrong. After that, it stops trying.
	if crdbc.queue.NumRequeues(key) < 5 {
		log.Errorf("Error syncing crdb %v: %v", key, err)

		// Re-enqueue the key rate limited. Based on the rate limiter on the
		// queue and the re-enqueue history, the key will be processed later again.
		crdbc.queue.AddRateLimited(key)
		return
	}

	crdbc.queue.Forget(key)
	// Report to an external entity that, even after several retries, we could not successfully process this key
	runtime.HandleError(err)
	log.Errorf("Dropping crdb %q out of the queue: %v", key, err)
}

func (crdbc *Controller) handleCRDB(record *Record) {
	crdbc.callback(record)
}

func (crdbc *Controller) ListCRDBs() []*Record {
	var records []*Record

	for _, key := range crdbc.indexer.ListKeys() {
		record, err := crdbc.EmitObject(key)
		if err == nil {
			records = append(records, record)
		}
	}
	return records
}

func (crdbc *Controller) EmitObject(key string) (*Record, error) {
	obj, exists, err := crdbc.indexer.GetByKey(key)
	if err != nil {
		log.Infof("Fetching object with key %s from store failed with %v", key, err)
		return nil, err
	}

	if !exists {
		return nil, fmt.Errorf("object %s doesn't exist in store", key)
	}

	crdb, ok := obj.(*v1alpha1.CockroachDB)
	if !ok {
		return nil, fmt.Errorf("unable to convert interface %v to object of type v1alpha1/CockroachDB", obj)
	}

	record := &Record{
		Key: key,
		Crud: RECORD,
		Data: *crdb,
	}

	return record, nil
}