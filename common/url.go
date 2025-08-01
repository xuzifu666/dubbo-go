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

package common

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	cm "github.com/Workiva/go-datastructures/common"

	gxset "github.com/dubbogo/gost/container/set"
	"github.com/google/uuid"
	"github.com/jinzhu/copier"

	"dubbo.apache.org/dubbo-go/v3/common/constant"
	perrors "github.com/pkg/errors"
)

// dubbo role type constant
const (
	CONSUMER = iota
	CONFIGURATOR
	ROUTER
	PROVIDER
	PROTOCOL = "protocol"
)

var (
	DubboNodes          = [...]string{"consumers", "configurators", "routers", "providers"} // Dubbo service node
	DubboRole           = [...]string{"consumer", "", "routers", "provider"}                // Dubbo service role
	compareURLEqualFunc CompareURLEqualFunc                                                 // function to compare two URL is equal
)

func init() {
	compareURLEqualFunc = defaultCompareURLEqual
}

// nolint
type RoleType int

func (t RoleType) String() string {
	return DubboNodes[t]
}

// Role returns role by @RoleType
func (t RoleType) Role() string {
	return DubboRole[t]
}

// noCopy may be embedded into structs which must not be copied
// after the first use.
//
// See https://golang.org/issues/8005#issuecomment-190753527
// for details.
type noCopy struct{}

// Lock is a no-op used by -copylocks checker from `go vet`.
func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

// URL thread-safe. but this URL should not be copied.
// we fail to define this struct to be immutable object.
// but, those method which will update the URL, including SetParam, SetParams
// are only allowed to be invoked in creating URL instance
// Please keep in mind that this struct is immutable after it has been created and initialized.
type URL struct {
	noCopy noCopy

	Protocol string
	Location string // ip+port
	Ip       string
	Port     string

	PrimitiveURL string
	// url.Values is not safe map, add to avoid concurrent map read and map write error
	paramsLock sync.RWMutex
	params     url.Values

	Path     string // like  /com.ikurento.dubbo.UserProvider
	Username string
	Password string
	Methods  []string

	attributesLock sync.RWMutex
	// attributes should not be transported
	attributes map[string]any `hessian:"-"`
	// special for registry
	SubURL *URL
}

// JavaClassName POJO for URL
func (c *URL) JavaClassName() string {
	return "org.apache.dubbo.common.URL"
}

// Option accepts URL
// Option will define a function of handling URL
type Option func(*URL)

// WithUsername sets username for URL
func WithUsername(username string) Option {
	return func(url *URL) {
		url.Username = username
	}
}

// WithPassword sets password for URL
func WithPassword(pwd string) Option {
	return func(url *URL) {
		url.Password = pwd
	}
}

// WithMethods sets methods for URL
func WithMethods(methods []string) Option {
	return func(url *URL) {
		url.Methods = methods
	}
}

// WithParams deep copy the params in the argument into params of the target URL
func WithParams(params url.Values) Option {
	return func(url *URL) {
		url.SetParams(params)
	}
}

// WithParamsValue sets params field for URL
func WithParamsValue(key, val string) Option {
	return func(url *URL) {
		url.SetParam(key, val)
	}
}

// WithProtocol sets protocol for URL
func WithProtocol(proto string) Option {
	return func(url *URL) {
		url.Protocol = proto
	}
}

// WithIp sets ip for URL
func WithIp(ip string) Option {
	return func(url *URL) {
		url.Ip = ip
	}
}

// WithPort sets port for URL
func WithPort(port string) Option {
	return func(url *URL) {
		url.Port = port
	}
}

// WithPath sets path for URL
func WithPath(path string) Option {
	return func(url *URL) {
		url.Path = "/" + strings.TrimPrefix(path, "/")
	}
}

// WithInterface sets interface param for URL
func WithInterface(v string) Option {
	return func(url *URL) {
		url.SetParam(constant.InterfaceKey, v)
	}
}

// WithLocation sets location for URL
func WithLocation(location string) Option {
	return func(url *URL) {
		url.Location = location
	}
}

