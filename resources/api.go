package resources

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/yoheimuta/go-protoparser/v4"
	"github.com/yoheimuta/go-protoparser/v4/parser"

	basev0 "github.com/codefly-dev/core/generated/go/codefly/base/v0"
	"github.com/codefly-dev/core/shared"
	"github.com/codefly-dev/core/standards"
	"github.com/codefly-dev/core/wool"
	openapiloads "github.com/go-openapi/loads"
	openapispec "github.com/go-openapi/spec"
)

func NewAPI(_ context.Context, endpoint *Endpoint, api *basev0.API) (*basev0.Endpoint, error) {
	if endpoint == nil {
		return nil, fmt.Errorf("endpoint is nil")
	}
	declaration := *endpoint
	declaration.API = APIString(api)
	result, err := declaration.Proto()
	if err != nil {
		return nil, err
	}
	result.ApiDetails = api
	return result, nil
}

func APIString(api *basev0.API) string {
	if api == nil {
		return standards.Unknown
	}
	if api.Value == nil {
		return standards.Unknown
	}
	switch api.Value.(type) {
	case *basev0.API_Grpc:
		return standards.GRPC
	case *basev0.API_Rest:
		return standards.REST
	case *basev0.API_Http:
		return standards.HTTP
	case *basev0.API_Tcp:
		return standards.TCP
	}
	return standards.Unknown
}

func ToGrpcAPI(grpc *basev0.GrpcAPI) *basev0.API {
	return &basev0.API{
		Value: &basev0.API_Grpc{
			Grpc: grpc,
		},
	}
}

func LoadGrpcAPI(_ context.Context, f *string) (*basev0.GrpcAPI, error) {
	if f == nil {
		return &basev0.GrpcAPI{}, nil
	}
	filename := *f
	// Read the file content
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %v", filename, err)
	}

	got, err := protoparser.Parse(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse proto file %s: %v", filename, err)
	}

	var rpcs []*basev0.RPC
	var packageName string
	for _, element := range got.ProtoBody {
		switch element := element.(type) {
		case *parser.Package:
			packageName = element.Name
		case *parser.Service:
			for _, inside := range element.ServiceBody {
				if rpc, ok := inside.(*parser.RPC); ok {
					rpcs = append(rpcs, &basev0.RPC{Name: rpc.RPCName, ServiceName: element.ServiceName})
				}
			}
		}
	}
	return &basev0.GrpcAPI{Proto: content, Package: packageName, Rpcs: rpcs}, nil
}

func ToRestAPI(rest *basev0.RestAPI) *basev0.API {
	return &basev0.API{
		Value: &basev0.API_Rest{
			Rest: rest,
		},
	}
}

func LoadRestAPI(ctx context.Context, f *string) (*basev0.RestAPI, error) {
	w := wool.Get(ctx).In("endpoints.NewRestAPIFromOpenAPI")
	if f == nil {
		return &basev0.RestAPI{}, nil
	}
	filename := *f
	w.Debug("loading REST GetAPI from file", wool.FileField(filename))
	exists, err := shared.FileExists(ctx, filename)
	if err != nil {
		return nil, w.Wrapf(err, "failed to check file existence")
	}
	if !exists {
		return nil, w.NewError("file does not exist")
	}
	// Read the file content
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, w.Wrapf(err, "failed to read file")
	}
	openapi, err := ParseOpenAPI(content)
	if err != nil {
		return nil, w.Wrapf(err, "failed to parse openapi spec")
	}

	groupMap := make(map[string]*basev0.RestRouteGroup)
	for path := range openapi.Paths.Paths {
		var group *basev0.RestRouteGroup
		var ok bool
		if group, ok = groupMap[path]; !ok {
			// Set Path: GroupKey() keys on group.Path, so leaving it empty
			// collapsed every group of an endpoint into one mis-named group.
			group = &basev0.RestRouteGroup{Path: path}
			groupMap[path] = group
		}
		item := openapi.Paths.Paths[path]
		methods := getHTTPMethodsFromPathItem(&item)
		for _, method := range methods {
			route := &basev0.RestRoute{Path: path, Method: method}
			group.Routes = append(group.Routes, route)
		}
	}
	var groups []*basev0.RestRouteGroup
	for _, group := range groupMap {
		groups = append(groups, group)
	}
	return &basev0.RestAPI{Openapi: content, Groups: groups}, nil
}

