/*
 * Copyright 2020, EnMasse authors.
 * License: Apache License 2.0 (see the file LICENSE or http://apache.org/licenses/LICENSE-2.0.html).
 */

package messaginginfra

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/enmasseproject/enmasse/pkg/amqpcommand"
	v1beta2 "github.com/enmasseproject/enmasse/pkg/apis/enmasse/v1beta2"
	"github.com/enmasseproject/enmasse/pkg/controller/messaginginfra/broker"
	"github.com/enmasseproject/enmasse/pkg/controller/messaginginfra/cert"
	"github.com/enmasseproject/enmasse/pkg/controller/messaginginfra/common"
	"github.com/enmasseproject/enmasse/pkg/controller/messaginginfra/router"
	"github.com/enmasseproject/enmasse/pkg/controller/messagingtenant"
	"github.com/enmasseproject/enmasse/pkg/state"
	. "github.com/enmasseproject/enmasse/pkg/state/common"
	"github.com/enmasseproject/enmasse/pkg/util"
	utilerrors "github.com/enmasseproject/enmasse/pkg/util/errors"
	"github.com/enmasseproject/enmasse/pkg/util/finalizer"

	logr "github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	k8errors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/client-go/tools/record"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	//"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_messaginginfra")
var _ reconcile.Reconciler = &ReconcileMessagingInfra{}

const (
	FINALIZER_NAME = "enmasse.io/operator"
)

type ReconcileMessagingInfra struct {
	client           client.Client
	reader           client.Reader
	recorder         record.EventRecorder
	scheme           *runtime.Scheme
	certController   *cert.CertController
	routerController *router.RouterController
	brokerController *broker.BrokerController
	clientManager    state.ClientManager
	namespace        string
}

// Gets called by parent "init", adding as to the manager
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

/**
 * - TODO Make certificate expiry configurable
 * - TODO Add podTemplate configuration to CRD in .spec.router.podTemplate and .spec.broker.podTemplate and apply
 * - TODO Add static router configuration options in .spec.router: thread pool, min available, max unavailable, link capacity, idle timeout
 * - TODO Add static broker configuration options in .spec.broker: minAvailable, maxUnavailable, storage capacity, java options, address full policy, global max size, storage class name, resize persistent volume claim, threadpool config, global address policies
 */

func newReconciler(mgr manager.Manager) *ReconcileMessagingInfra {
	namespace := util.GetEnvOrDefault("NAMESPACE", "enmasse-infra")

	// TODO: Make expiry configurable
	clientManager := state.GetClientManager()
	certController := cert.NewCertController(mgr.GetClient(), mgr.GetScheme(), 24*30*time.Hour, 24*time.Hour)
	brokerController := broker.NewBrokerController(mgr.GetClient(), mgr.GetScheme(), certController)
	routerController := router.NewRouterController(mgr.GetClient(), mgr.GetScheme(), certController)
	return &ReconcileMessagingInfra{
		client:           mgr.GetClient(),
		certController:   certController,
		routerController: routerController,
		brokerController: brokerController,
		clientManager:    clientManager,
		reader:           mgr.GetAPIReader(),
		recorder:         mgr.GetEventRecorderFor("messaginginfra"),
		scheme:           mgr.GetScheme(),
		namespace:        namespace,
	}
}