// WithToken sets token for URL
func WithToken(token string) Option {
	return func(url *URL) {
		if len(token) > 0 {
			value := token
			if strings.ToLower(token) == "true" || strings.ToLower(token) == "default" {
				u, _ := uuid.NewUUID()
				value = u.String()
			}
			url.SetParam(constant.TokenKey, value)
		}
	}
}

// WithAttribute sets attribute for URL
func WithAttribute(key string, attribute any) Option {
	return func(url *URL) {
		if url.attributes == nil {
			url.attributes = make(map[string]any)
		}
		url.attributes[key] = attribute
	}
}

// WithWeight sets weight for URL
func WithWeight(weight int64) Option {
	return func(url *URL) {
		if weight > 0 {
			url.SetParam(constant.WeightKey, strconv.FormatInt(weight, 10))
		}
	}
}

// NewURLWithOptions will create a new URL with options
func NewURLWithOptions(opts ...Option) *URL {
	newURL := &URL{}
	for _, opt := range opts {
		opt(newURL)
	}
	newURL.Location = newURL.Ip + ":" + newURL.Port
	return newURL
}

// NewURL will create a new URL
// the urlString should not be empty
func NewURL(urlString string, opts ...Option) (*URL, error) {
	s := URL{}
	if urlString == "" {
		return &s, nil
	}

	rawURLString, err := url.QueryUnescape(urlString)
	if err != nil {
		return &s, perrors.Errorf("URL.QueryUnescape(%s),  error{%v}", urlString, err)
	}

	// rawURLString = "//" + rawURLString
	if !strings.Contains(rawURLString, "//") {
		t := URL{}
		for _, opt := range opts {
			opt(&t)
		}
		rawURLString = t.Protocol + "://" + rawURLString
	}

	serviceURL, urlParseErr := url.Parse(rawURLString)
	if urlParseErr != nil {
		return &s, perrors.Errorf("URL.Parse(URL string{%s}),  error{%v}", rawURLString, err)
	}

	s.params, err = url.ParseQuery(serviceURL.RawQuery)
	if err != nil {
		return &s, perrors.Errorf("URL.ParseQuery(raw URL string{%s}),  error{%v}", serviceURL.RawQuery, err)
	}

	s.PrimitiveURL = urlString
	s.Protocol = serviceURL.Scheme
	s.Username = serviceURL.User.Username()
	s.Password, _ = serviceURL.User.Password()
	s.Location = serviceURL.Host
	s.Path = serviceURL.Path
	for _, location := range strings.Split(s.Location, ",") {
		location = strings.Trim(location, " ")
		if strings.Contains(location, ":") {
			s.Ip, s.Port, err = net.SplitHostPort(location)
			if err != nil {
				return &s, perrors.Errorf("net.SplitHostPort(url.Host{%s}), error{%v}", s.Location, err)
			}
			break
		}
	}
	for _, opt := range opts {
		opt(&s)
	}
	if s.params.Get(constant.RegistryGroupKey) != "" {
		s.PrimitiveURL = strings.Join([]string{s.PrimitiveURL, s.params.Get(constant.RegistryGroupKey)}, constant.PathSeparator)
	}
	return &s, nil
}

func MatchKey(serviceKey string, protocol string) string {
	return serviceKey + ":" + protocol
}

// Group get group
func (c *URL) Group() string {
	return c.GetParam(constant.GroupKey, "")
}

// Interface get interface
func (c *URL) Interface() string {
	return c.GetParam(constant.InterfaceKey, "")
}

// Version get group
func (c *URL) Version() string {
	return c.GetParam(constant.VersionKey, "")
}

// Address with format "ip:port"
func (c *URL) Address() string {
	if c.Port == "" {
		return c.Ip
	}
	return c.Ip + ":" + c.Port
}

