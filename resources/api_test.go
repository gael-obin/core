package resources_test

import (
	"context"
	"testing"

	"github.com/codefly-dev/core/resources"
	"github.com/codefly-dev/core/shared"

	basev0 "github.com/codefly-dev/core/generated/go/codefly/base/v0"

	"github.com/stretchr/testify/require"
)

func TestREST(t *testing.T) {
	ctx := context.Background()
	rest, err := resources.LoadRestAPI(ctx, shared.Pointer("testdata/endpoints/basic/openapi/api.json"))
	require.NoError(t, err)
	require.Equal(t, 2, len(rest.Groups)) // 2 Paths
	var routes []*basev0.RestRoute
	for _, group := range rest.Groups {
		routes = append(routes, group.Routes...)
	}
	require.Equal(t, 3, len(routes)) // 3 Routes (1 path with 2 Methods)
}

func TestGRPC(t *testing.T) {
	ctx := context.Background()
	grpc, err := resources.LoadGrpcAPI(ctx, shared.Pointer("testdata/endpoints/basic/proto/api.proto"))
	require.NoError(t, err)
	require.Equal(t, "management.organization", grpc.Package)
	require.Equal(t, 4, len(grpc.Rpcs)) // 4 RPCs
	for _, rpc := range grpc.Rpcs {
		require.Equal(t, "OrganizationService", rpc.ServiceName)
	}
}

func TestNewAPIPreservesEndpointAccess(t *testing.T) {
	endpoint := &resources.Endpoint{
		Module: "secrets", Service: "vault", Name: "http",
		Visibility:   resources.VisibilityInternal,
		AllowModules: []string{"host"}, Location: resources.LocationExternal,
	}
	api := resources.ToHTTPAPI(&basev0.HttpAPI{Secured: true})
	got, err := resources.NewAPI(context.Background(), endpoint, api)
	require.NoError(t, err)
	require.NoError(t, resources.Validate(got))
	require.Equal(t, resources.LocationExternal, got.GetLocation())
	require.Same(t, api, got.GetApiDetails())
	require.NoError(t, resources.ValidateEndpointVisibility("host", got.Module, got.Service, got.Name, got.Visibility, got.Location, got.AllowModules))
	require.Error(t, resources.ValidateEndpointVisibility("unrelated", got.Module, got.Service, got.Name, got.Visibility, got.Location, got.AllowModules))
	require.Empty(t, endpoint.API, "attaching API details must not mutate the declaration")

	endpoint.Visibility = resources.VisibilityPrivate
	_, err = resources.NewAPI(context.Background(), endpoint, api)
	require.Error(t, err, "a private endpoint cannot carry an unread allow-list")
}
