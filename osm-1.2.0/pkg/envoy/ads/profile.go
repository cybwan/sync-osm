package ads

import (
	"fmt"
	"time"

	mapset "github.com/deckarep/golang-set"
	xds_discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"

	"github.com/openservicemesh/osm/pkg/envoy"
	"github.com/openservicemesh/osm/pkg/metricsstore"
)

const (
	// MaxXdsLogsPerProxy keeps a higher bound of how many timestamps do we keep per proxy
	MaxXdsLogsPerProxy = 20
)

func xdsPathTimeTrack(startedAt time.Time, typeURI envoy.TypeURI, proxy *envoy.Proxy, success bool) {
	elapsed := time.Since(startedAt)

	log.Debug().Str("proxy", proxy.String()).Msgf("Time taken proxy to generate response for request with typeURI=%s: %s", typeURI, elapsed)

	metricsstore.DefaultMetricsStore.ProxyConfigUpdateTime.
		WithLabelValues(typeURI.String(), fmt.Sprintf("%t", success)).
		Observe(elapsed.Seconds())
}

func (s *Server) trackXDSLog(proxyUUID string, typeURL envoy.TypeURI) {
	s.withXdsLogMutex(func() {
		if _, ok := s.xdsLog[proxyUUID]; !ok {
			s.xdsLog[proxyUUID] = make(map[envoy.TypeURI][]time.Time)
		}

		timeSlice, ok := s.xdsLog[proxyUUID][typeURL]
		if !ok {
			s.xdsLog[proxyUUID][typeURL] = []time.Time{time.Now()}
			return
		}

		timeSlice = append(timeSlice, time.Now())
		if len(timeSlice) > MaxXdsLogsPerProxy {
			timeSlice = timeSlice[1:]
		}
		s.xdsLog[proxyUUID][typeURL] = timeSlice
	})
}

// validateRequestResponse is a utility function to validate the response generated by a given request.
// currently, it checks that all resources responded for a request are being responded to,
// or will log as `warn` otherwise
// Returns the number of resources NOT being answered for a request. For wildcard case, this is always 0.
func validateRequestResponse(proxy *envoy.Proxy, request *xds_discovery.DiscoveryRequest, respResources []types.Resource) int {
	// No validation is done for wildcard cases
	resourcesRequested := mapset.NewSet()
	resourcesToSend := mapset.NewSet()

	// Get resources being requested
	for _, reqResource := range request.ResourceNames {
		resourcesRequested.Add(reqResource)
	}

	// Get resources being responded by name
	for _, res := range respResources {
		resourcesToSend.Add(cache.GetResourceName(res))
	}

	// Compute difference from request resources' perspective
	resDifference := resourcesRequested.Difference(resourcesToSend)
	diffCardinality := resDifference.Cardinality()
	if diffCardinality != 0 {
		log.Warn().Str("proxy", proxy.String()).Msgf("Not all request resources for type %s are being responded to req [%v] resp [%v] diff [%v]",
			envoy.TypeURI(request.TypeUrl).Short(),
			resourcesRequested, resourcesToSend, resDifference)
	}

	return diffCardinality
}