// URLEqual judge @URL and @c is equal or not.
func (c *URL) URLEqual(url *URL) bool {
	tmpC := c.Clone()
	tmpC.Ip = ""
	tmpC.Port = ""

	tmpURL := url.Clone()
	tmpURL.Ip = ""
	tmpURL.Port = ""

	cGroup := tmpC.GetParam(constant.GroupKey, "")
	urlGroup := tmpURL.GetParam(constant.GroupKey, "")
	cKey := tmpC.Key()
	urlKey := tmpURL.Key()

	if cGroup == constant.AnyValue {
		cKey = strings.Replace(cKey, "group=*", "group="+urlGroup, 1)
	} else if urlGroup == constant.AnyValue {
		urlKey = strings.Replace(urlKey, "group=*", "group="+cGroup, 1)
	}

	// 1. protocol, username, password, ip, port, service name, group, version should be equal
	if cKey != urlKey {
		return false
	}

	// 2. if URL contains enabled key, should be true, or *
	if tmpURL.GetParam(constant.EnabledKey, "true") != "true" && tmpURL.GetParam(constant.EnabledKey, "") != constant.AnyValue {
		return false
	}

	// TODO :may need add interface key any value condition
	return isMatchCategory(tmpURL.GetParam(constant.CategoryKey, constant.DefaultCategory), tmpC.GetParam(constant.CategoryKey, constant.DefaultCategory))
}

func isMatchCategory(category1 string, category2 string) bool {
	if len(category2) == 0 {
		return category1 == constant.DefaultCategory
	} else if strings.Contains(category2, constant.AnyValue) {
		return true
	} else if strings.Contains(category2, constant.RemoveValuePrefix) {
		return !strings.Contains(category2, constant.RemoveValuePrefix+category1)
	} else {
		return strings.Contains(category2, category1)
	}
}

func (c *URL) String() string {
	c.paramsLock.Lock()
	defer c.paramsLock.Unlock()
	var buf strings.Builder
	if len(c.Username) == 0 && len(c.Password) == 0 {
		buf.WriteString(fmt.Sprintf("%s://%s:%s%s?", c.Protocol, c.Ip, c.Port, c.Path))
	} else {
		buf.WriteString(fmt.Sprintf("%s://%s:%s@%s:%s%s?", c.Protocol, c.Username, c.Password, c.Ip, c.Port, c.Path))
	}
	buf.WriteString(c.params.Encode())
	return buf.String()
}

// Key gets key
func (c *URL) Key() string {
	buildString := fmt.Sprintf("%s://%s:%s@%s:%s/?interface=%s&group=%s&version=%s",
		c.Protocol, c.Username, c.Password, c.Ip, c.Port, c.Service(), c.GetParam(constant.GroupKey, ""), c.GetParam(constant.VersionKey, ""))
	return buildString
}

// GetCacheInvokerMapKey get directory cacheInvokerMap key
func (c *URL) GetCacheInvokerMapKey() string {
	urlNew, _ := NewURL(c.PrimitiveURL)

	buildString := fmt.Sprintf("%s://%s:%s@%s:%s/?interface=%s&group=%s&version=%s&timestamp=%s&"+constant.MeshClusterIDKey+"=%s",
		c.Protocol, c.Username, c.Password, c.Ip, c.Port, c.Service(), c.GetParam(constant.GroupKey, ""),
		c.GetParam(constant.VersionKey, ""), urlNew.GetParam(constant.TimestampKey, ""),
		c.GetParam(constant.MeshClusterIDKey, ""))
	return buildString
}

// ServiceKey gets a unique key of a service.
func (c *URL) ServiceKey() string {
	return ServiceKey(c.GetParam(constant.InterfaceKey, strings.TrimPrefix(c.Path, constant.PathSeparator)),
		c.GetParam(constant.GroupKey, ""), c.GetParam(constant.VersionKey, ""))
}

func ServiceKey(intf string, group string, version string) string {
	if intf == "" {
		return ""
	}
	buf := &bytes.Buffer{}
	if group != "" {
		buf.WriteString(group)
		buf.WriteString("/")
	}

	buf.WriteString(intf)

	if version != "" && version != "0.0.0" {
		buf.WriteString(":")
		buf.WriteString(version)
	}

	return buf.String()
}

