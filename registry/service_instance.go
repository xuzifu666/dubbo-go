/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package registry

import (
	"encoding/json"
	url2 "net/url"
	"strconv"

	"github.com/dubbogo/gost/log/logger"

	"dubbo.apache.org/dubbo-go/v3/common"
	"dubbo.apache.org/dubbo-go/v3/common/constant"
	"dubbo.apache.org/dubbo-go/v3/metadata/info"
	gxsort "github.com/dubbogo/gost/sort"
)

// ServiceInstance is the interface  which is used for service registration and discovery.
type ServiceInstance interface {

	// GetID will return this instance's id. It should be unique.
	GetID() string

	// GetServiceName will return the serviceName
	GetServiceName() string

	// GetHost will return the hostname
	GetHost() string

	// GetPort will return the port.
	GetPort() int

	// IsEnable will return the enable status of this instance
	IsEnable() bool

	// IsHealthy will return the value represent the instance whether healthy or not
	IsHealthy() bool

	// GetMetadata will return the metadata
	GetMetadata() map[string]string

	// ToURLs will return a list of url
	ToURLs(service *info.ServiceInfo) []*common.URL

	// GetEndPoints will get end points from metadata
	GetEndPoints() []*Endpoint

	// Copy will return a instance with different port
	Copy(endpoint *Endpoint) ServiceInstance

	// GetAddress will return the ip:Port
	GetAddress() string

	// SetServiceMetadata saves metadata in instance
	SetServiceMetadata(info *info.MetadataInfo)
	GetServiceMetadata() *info.MetadataInfo

	// GetTag will return the tag of the instance
	GetTag() string

	// GetWeight will return the weight of the instance; if ≤0, the caller should use DefaultWeight as fallback
	GetWeight() int64
}

// nolint
type Endpoint struct {
	Port     int    `json:"port,omitempty"`
	Protocol string `json:"protocol,omitempty"`
}

// DefaultServiceInstance the default implementation of ServiceInstance
// or change the ServiceInstance to be struct???
type DefaultServiceInstance struct {
	ID              string
	ServiceName     string
	Host            string
	Port            int
	Weight          int64
	Enable          bool
	Healthy         bool
	Metadata        map[string]string
	ServiceMetadata *info.MetadataInfo
	Address         string
	GroupName       string
	endpoints       []*Endpoint `json:"-"`
	Tag             string
}

// GetID will return this instance's id. It should be unique.
func (d *DefaultServiceInstance) GetID() string {
	return d.ID
}

// GetServiceName will return the serviceName
func (d *DefaultServiceInstance) GetServiceName() string {
	return d.ServiceName
}

// GetHost will return the hostname
func (d *DefaultServiceInstance) GetHost() string {
	return d.Host
}

// GetPort will return the port.
func (d *DefaultServiceInstance) GetPort() int {
	return d.Port
}

// IsEnable will return the enable status of this instance
func (d *DefaultServiceInstance) IsEnable() bool {
	return d.Enable
}

// IsHealthy will return the value represent the instance whether healthy or not
func (d *DefaultServiceInstance) IsHealthy() bool {
	return d.Healthy
}

// GetAddress will return the ip:Port
func (d *DefaultServiceInstance) GetAddress() string {
	if d.Address != "" {
		return d.Address
	}
	if d.Port <= 0 {
		d.Address = d.Host
	} else {
		d.Address = d.Host + ":" + strconv.Itoa(d.Port)
	}
	return d.Address
}

// SetServiceMetadata save metadata in instance
func (d *DefaultServiceInstance) SetServiceMetadata(m *info.MetadataInfo) {
	d.ServiceMetadata = m
}

// GetServiceMetadata save metadata in instance
func (d *DefaultServiceInstance) GetServiceMetadata() *info.MetadataInfo {
	return d.ServiceMetadata
}

func (d *DefaultServiceInstance) GetTag() string {
	return d.Tag
}

// ToURLs return a list of url.
func (d *DefaultServiceInstance) ToURLs(service *info.ServiceInfo) []*common.URL {
	urls := make([]*common.URL, 0, 8)
	if d.endpoints == nil {
		err := json.Unmarshal([]byte(d.Metadata[constant.ServiceInstanceEndpoints]), &d.endpoints)
		if err != nil {
			logger.Errorf("Error parsing endpoints of service instance v%, multiple protocol services might not be able to work properly, err is v%.", d, err)
		}
	}

	if len(d.endpoints) > 0 {
		for _, endpoint := range d.endpoints {
			if endpoint.Protocol == service.Protocol {
				url := common.NewURLWithOptions(common.WithProtocol(service.Protocol),
					common.WithIp(d.Host), common.WithPort(strconv.Itoa(endpoint.Port)),
					common.WithPath(service.Name), common.WithInterface(service.Name),
					common.WithMethods(service.GetMethods()), common.WithParams(service.GetParams()),
					common.WithParams(url2.Values{constant.Tagkey: {d.Tag}}),
					common.WithWeight(d.GetWeight()))
				urls = append(urls, url)
			}
		}
	} else {
		url := common.NewURLWithOptions(common.WithProtocol(service.Protocol),
			common.WithIp(d.Host), common.WithPort(strconv.Itoa(d.Port)),
			common.WithPath(service.Name), common.WithInterface(service.Name),
			common.WithMethods(service.GetMethods()), common.WithParams(service.GetParams()),
			common.WithParams(url2.Values{constant.Tagkey: {d.Tag}}),
			common.WithWeight(d.GetWeight()))
		urls = append(urls, url)
	}
	return urls
}

// GetEndPoints get end points from metadata
func (d *DefaultServiceInstance) GetEndPoints() []*Endpoint {
	rawEndpoints := d.Metadata[constant.ServiceInstanceEndpoints]
	if len(rawEndpoints) == 0 {
		return nil
	}
	var endpoints []*Endpoint
	err := json.Unmarshal([]byte(rawEndpoints), &endpoints)
	if err != nil {
		logger.Errorf("json umarshal rawEndpoints[%s] catch error:%s", rawEndpoints, err.Error())
		return nil
	}
	return endpoints
}

// Copy return a instance with different port
func (d *DefaultServiceInstance) Copy(endpoint *Endpoint) ServiceInstance {
	dn := &DefaultServiceInstance{
		ID:              d.ID,
		ServiceName:     d.ServiceName,
		Host:            d.Host,
		Port:            endpoint.Port,
		Enable:          d.Enable,
		Healthy:         d.Healthy,
		Metadata:        d.Metadata,
		ServiceMetadata: d.ServiceMetadata,
		Tag:             d.Tag,
	}
	dn.ID = d.GetAddress()
	return dn
}

// GetMetadata will return the metadata, it will never return nil
func (d *DefaultServiceInstance) GetMetadata() map[string]string {
	if d.Metadata == nil {
		d.Metadata = make(map[string]string)
	}
	return d.Metadata
}

// ServiceInstanceCustomizer is an extension point which allow user using custom
// logic to modify instance. Be careful of priority. Usually you should use number
// between [100, 9000] other number will be thought as system reserve number
type ServiceInstanceCustomizer interface {
	gxsort.Prioritizer

	Customize(instance ServiceInstance)
}

func (d *DefaultServiceInstance) GetWeight() int64 {
	if d.Weight <= 0 {
		return constant.DefaultWeight
	}
	return d.Weight
}
