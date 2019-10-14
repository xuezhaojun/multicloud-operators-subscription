// Copyright 2019 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package subscription

import (
	"context"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	chnv1alpha1 "github.com/IBM/multicloud-operators-channel/pkg/apis/app/v1alpha1"
	appv1alpha1 "github.com/IBM/multicloud-operators-subscription/pkg/apis/app/v1alpha1"
	nssub "github.com/IBM/multicloud-operators-subscription/pkg/subscriber/namespace"
	"github.com/IBM/multicloud-operators-subscription/pkg/utils"
)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new Subscription Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager, hubconfig *rest.Config) error {
	hubclient, err := client.New(hubconfig, client.Options{})
	if err != nil {
		klog.Error("Failed to generate client to hub cluster with error:", err)
		return err
	}

	subs := make(map[string]appv1alpha1.Subscriber)

	if nssub.GetDefaultSubscriber() == nil {
		errmsg := "default namespace subscriber is not initialized"
		klog.Error(errmsg)

		return errors.NewServiceUnavailable(errmsg)
	}

	subs[chnv1alpha1.ChannelTypeNamespace] = nssub.GetDefaultSubscriber()

	return add(mgr, newReconciler(mgr, hubclient, subs))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, hubclient client.Client, subscribers map[string]appv1alpha1.Subscriber) reconcile.Reconciler {
	rec := &ReconcileSubscription{
		Client:      mgr.GetClient(),
		scheme:      mgr.GetScheme(),
		hubclient:   hubclient,
		subscribers: subscribers,
	}

	return rec
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("subscription-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Subscription
	err = c.Watch(&source.Kind{Type: &appv1alpha1.Subscription{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileSubscription implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileSubscription{}

// ReconcileSubscription reconciles a Subscription object
type ReconcileSubscription struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client.Client
	hubclient   client.Client
	scheme      *runtime.Scheme
	subscribers map[string]appv1alpha1.Subscriber
}

// Reconcile reads that state of the cluster for a Subscription object and makes changes based on the state read
// and what is in the Subscription.Spec
func (r *ReconcileSubscription) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	klog.Info("Reconciling: ", request.NamespacedName)

	instance := &appv1alpha1.Subscription{}
	err := r.Get(context.TODO(), request.NamespacedName, instance)

	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, delete existing subscriberitem if any
			for _, sub := range r.subscribers {
				_ = sub.UnsubscribeItem(request.NamespacedName)
			}

			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	_ = r.doReconcile(instance)

	return reconcile.Result{}, nil
}

func (r *ReconcileSubscription) doReconcile(instance *appv1alpha1.Subscription) error {
	var err error

	subitem := &appv1alpha1.SubsriberItem{}
	subitem.Subscription = instance

	subitem.Channel = &chnv1alpha1.Channel{}
	chnkey := utils.NamespacedNameFormat(instance.Spec.Channel)
	err = r.hubclient.Get(context.TODO(), chnkey, subitem.Channel)

	if err != nil {
		klog.Error("Failed to get channel of subscription:", instance)
		return err
	}

	if instance.Spec.PackageFilter != nil && instance.Spec.PackageFilter.FilterRef != nil {
		subitem.SubscriptionConfigMap = &v1.ConfigMap{}
		subcfgkey := types.NamespacedName{
			Name:      instance.Spec.PackageFilter.FilterRef.Name,
			Namespace: instance.Namespace,
		}

		err = r.Get(context.TODO(), subcfgkey, subitem.SubscriptionConfigMap)
		if err != nil {
			klog.Error("Failed to get secret of subsciption, error: ", err)
			return err
		}
	}

	if subitem.Channel.Spec.SecretRef != nil {
		subitem.ChannelSecret = &v1.Secret{}
		chnseckey := types.NamespacedName{
			Name:      subitem.Channel.Spec.SecretRef.Name,
			Namespace: subitem.Channel.Namespace,
		}

		err = r.hubclient.Get(context.TODO(), chnseckey, subitem.ChannelSecret)
		if err != nil {
			klog.Error("Failed to get secret of channel, error: ", err)
			return err
		}
	}

	if subitem.Channel.Spec.ConfigMapRef != nil {
		subitem.ChannelConfigMap = &v1.ConfigMap{}
		chncfgkey := types.NamespacedName{
			Name:      subitem.Channel.Spec.ConfigMapRef.Name,
			Namespace: subitem.Channel.Namespace,
		}

		err = r.hubclient.Get(context.TODO(), chncfgkey, subitem.ChannelConfigMap)
		if err != nil {
			klog.Error("Failed to get configmap of channel, error: ", err)
			return err
		}
	}

	subtype := strings.ToLower(string(subitem.Channel.Spec.Type))

	// subscribe it with right channel type and unsubscribe from other channel types (in case user modify channel type)
	for k, sub := range r.subscribers {
		action := "subscribe"

		if k == subtype {
			err = sub.SubscribeItem(subitem)
		} else {
			err = sub.UnsubscribeItem(types.NamespacedName{Name: subitem.Subscription.Name, Namespace: subitem.Subscription.Namespace})
			action = "unsubscribe"
		}

		if err != nil {
			klog.Error("Failed to ", action, " with subscriber ", k, " error:", err)
		}
	}

	return nil
}