// ParseServiceKey gets interface, group and version from service key
func ParseServiceKey(serviceKey string) (string, string, string) {
	var (
		group   string
		version string
	)
	if serviceKey == "" {
		return "", "", ""
	}
	// get group if it exists
	sepIndex := strings.Index(serviceKey, constant.PathSeparator)
	if sepIndex != -1 {
		group = serviceKey[:sepIndex]
		serviceKey = serviceKey[sepIndex+1:]
	}
	// get version if it exists
	sepIndex = strings.LastIndex(serviceKey, constant.KeySeparator)
	if sepIndex != -1 {
		version = serviceKey[sepIndex+1:]
		serviceKey = serviceKey[:sepIndex]
	}

	return serviceKey, group, version
}

// IsAnyCondition judges if is any condition
func IsAnyCondition(intf, group, version string, serviceURL *URL) bool {
	return intf == constant.AnyValue && (group == constant.AnyValue ||
		group == serviceURL.Group()) && (version == constant.AnyValue || version == serviceURL.Version())
}

// ColonSeparatedKey
// The format is "{interface}:[version]:[group]"
func (c *URL) ColonSeparatedKey() string {
	intf := c.GetParam(constant.InterfaceKey, strings.TrimPrefix(c.Path, "/"))
	if intf == "" {
		return ""
	}
	var buf strings.Builder
	buf.WriteString(intf)
	buf.WriteString(":")
	version := c.GetParam(constant.VersionKey, "")
	if version != "" && version != "0.0.0" {
		buf.WriteString(version)
	}
	group := c.GetParam(constant.GroupKey, "")
	buf.WriteString(":")
	if group != "" {
		buf.WriteString(group)
	}
	return buf.String()
}

// EncodedServiceKey encode the service key
func (c *URL) EncodedServiceKey() string {
	serviceKey := c.ServiceKey()
	return strings.Replace(serviceKey, "/", "*", 1)
}

// Service gets service
func (c *URL) Service() string {
	service := c.GetParam(constant.InterfaceKey, strings.TrimPrefix(c.Path, "/"))
	if service != "" {
		return service
	} else if c.SubURL != nil {
		service = c.SubURL.GetParam(constant.InterfaceKey, strings.TrimPrefix(c.Path, "/"))
		if service != "" { // if URL.path is "" then return suburl's path, special for registry URL
			return service
		}
	}
	return ""
}

// AddParam will add the key-value pair
func (c *URL) AddParam(key string, value string) {
	c.paramsLock.Lock()
	defer c.paramsLock.Unlock()
	if c.params == nil {
		c.params = url.Values{}
	}
	c.params.Add(key, value)
}

// AddParamAvoidNil will add key-value pair
func (c *URL) AddParamAvoidNil(key string, value string) {
	c.paramsLock.Lock()
	defer c.paramsLock.Unlock()
	if c.params == nil {
		c.params = url.Values{}
	}
	c.params.Add(key, value)
}

// SetParam will put the key-value pair into URL
// usually it should only be invoked when you want to initialized an URL
func (c *URL) SetParam(key string, value string) {
	c.paramsLock.Lock()
	defer c.paramsLock.Unlock()
	if c.params == nil {
		c.params = url.Values{}
	}
	c.params.Set(key, value)
}

func (c *URL) SetAttribute(key string, value any) {
	c.attributesLock.Lock()
	defer c.attributesLock.Unlock()
	if c.attributes == nil {
		c.attributes = make(map[string]any)
	}
	c.attributes[key] = value
}

func (c *URL) GetAttribute(key string) (any, bool) {
	c.attributesLock.RLock()
	defer c.attributesLock.RUnlock()
	r, ok := c.attributes[key]
	return r, ok
}

// DelParam will delete the given key from the URL
func (c *URL) DelParam(key string) {
	c.paramsLock.Lock()
	defer c.paramsLock.Unlock()
	if c.params != nil {
		c.params.Del(key)
	}
}

// ReplaceParams will replace the URL.params
// usually it should only be invoked when you want to modify an URL, such as MergeURL
func (c *URL) ReplaceParams(param url.Values) {
	c.paramsLock.Lock()
	defer c.paramsLock.Unlock()
	c.params = param
}

// RangeParams will iterate the params
func (c *URL) RangeParams(f func(key, value string) bool) {
	c.paramsLock.RLock()
	defer c.paramsLock.RUnlock()
	for k, v := range c.params {
		if !f(k, v[0]) {
			break
		}
	}
}

