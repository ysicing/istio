// Copyright Istio Authors
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

package route

import (
	"crypto/md5"
	"encoding/hex"
	"strconv"
	"strings"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
)

// Cache includes the variables that can influence a Route Configuration.
// Implements XdsCacheEntry interface.
type Cache struct {
	RouteName string

	ProxyVersion string
	// proxy cluster ID
	ClusterID string
	// proxy dns domain
	DNSDomain string
	// DNSCapture indicates whether the workload has enabled dns capture
	DNSCapture bool
	// DNSAutoAllocate indicates whether the workload should have auto allocated addresses for ServiceEntry
	// This allows resolving ServiceEntries, which is especially useful for distinguishing TCP traffic
	// This depends on DNSCapture.
	DNSAutoAllocate bool

	ListenerPort     int
	Services         []*model.Service
	VirtualServices  []config.Config
	DestinationRules []*config.Config
	EnvoyFilterKeys  []string
	// Push version is a very broad key. Any config key will invalidate it. Its still valuable to cache,
	// as that means we can generate a cluster once and send it to all proxies, rather than N times for N proxies.
	// Hypothetically we could get smarter and determine the exact set of all configs we use and their versions,
	// which we probably will need for proper delta XDS, but for now this is sufficient.
	PushVersion string
}

func (r *Cache) Cacheable() bool {
	if r == nil {
		return false
	}
	if r.ListenerPort == 0 {
		return false
	}

	for _, config := range r.VirtualServices {
		vs := config.Spec.(*networking.VirtualService)
		for _, httpRoute := range vs.Http {
			for _, match := range httpRoute.Match {
				// if vs has source match, not cacheable
				if len(match.SourceLabels) > 0 || match.SourceNamespace != "" {
					return false
				}
			}
		}
	}

	return true
}

func (r *Cache) DependentConfigs() []model.ConfigKey {
	configs := make([]model.ConfigKey, len(r.Services)+len(r.VirtualServices)+len(r.DestinationRules))
	for _, svc := range r.Services {
		configs = append(configs, model.ConfigKey{Kind: gvk.ServiceEntry, Name: string(svc.Hostname), Namespace: svc.Attributes.Namespace})
	}
	for _, vs := range r.VirtualServices {
		configs = append(configs, model.ConfigKey{Kind: gvk.VirtualService, Name: vs.Name, Namespace: vs.Namespace})
	}
	for _, dr := range r.DestinationRules {
		configs = append(configs, model.ConfigKey{Kind: gvk.DestinationRule, Name: dr.Name, Namespace: dr.Namespace})
	}

	for _, efKey := range r.EnvoyFilterKeys {
		items := strings.Split(efKey, "/")
		configs = append(configs, model.ConfigKey{Kind: gvk.EnvoyFilter, Name: items[1], Namespace: items[0]})
	}
	return configs
}

func (r *Cache) DependentTypes() []config.GroupVersionKind {
	return nil
}

func (r *Cache) Key() string {
	params := []string{
		r.RouteName, r.ProxyVersion, r.ClusterID, r.DNSDomain,
		strconv.FormatBool(r.DNSCapture), strconv.FormatBool(r.DNSAutoAllocate),
		r.PushVersion,
	}
	for _, svc := range r.Services {
		params = append(params, string(svc.Hostname)+"/"+svc.Attributes.Namespace)
	}
	for _, vs := range r.VirtualServices {
		params = append(params, vs.Name+"/"+vs.Namespace)
	}
	for _, dr := range r.DestinationRules {
		params = append(params, dr.Name+"/"+dr.Namespace)
	}
	params = append(params, r.EnvoyFilterKeys...)

	hash := md5.New()
	for _, param := range params {
		hash.Write([]byte(param))
	}
	sum := hash.Sum(nil)
	return hex.EncodeToString(sum)
}