func add(mgr manager.Manager, r *ReconcileMessagingInfra) error {

	c, err := controller.New("messaginginfra-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &v1beta2.MessagingInfrastructure{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch changes to statefulsets for underlying routers and brokers
	err = c.Watch(&source.Kind{Type: &appsv1.StatefulSet{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &v1beta2.MessagingInfrastructure{},
	})
	if err != nil {
		return err
	}

	// Watch changes to secrets that we own
	err = c.Watch(&source.Kind{Type: &corev1.Secret{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &v1beta2.MessagingInfrastructure{},
	})
	if err != nil {
		return err
	}

	// Watch pods that map to this infra
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestsFromMapFunc{
		ToRequests: handler.ToRequestsFunc(func(o handler.MapObject) []reconcile.Request {
			pod := o.Object.(*corev1.Pod)
			annotations := pod.Annotations
			if annotations == nil {
				return []reconcile.Request{}
			}

			if _, exists := annotations[common.ANNOTATION_INFRA_NAME]; !exists {
				return []reconcile.Request{}
			}

			if _, exists := annotations[common.ANNOTATION_INFRA_NAMESPACE]; !exists {
				return []reconcile.Request{}
			}
			return []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      pod.Annotations[common.ANNOTATION_INFRA_NAME],
						Namespace: pod.Annotations[common.ANNOTATION_INFRA_NAMESPACE],
					},
				},
			}
		}),
	}, &infraPodPredicate{})
	if err != nil {
		return err
	}

	/*
		// Watch changes to tenants to retrigger infra sync
		err = c.Watch(&source.Kind{Type: &v1beta2.MessagingTenant{}}, &handler.EnqueueRequestsFromMapFunc{
			ToRequests: handler.ToRequestsFunc(func(o handler.MapObject) []reconcile.Request {
				tenant := o.Object.(*v1beta2.MessagingTenant)
				if tenant.Status.MessagingInfraRef != nil {
					return []reconcile.Request{
						{
							NamespacedName: types.NamespacedName{
								Name:      tenant.Status.MessagingInfraRef.Name,
								Namespace: tenant.Status.MessagingInfraRef.Namespace,
							},
						},
					}
				} else {
					return []reconcile.Request{}
				}
			}),
		})
		if err != nil {
			return err
		}

		// Watch changes to addresses that map to our infra
		err = c.Watch(&source.Kind{Type: &v1beta2.MessagingAddress{}}, &handler.EnqueueRequestsFromMapFunc{
			ToRequests: handler.ToRequestsFunc(func(o handler.MapObject) []reconcile.Request {
				address := o.Object.(*v1beta2.MessagingAddress)
				tenant := &v1beta2.MessagingTenant{}
				err := r.client.Get(context.Background(), client.ObjectKey{Name: messagingtenant.TENANT_RESOURCE_NAME, Namespace: address.Namespace}, tenant)
				if err != nil || tenant.Status.MessagingInfraRef == nil {
					// Skip triggering if we can't find tenant
					return []reconcile.Request{}
				}

				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Namespace: tenant.Status.MessagingInfraRef.Namespace,
							Name:      tenant.Status.MessagingInfraRef.Name,
						},
					},
				}
			}),
		})
		if err != nil {
			return err
		}

		// Watch changes to endpoints that map to our infra
		err = c.Watch(&source.Kind{Type: &v1beta2.MessagingEndpoint{}}, &handler.EnqueueRequestsFromMapFunc{
			ToRequests: handler.ToRequestsFunc(func(o handler.MapObject) []reconcile.Request {
				endpoint := o.Object.(*v1beta2.MessagingEndpoint)
				tenant := &v1beta2.MessagingTenant{}
				err := r.client.Get(context.Background(), client.ObjectKey{Name: messagingtenant.TENANT_RESOURCE_NAME, Namespace: endpoint.Namespace}, tenant)
				if err != nil || tenant.Status.MessagingInfraRef == nil {
					// Skip triggering if we can't find tenant
					return []reconcile.Request{}
				}

				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Namespace: tenant.Status.MessagingInfraRef.Namespace,
							Name:      tenant.Status.MessagingInfraRef.Name,
						},
					},
				}
			}),
		})
	*/

	return err
}

type infraPodPredicate struct {
	predicate.Funcs
}

// Predicate that only matches pods that have annotations of their infra set
func (infraPodPredicate) Update(e event.UpdateEvent) bool {
	if e.MetaOld == nil {
		log.Error(nil, "UpdateEvent has no old metadata", "event", e)
		return false
	}

	if e.MetaOld.GetAnnotations() == nil {
		return false
	}
	annotations := e.MetaOld.GetAnnotations()

	if _, exists := annotations[common.ANNOTATION_INFRA_NAME]; !exists {
		return false
	}

	if _, exists := annotations[common.ANNOTATION_INFRA_NAMESPACE]; !exists {
		return false
	}
	return true
}