// GetParam gets value by key
func (c *URL) GetParam(s string, d string) string {
	c.paramsLock.RLock()
	defer c.paramsLock.RUnlock()

	var r string
	if len(c.params) > 0 {
		r = c.params.Get(s)
	}
	if len(r) == 0 {
		r = d
	}

	return r
}

// GetNonDefaultParam gets value by key, return nil,false if no value found mapping to the key
func (c *URL) GetNonDefaultParam(s string) (string, bool) {
	c.paramsLock.RLock()
	defer c.paramsLock.RUnlock()

	var r string
	if len(c.params) > 0 {
		r = c.params.Get(s)
	}

	return r, r != ""
}

// GetParams gets values
func (c *URL) GetParams() url.Values {
	return c.params
}

// GetParamAndDecoded gets values and decode
func (c *URL) GetParamAndDecoded(key string) (string, error) {
	ruleDec, err := base64.URLEncoding.DecodeString(c.GetParam(key, ""))
	value := string(ruleDec)
	return value, err
}

// GetRawParam gets raw param
func (c *URL) GetRawParam(key string) string {
	switch key {
	case PROTOCOL:
		return c.Protocol
	case "username":
		return c.Username
	case "host":
		return strings.Split(c.Location, ":")[0]
	case "password":
		return c.Password
	case "port":
		return c.Port
	case "path":
		return c.Path
	default:
		return c.GetParam(key, "")
	}
}

// GetParamBool judge whether @key exists or not
func (c *URL) GetParamBool(key string, d bool) bool {
	r, err := strconv.ParseBool(c.GetParam(key, ""))
	if err != nil {
		return d
	}
	return r
}

// GetParamInt gets int64 value by @key
func (c *URL) GetParamInt(key string, d int64) int64 {
	r, err := strconv.ParseInt(c.GetParam(key, ""), 10, 64)
	if err != nil {
		return d
	}
	return r
}

// GetParamInt32 gets int32 value by @key
func (c *URL) GetParamInt32(key string, d int32) int32 {
	r, err := strconv.ParseInt(c.GetParam(key, ""), 10, 32)
	if err != nil {
		return d
	}
	return int32(r)
}

// GetParamByIntValue gets int value by @key
func (c *URL) GetParamByIntValue(key string, d int) int {
	r, err := strconv.ParseInt(c.GetParam(key, ""), 10, 0)
	if err != nil {
		return d
	}
	return int(r)
}

// GetMethodParamInt gets int method param
func (c *URL) GetMethodParamInt(method string, key string, d int64) int64 {
	r, err := strconv.ParseInt(c.GetParam("methods."+method+"."+key, ""), 10, 64)
	if err != nil {
		return d
	}
	return r
}

// GetMethodParamIntValue gets int method param
func (c *URL) GetMethodParamIntValue(method string, key string, d int) int {
	r, err := strconv.ParseInt(c.GetParam("methods."+method+"."+key, ""), 10, 0)
	if err != nil {
		return d
	}
	return int(r)
}

// GetMethodParamInt64 gets int64 method param
func (c *URL) GetMethodParamInt64(method string, key string, d int64) int64 {
	r := c.GetMethodParamInt(method, key, math.MinInt64)
	if r == math.MinInt64 {
		return c.GetParamInt(key, d)
	}
	return r
}

// GetMethodParam gets method param
func (c *URL) GetMethodParam(method string, key string, d string) string {
	r := c.GetParam("methods."+method+"."+key, "")
	if r == "" {
		r = d
	}
	return r
}

// GetMethodParamBool judge whether @method param exists or not
func (c *URL) GetMethodParamBool(method string, key string, d bool) bool {
	r := c.GetParamBool("methods."+method+"."+key, d)
	return r
}

// SetParams will put all key-value pair into URL.
// 1. if there already has same key, the value will be override
// 2. it's not thread safe
// 3. think twice when you want to invoke this method
func (c *URL) SetParams(m url.Values) {
	for k := range m {
		c.SetParam(k, m.Get(k))
	}
}

