// Copyright 2016 IBM Corporation
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

package kubernetes

import (
	"fmt"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"

	"github.com/amalgam8/registry/auth"
	"github.com/amalgam8/registry/store"
	"github.com/amalgam8/registry/utils/logging"
)

const (
	module string = "K8SCATALOG"
)

type serviceMap map[string][]*store.ServiceInstance // service name -> instance list
type instanceMap map[string]*store.ServiceInstance  // instance ID -> instance

type k8sCatalog struct {
	namespace auth.Namespace
	client    *k8sClient

	services  serviceMap
	instances instanceMap

	logger *log.Entry
	sync.RWMutex
}

func newK8sCatalog(namespace auth.Namespace, client *k8sClient) (*k8sCatalog, error) {
	catalog := &k8sCatalog{
		services:  serviceMap{},
		instances: instanceMap{},
		namespace: namespace,
		client:    client,
		logger:    logging.GetLogger(module),
	}

	catalog.refresh()

	// TODO: more efficient implementation using Kubernetes API watch interface
	ticker := time.NewTicker(10 * time.Second)
	go func() {
		for _ = range ticker.C {
			catalog.refresh()
		}
	}()

	return catalog, nil
}

func (kc *k8sCatalog) ListServices(predicate store.Predicate) []*store.Service {
	kc.RLock()
	defer kc.RUnlock()

	serviceCollection := make([]*store.Service, 0, len(kc.services))
	for service, instances := range kc.services {
		for _, inst := range instances {
			if predicate == nil || predicate(inst) {
				serviceCollection = append(serviceCollection, &store.Service{ServiceName: service})
				break
			}
		}
	}

	return serviceCollection
}

func (kc *k8sCatalog) List(serviceName string, predicate store.Predicate) ([]*store.ServiceInstance, error) {
	kc.RLock()
	defer kc.RUnlock()

	service := kc.services[serviceName]
	if nil == service {
		return nil, store.NewError(store.ErrorNoSuchServiceName, "no such service ", serviceName)
	}

	instanceCollection := make([]*store.ServiceInstance, 0, len(service))
	for _, inst := range service {
		if predicate == nil || predicate(inst) {
			instanceCollection = append(instanceCollection, inst.DeepClone())
		}
	}
	return instanceCollection, nil
}

func (kc *k8sCatalog) Instance(instanceID string) (*store.ServiceInstance, error) {
	kc.RLock()
	defer kc.RUnlock()

	instance := kc.instances[instanceID]
	if instance == nil {
		return nil, store.NewError(store.ErrorNoSuchServiceInstance, "no such service instance", instanceID)
	}
	return instance.DeepClone(), nil
}

func (kc *k8sCatalog) Register(si *store.ServiceInstance) (*store.ServiceInstance, error) {
	kc.logger.Infof("Unsupported API (Register) called")
	return nil, store.NewError(store.ErrorBadRequest, "Read-only Catalog: API Not Supported", "Register")
}

func (kc *k8sCatalog) Deregister(instanceID string) error {
	kc.logger.Infof("Unsupported API (Deregister) called")
	return store.NewError(store.ErrorBadRequest, "Read-only Catalog: API Not Supported", "Deregister")
}

func (kc *k8sCatalog) Renew(instanceID string) error {
	kc.logger.Infof("Unsupported API (Renew) called")
	return store.NewError(store.ErrorBadRequest, "Read-only Catalog: API Not Supported", "Renew")
}

func (kc *k8sCatalog) SetStatus(instanceID, status string) error {
	kc.logger.Infof("Unsupported API (SetStatus) called")
	return store.NewError(store.ErrorBadRequest, "Read-only Catalog: API Not Supported", "SetStatus")
}

func (kc *k8sCatalog) refresh() {
	kc.Lock()
	defer kc.Unlock()

	services, instances, err := kc.getServices()
	if err == nil {
		kc.services = services
		kc.instances = instances
	}
}

func (kc *k8sCatalog) getServices() (serviceMap, instanceMap, error) {
	services := serviceMap{}
	instances := instanceMap{}
	endpointsList, err := kc.client.getEndpointsList(kc.namespace)
	if err != nil {
		kc.logger.Warnf("Unable to get endpoints: %s", err)
		return services, instances, err
	}
	for _, endpoints := range endpointsList.Items {
		sname := endpoints.ObjectMeta.Name
		insts := []*store.ServiceInstance{}
		for _, subset := range endpoints.Subsets {
			for _, address := range subset.Addresses {
				for _, port := range subset.Ports {
					var endpointType string
					switch port.Protocol {
					case ProtocolUDP:
						endpointType = "udp"
					case ProtocolTCP:
						endpointType = "tcp"
					}
					endpointValue := fmt.Sprintf("%s:%d", address.IP, port.Port)
					var uid string
					if address.TargetRef != nil {
						uid = address.TargetRef.UID
					} else {
						uid = address.IP
					}
					inst := &store.ServiceInstance{
						ID:          fmt.Sprintf("%s-%d", uid, port.Port),
						ServiceName: sname,
						Endpoint:    &store.Endpoint{Type: endpointType, Value: endpointValue},
						Status:      "UP",
						Metadata:    []byte(fmt.Sprintf("{\"kubernetes_url\":\"%s/%s\"}", kc.client.getEndpointsURL(kc.namespace), sname)),
						Tags:        []string{"kubernetes"},
						TTL:         0}
					insts = append(insts, inst)
					instances[inst.ID] = inst
				}
			}
			services[sname] = insts
		}
	}
	return services, instances, nil
}