func (infraPodPredicate) Delete(e event.DeleteEvent) bool {
	if e.Meta == nil {
		log.Error(nil, "DeleteEvent has no metadata", "event", e)
		return false
	}

	if e.Meta.GetAnnotations() == nil {
		return false
	}
	annotations := e.Meta.GetAnnotations()

	if _, exists := annotations[common.ANNOTATION_INFRA_NAME]; !exists {
		return false
	}

	if _, exists := annotations[common.ANNOTATION_INFRA_NAMESPACE]; !exists {
		return false
	}
	return true
}

func (r *ReconcileMessagingInfra) Reconcile(request reconcile.Request) (reconcile.Result, error) {

	logger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)

	ctx := context.Background()

	logger.Info("Reconciling MessagingInfra")

	found := &v1beta2.MessagingInfrastructure{}
	err := r.reader.Get(ctx, request.NamespacedName, found)
	if err != nil {
		if k8errors.IsNotFound(err) {
			logger.Info("MessagingInfra resource not found. Ignoring since object must be deleted")
			return reconcile.Result{}, nil
		}
		logger.Error(err, "Failed to get MessagingInfra")
		return reconcile.Result{}, err
	}

	fresult, err := r.reconcileFinalizers(ctx, logger, found)
	if err != nil || fresult.Requeue || fresult.Return {
		return reconcile.Result{Requeue: fresult.Requeue}, err
	}

	// Start regular processing loop
	rc := &resourceContext{
		ctx:    ctx,
		client: r.client,
		infra:  found,
		status: found.Status.DeepCopy(),
	}

	var ready *v1beta2.MessagingInfrastructureCondition
	var caCreated *v1beta2.MessagingInfrastructureCondition
	var certCreated *v1beta2.MessagingInfrastructureCondition
	var brokersCreated *v1beta2.MessagingInfrastructureCondition
	var routersCreated *v1beta2.MessagingInfrastructureCondition
	var brokersConnected *v1beta2.MessagingInfrastructureCondition
	var synchronized *v1beta2.MessagingInfrastructureCondition

	// Initialize status
	result, err := rc.Process(func(infra *v1beta2.MessagingInfrastructure) (processorResult, error) {
		if infra.Status.Phase == "" || infra.Status.Phase == v1beta2.MessagingInfrastructurePending {
			infra.Status.Phase = v1beta2.MessagingInfrastructureConfiguring
		}

		ready = infra.Status.GetMessagingInfrastructureCondition(v1beta2.MessagingInfrastructureReady)
		caCreated = infra.Status.GetMessagingInfrastructureCondition(v1beta2.MessagingInfrastructureCaCreated)
		certCreated = infra.Status.GetMessagingInfrastructureCondition(v1beta2.MessagingInfrastructureCertCreated)
		brokersCreated = infra.Status.GetMessagingInfrastructureCondition(v1beta2.MessagingInfrastructureBrokersCreated)
		routersCreated = infra.Status.GetMessagingInfrastructureCondition(v1beta2.MessagingInfrastructureRoutersCreated)
		brokersConnected = infra.Status.GetMessagingInfrastructureCondition(v1beta2.MessagingInfrastructureBrokersConnected)
		synchronized = infra.Status.GetMessagingInfrastructureCondition(v1beta2.MessagingInfrastructureSynchronized)
		return processorResult{}, nil
	})
	if result.ShouldReturn(err) {
		return result.Result(), err
	}

	// Reconcile CA
	var infraCa *x509.CertPool = x509.NewCertPool()
	result, err = rc.Process(func(infra *v1beta2.MessagingInfrastructure) (processorResult, error) {
		ca, err := r.certController.ReconcileCa(ctx, logger, infra)
		if err != nil {
			infra.Status.Message = err.Error()
			caCreated.SetStatus(corev1.ConditionFalse, "", err.Error())
			return processorResult{}, err
		}
		caCreated.SetStatus(corev1.ConditionTrue, "", "")
		infraCa.AppendCertsFromPEM(ca)
		return processorResult{}, nil
	})
	if result.ShouldReturn(err) {
		return result.Result(), err
	}

	// Reconcile operator cert
	var controllerCert tls.Certificate
	result, err = rc.Process(func(infra *v1beta2.MessagingInfrastructure) (processorResult, error) {
		certInfo, err := r.certController.ReconcileCert(ctx, logger, infra, infra, fmt.Sprintf("enmasse-operator-%s", infra.Name))
		if err != nil {
			infra.Status.Message = err.Error()
			certCreated.SetStatus(corev1.ConditionFalse, "", err.Error())
			return processorResult{}, err
		}
		certCreated.SetStatus(corev1.ConditionTrue, "", "")
		controllerCert = *certInfo.Certificate
		return processorResult{}, nil
	})
	if result.ShouldReturn(err) {
		return result.Result(), err
	}

	// Reconcile Routers
	var runningRouters []Host
	var routerHosts []Host
	result, err = rc.Process(func(infra *v1beta2.MessagingInfrastructure) (processorResult, error) {
		hosts, err := r.routerController.ReconcileRouters(ctx, logger, infra)
		if err != nil {
			infra.Status.Message = err.Error()
			routersCreated.SetStatus(corev1.ConditionFalse, "", err.Error())
			return processorResult{}, err
		}

		infra.Status.Routers = make([]v1beta2.MessagingInfrastructureStatusRouter, 0, len(hosts))
		for _, host := range hosts {
			if host.Ip != "" {
				runningRouters = append(runningRouters, host)
				infra.Status.Routers = append(infra.Status.Routers, v1beta2.MessagingInfrastructureStatusRouter{Host: host.Hostname})
			}
		}
		logger.Info("Retrieved router hosts", "hosts", hosts, "running", runningRouters)

		routerHosts = hosts
		if len(runningRouters) < len(routerHosts) {
			msg := fmt.Sprintf("%d/%d router pods running", len(runningRouters), len(routerHosts))
			infra.Status.Message = msg
			routersCreated.SetStatus(corev1.ConditionFalse, "", msg)
			return processorResult{}, nil
		}

		routersCreated.SetStatus(corev1.ConditionTrue, "", "")
		return processorResult{}, nil
	})
	if result.ShouldReturn(err) {
		return result.Result(), err
	}

	// Reconcile Brokers
	var runningBrokers []Host
	var brokerHosts []Host
	result, err = rc.Process(func(infra *v1beta2.MessagingInfrastructure) (processorResult, error) {
		hosts, err := r.brokerController.ReconcileBrokers(ctx, logger, infra)
		if err != nil {
			infra.Status.Message = err.Error()
			brokersCreated.SetStatus(corev1.ConditionFalse, "", err.Error())
			return processorResult{}, err
		}

		infra.Status.Brokers = make([]v1beta2.MessagingInfrastructureStatusBroker, 0, len(hosts))
		for _, host := range hosts {
			if host.Ip != "" {
				runningBrokers = append(runningBrokers, host)
				infra.Status.Brokers = append(infra.Status.Brokers, v1beta2.MessagingInfrastructureStatusBroker{Host: host.Hostname})
			}
		}

		logger.Info("Retrieved broker hosts", "hosts", hosts, "running", runningBrokers)

		brokerHosts = hosts
		if len(runningBrokers) < len(brokerHosts) {
			msg := fmt.Sprintf("%d/%d broker pods running", len(runningBrokers), len(brokerHosts))
			infra.Status.Message = msg
			brokersCreated.SetStatus(corev1.ConditionFalse, "", msg)
			return processorResult{}, nil
		}
		brokersCreated.SetStatus(corev1.ConditionTrue, "", "")
		return processorResult{}, nil
	})
	if result.ShouldReturn(err) {
		return result.Result(), err
	}

	// Sync all configuration
	var connectorStatuses []state.ConnectorStatus
	result, err = rc.Process(func(infra *v1beta2.MessagingInfrastructure) (processorResult, error) {
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{controllerCert},
			RootCAs:      infraCa,
		}
		statuses, err := r.clientManager.GetClient(infra).SyncAll(runningRouters, runningBrokers, tlsConfig)
		// Treat as transient error
		if errors.Is(err, amqpcommand.RequestTimeoutError) {
			logger.Info("Error syncing infra", "error", err.Error())
			// Note: status message is not set to avoid too verbose error messages
			synchronized.SetStatus(corev1.ConditionFalse, "", err.Error())
			return processorResult{RequeueAfter: 10 * time.Second}, nil
		}
		if err != nil {
			return processorResult{}, err
		}

		synchronized.SetStatus(corev1.ConditionTrue, "", "")
		connectorStatuses = statuses
		return processorResult{}, nil
	})
	if result.ShouldReturn(err) {
		return result.Result(), err
	}

	// If not all brokers and routers are not fully running, break here until they are
	result, err = rc.Process(func(infra *v1beta2.MessagingInfrastructure) (processorResult, error) {
		if len(runningBrokers) < len(brokerHosts) || len(runningRouters) < len(routerHosts) {
			return processorResult{RequeueAfter: 5 * time.Second}, nil
		}
		return processorResult{}, nil
	})
	if result.ShouldReturn(err) {
		return result.Result(), err
	}

	// Check status
	result, err = rc.Process(func(infra *v1beta2.MessagingInfrastructure) (processorResult, error) {
		expectedConnectors := len(runningRouters) * len(runningBrokers)
		if len(connectorStatuses) != expectedConnectors {
			msg := fmt.Sprintf("components not fully connected %d/%d connectors configured", len(connectorStatuses), expectedConnectors)
			infra.Status.Message = msg
			brokersConnected.SetStatus(corev1.ConditionFalse, "", msg)
			return processorResult{RequeueAfter: 10 * time.Second}, nil
		}
		for _, connectorStatus := range connectorStatuses {
			if !connectorStatus.Connected {
				msg := fmt.Sprintf("connection between %s and %s not ready: %s", connectorStatus.Router, connectorStatus.Broker, connectorStatus.Message)
				infra.Status.Message = msg
				brokersConnected.SetStatus(corev1.ConditionFalse, "", msg)
				return processorResult{RequeueAfter: 10 * time.Second}, nil
			}
		}
		brokersConnected.SetStatus(corev1.ConditionTrue, "", "")
		return processorResult{}, nil
	})
	if result.ShouldReturn(err) {
		return result.Result(), err
	}

	// Update main condition
	result, err = rc.Process(func(infra *v1beta2.MessagingInfrastructure) (processorResult, error) {
		originalStatus := infra.Status.DeepCopy()
		infra.Status.Phase = v1beta2.MessagingInfrastructureActive
		infra.Status.Message = ""
		ready.SetStatus(corev1.ConditionTrue, "", "")
		if !reflect.DeepEqual(originalStatus, infra.Status) {
			// If there was an error and the status has changed, perform an update so that
			// errors are visible to the user.
			err := r.client.Status().Update(ctx, infra)
			return processorResult{}, err
		}
		return processorResult{}, nil
	})
	if result.ShouldReturn(err) {
		return result.Result(), err
	}

	// Recheck infra state periodically
	return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
}