// ToMap transfer URL to Map
func (c *URL) ToMap() map[string]string {
	paramsMap := make(map[string]string)

	c.RangeParams(func(key, value string) bool {
		paramsMap[key] = value
		return true
	})

	if c.Protocol != "" {
		paramsMap[PROTOCOL] = c.Protocol
	}
	if c.Username != "" {
		paramsMap["username"] = c.Username
	}
	if c.Password != "" {
		paramsMap["password"] = c.Password
	}
	if c.Location != "" {
		paramsMap["host"] = strings.Split(c.Location, ":")[0]
		var port string
		if strings.Contains(c.Location, ":") {
			port = strings.Split(c.Location, ":")[1]
		} else {
			port = "0"
		}
		paramsMap["port"] = port
	}
	if c.Protocol != "" {
		paramsMap[PROTOCOL] = c.Protocol
	}
	if c.Path != "" {
		paramsMap["path"] = c.Path
	}
	if len(paramsMap) == 0 {
		return nil
	}
	return paramsMap
}

// configuration  > reference config >service config
//  in this function we should merge the reference local URL config into the service URL from registry.
// TODO configuration merge, in the future , the configuration center's config should merge too.

// MergeURL will merge those two URL
// the result is based on c, and the key which si only contained in anotherUrl
// will be added into result.
// for example, if c contains params (a1->v1, b1->v2) and anotherUrl contains params(a2->v3, b1 -> v4)
// the params of result will be (a1->v1, b1->v2, a2->v3).
// You should notice that the value of b1 is v2, not v4
// except constant.LoadbalanceKey, constant.ClusterKey, constant.RetriesKey, constant.TimeoutKey.
// due to URL is not thread-safe, so this method is not thread-safe
func (c *URL) MergeURL(anotherUrl *URL) *URL {
	// After Clone, it is a new URL that there is no thread safe issue.
	mergedURL := c.Clone()
	params := mergedURL.GetParams()
	// iterator the anotherUrl if c not have the key ,merge in
	// anotherUrl usually will not changed. so change RangeParams to GetParams to avoid the string value copy.// Group get group
	for key, value := range anotherUrl.GetParams() {
		if _, ok := mergedURL.GetNonDefaultParam(key); !ok {
			if len(value) > 0 {
				params[key] = make([]string, len(value))
				copy(params[key], value)
			}
		}
	}

	// remote timestamp
	if v, ok := c.GetNonDefaultParam(constant.TimestampKey); !ok {
		params[constant.RemoteTimestampKey] = []string{v}
		params[constant.TimestampKey] = []string{anotherUrl.GetParam(constant.TimestampKey, "")}
	}

	// finally execute methodConfigMergeFcn
	mergedURL.Methods = make([]string, len(anotherUrl.Methods))
	for i, method := range anotherUrl.Methods {
		for _, paramKey := range []string{constant.LoadbalanceKey, constant.ClusterKey, constant.RetriesKey, constant.TimeoutKey} {
			if v := anotherUrl.GetParam(paramKey, ""); len(v) > 0 {
				params[paramKey] = []string{v}
			}

			methodsKey := "methods." + method + "." + paramKey
			//if len(mergedURL.GetParam(methodsKey, "")) == 0 {
			if v := anotherUrl.GetParam(methodsKey, ""); len(v) > 0 {
				params[methodsKey] = []string{v}
			}
			//}
			mergedURL.Methods[i] = method
		}
	}
	// merge attributes
	if mergedURL.attributes == nil {
		mergedURL.attributes = make(map[string]any, len(anotherUrl.attributes))
	}
	for attrK, attrV := range anotherUrl.attributes {
		if _, ok := mergedURL.GetAttribute(attrK); !ok {
			mergedURL.attributes[attrK] = attrV
		}
	}
	// In this way, we will raise some performance.
	mergedURL.ReplaceParams(params)
	return mergedURL
}

// Clone will copy the URL
func (c *URL) Clone() *URL {
	newURL := &URL{}
	if err := copier.Copy(newURL, c); err != nil {
		// this is impossible
		return newURL
	}
	newURL.params = url.Values{}
	c.RangeParams(func(key, value string) bool {
		newURL.SetParam(key, value)
		return true
	})
	c.RangeAttributes(func(key string, value any) bool {
		newURL.SetAttribute(key, value)
		return true
	})
	return newURL
}