func EndpointRestAPI(endpoint *basev0.Endpoint) *basev0.RestAPI {
	if endpoint == nil {
		return nil
	}
	if endpoint.ApiDetails == nil {
		return nil
	}
	switch v := endpoint.ApiDetails.Value.(type) {
	case *basev0.API_Rest:
		return v.Rest
	default:
		return nil
	}
}

type HTTPAPI struct{}

func (h *HTTPAPI) Proto() (*basev0.API, error) {
	return &basev0.API{
		Value: &basev0.API_Http{
			Http: &basev0.HttpAPI{}},
	}, nil
}

func LoadHTTPAPI(_ context.Context) (*basev0.HttpAPI, error) {
	return &basev0.HttpAPI{}, nil
}

func ToHTTPAPI(http *basev0.HttpAPI) *basev0.API {
	return &basev0.API{
		Value: &basev0.API_Http{
			Http: http,
		},
	}
}

type TCP struct{}

func NewTCP() (*TCP, error) {
	return &TCP{}, nil
}

func (*TCP) Proto() (*basev0.API, error) {
	// Add a GrpcAPI message with the file content
	tcp := &basev0.TcpAPI{}
	// Add an GetAPI message with the GrpcAPI message
	api := &basev0.API{
		Value: &basev0.API_Tcp{
			Tcp: tcp,
		},
	}
	return api, nil
}

func LoadTCPAPI(_ context.Context) (*basev0.TcpAPI, error) {
	return &basev0.TcpAPI{}, nil
}

func ToTCPAPI(tcp *basev0.TcpAPI) *basev0.API {
	return &basev0.API{
		Value: &basev0.API_Tcp{
			Tcp: tcp,
		},
	}
}

/* Helpers */

func ParseOpenAPI(spec []byte) (*openapispec.Swagger, error) {
	analyzed, err := openapiloads.Analyzed(spec, "2.0")
	if err != nil {
		return nil, err
	}
	return analyzed.Spec(), nil
}

func getHTTPMethodsFromPathItem(pathItem *openapispec.PathItem) []basev0.HTTPMethod {
	var methods []basev0.HTTPMethod

	if pathItem.Get != nil {
		methods = append(methods, basev0.HTTPMethod_GET)
	}
	if pathItem.Post != nil {
		methods = append(methods, basev0.HTTPMethod_POST)
	}
	if pathItem.Put != nil {
		methods = append(methods, basev0.HTTPMethod_PUT)
	}
	if pathItem.Delete != nil {
		methods = append(methods, basev0.HTTPMethod_DELETE)
	}
	if pathItem.Options != nil {
		methods = append(methods, basev0.HTTPMethod_OPTIONS)
	}
	if pathItem.Head != nil {
		methods = append(methods, basev0.HTTPMethod_HEAD)
	}
	if pathItem.Patch != nil {
		methods = append(methods, basev0.HTTPMethod_PATCH)
	}
	return methods
}

func LightAPI(api *basev0.API) *basev0.API {
	switch v := api.Value.(type) {
	case *basev0.API_Grpc:
		return &basev0.API{
			Value: &basev0.API_Grpc{},
		}
	case *basev0.API_Rest:
		return &basev0.API{
			Value: &basev0.API_Rest{
				Rest: &basev0.RestAPI{Groups: v.Rest.Groups},
			},
		}
	case *basev0.API_Http:
		return &basev0.API{
			Value: &basev0.API_Http{},
		}
	case *basev0.API_Tcp:
		return &basev0.API{
			Value: &basev0.API_Tcp{},
		}
	default:
		return nil
	}
}

func RestRoutes(rest *basev0.RestAPI) string {
	if rest == nil {
		return ""
	}
	// display only all the rest groups
	return fmt.Sprintf("%v", rest.Groups)
}