type finalizerResult struct {
	Requeue bool
	Return  bool
}

func (r *ReconcileMessagingInfra) reconcileFinalizers(ctx context.Context, logger logr.Logger, infra *v1beta2.MessagingInfrastructure) (finalizerResult, error) {
	// Handle finalizing an deletion state first
	if infra.DeletionTimestamp != nil && infra.Status.Phase != v1beta2.MessagingInfrastructureTerminating {
		infra.Status.Phase = v1beta2.MessagingInfrastructureTerminating
		err := r.client.Status().Update(ctx, infra)
		return finalizerResult{Requeue: true}, err
	}

	original := infra.DeepCopy()
	result, err := finalizer.ProcessFinalizers(ctx, r.client, r.reader, r.recorder, infra, []finalizer.Finalizer{
		finalizer.Finalizer{
			Name: FINALIZER_NAME,
			Deconstruct: func(c finalizer.DeconstructorContext) (reconcile.Result, error) {
				infra, ok := c.Object.(*v1beta2.MessagingInfrastructure)
				if !ok {
					return reconcile.Result{}, fmt.Errorf("provided wrong object type to finalizer, only supports MessagingInfra")
				}

				// Check if its in use by any tenants
				tenants := &v1beta2.MessagingTenantList{}
				err := r.client.List(ctx, tenants)
				if err != nil {
					return reconcile.Result{}, err
				}
				for _, tenant := range tenants.Items {
					if tenant.Status.MessagingInfrastructureRef.Name == infra.Name && tenant.Status.MessagingInfrastructureRef.Namespace == infra.Namespace {
						return reconcile.Result{}, fmt.Errorf("unable to delete MessagingInfra %s/%s: in use by MessagingTenant %s/%s", infra.Namespace, infra.Name, tenant.Namespace, tenant.Name)
					}
				}

				err = r.clientManager.DeleteClient(infra)
				return reconcile.Result{}, err
			},
		},
	})
	if err != nil {
		return finalizerResult{}, err
	}

	if result.Requeue {
		// Update and requeue if changed
		if !reflect.DeepEqual(original, infra) {
			err := r.client.Update(ctx, infra)
			return finalizerResult{Return: true}, err
		}
	}
	return finalizerResult{Requeue: result.Requeue}, nil
}