func (c *URL) RangeAttributes(f func(key string, value any) bool) {
	c.attributesLock.RLock()
	defer c.attributesLock.RUnlock()
	for k, v := range c.attributes {
		if !f(k, v) {
			break
		}
	}
}

func (c *URL) CloneExceptParams(excludeParams *gxset.HashSet) *URL {
	newURL := &URL{}
	if err := copier.Copy(newURL, c); err != nil {
		// this is impossible
		return newURL
	}
	newURL.params = url.Values{}
	c.RangeParams(func(key, value string) bool {
		if !excludeParams.Contains(key) {
			newURL.SetParam(key, value)
		}
		return true
	})
	return newURL
}

func (c *URL) Compare(comp cm.Comparator) int {
	a := c.String()
	b := comp.(*URL).String()
	switch {
	case a > b:
		return 1
	case a < b:
		return -1
	default:
		return 0
	}
}

// CloneWithParams Copy URL based on the reserved parameter's keys.
func (c *URL) CloneWithParams(reserveParams []string) *URL {
	params := url.Values{}
	for _, reserveParam := range reserveParams {
		v := c.GetParam(reserveParam, "")
		if len(v) != 0 {
			params.Set(reserveParam, v)
		}
	}

	return NewURLWithOptions(
		WithProtocol(c.Protocol),
		WithUsername(c.Username),
		WithPassword(c.Password),
		WithIp(c.Ip),
		WithPort(c.Port),
		WithPath(c.Path),
		WithMethods(c.Methods),
		WithParams(params),
	)
}

// IsEquals compares if two URLs equals with each other. Excludes are all parameter keys which should ignored.
func IsEquals(left *URL, right *URL, excludes ...string) bool {
	if (left == nil && right != nil) || (right == nil && left != nil) {
		return false
	}
	if left.Ip != right.Ip || left.Port != right.Port {
		return false
	}

	leftMap := left.ToMap()
	rightMap := right.ToMap()
	for _, exclude := range excludes {
		delete(leftMap, exclude)
		delete(rightMap, exclude)
	}

	if len(leftMap) != len(rightMap) {
		return false
	}

	for lk, lv := range leftMap {
		if rv, ok := rightMap[lk]; !ok {
			return false
		} else if lv != rv {
			return false
		}
	}

	return true
}

// URLSlice will be used to sort URL instance
// Instances will be order by URL.String()
type URLSlice []*URL

// nolint
func (s URLSlice) Len() int {
	return len(s)
}

// nolint
func (s URLSlice) Less(i, j int) bool {
	return s[i].String() < s[j].String()
}

// nolint
func (s URLSlice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

type CompareURLEqualFunc func(l *URL, r *URL, excludeParam ...string) bool

func defaultCompareURLEqual(l *URL, r *URL, excludeParam ...string) bool {
	return IsEquals(l, r, excludeParam...)
}

func SetCompareURLEqualFunc(f CompareURLEqualFunc) {
	compareURLEqualFunc = f
}

func GetCompareURLEqualFunc() CompareURLEqualFunc {
	return compareURLEqualFunc
}

// GetParamDuration get duration if param is invalid or missing default value will return 3s
func (c *URL) GetParamDuration(s string, d string) time.Duration {
	if t, err := time.ParseDuration(c.GetParam(s, d)); err == nil {
		return t
	}
	return 3 * time.Second
}

func GetSubscribeName(url *URL) string {
	var buffer bytes.Buffer

	buffer.Write([]byte(DubboNodes[PROVIDER]))
	appendParam(&buffer, url, constant.InterfaceKey)
	appendParam(&buffer, url, constant.VersionKey)
	appendParam(&buffer, url, constant.GroupKey)
	return buffer.String()
}

func appendParam(target *bytes.Buffer, url *URL, key string) {
	value := url.GetParam(key, "")
	target.Write([]byte(constant.NacosServiceNameSeparator))
	if strings.TrimSpace(value) != "" {
		target.Write([]byte(value))
	}
}