/*
 * Automatically handle status update of the resource after running some reconcile logic.
 */
type resourceContext struct {
	ctx    context.Context
	client client.Client
	status *v1beta2.MessagingInfrastructureStatus
	infra  *v1beta2.MessagingInfrastructure
}

type processorResult struct {
	Requeue      bool
	RequeueAfter time.Duration
	Return       bool
}

func (r *resourceContext) Process(processor func(infra *v1beta2.MessagingInfrastructure) (processorResult, error)) (processorResult, error) {
	result, err := processor(r.infra)
	if !reflect.DeepEqual(r.status, r.infra.Status) {
		if err != nil || result.Requeue || result.RequeueAfter > 0 {
			// If there was an error and the status has changed, perform an update so that
			// errors are visible to the user.
			statuserr := r.client.Status().Update(r.ctx, r.infra)
			if statuserr != nil {
				// If this fails, report the status error if everything else whent ok, otherwise report the original error
				log.Error(statuserr, "Status update failed", "infra", r.infra.Name)
				if err == nil {
					err = statuserr
				}
			} else {
				r.status = r.infra.Status.DeepCopy()
			}
			return result, err
		}
	}
	return result, err
}

func (r *processorResult) ShouldReturn(err error) bool {
	return err != nil || r.Requeue || r.RequeueAfter > 0 || r.Return
}

func (r *processorResult) Result() reconcile.Result {
	return reconcile.Result{Requeue: r.Requeue, RequeueAfter: r.RequeueAfter}
}

// Find the MessagingInfra servicing a given namespace
func LookupInfra(ctx context.Context, c client.Client, namespace string) (*v1beta2.MessagingInfrastructure, error) {
	// Retrieve the MessagingTenant for this namespace
	tenant := &v1beta2.MessagingTenant{}
	err := c.Get(ctx, types.NamespacedName{Name: messagingtenant.TENANT_RESOURCE_NAME, Namespace: namespace}, tenant)
	if err != nil {
		if k8errors.IsNotFound(err) {
			return nil, utilerrors.NewNotFoundError("MessagingTenant", messagingtenant.TENANT_RESOURCE_NAME, namespace)
		}
		return nil, err
	}

	if !tenant.IsBound() {
		return nil, utilerrors.NewNotBoundError(namespace)
	}

	// Retrieve the MessagingInfra for this MessagingTenant
	infra := &v1beta2.MessagingInfrastructure{}
	err = c.Get(ctx, types.NamespacedName{Name: tenant.Status.MessagingInfrastructureRef.Name, Namespace: tenant.Status.MessagingInfrastructureRef.Namespace}, infra)
	if err != nil {
		return nil, err
	}
	return infra, nil
}
