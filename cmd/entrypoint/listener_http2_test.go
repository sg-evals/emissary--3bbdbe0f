package entrypoint_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emissary-ingress/emissary/v3/cmd/entrypoint"
	v3bootstrap "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	v3listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
)

func HasListenerPredicate(listenerName string) func(*v3bootstrap.Bootstrap) bool {
	return func(config *v3bootstrap.Bootstrap) bool {
		// Make sure we have the specified listener.
		for _, listener := range config.StaticResources.Listeners {
			if listener.Name == listenerName {
				return true
			}
		}

		return false
	}
}

func GetEnvoyConfigWithListener(t *testing.T, f *entrypoint.Fake, listenerName string) *v3listener.Listener {
	envoyConfig, err := f.GetEnvoyConfig(HasListenerPredicate(listenerName))
	assert.NoError(t, err)
	assert.NotNil(t, envoyConfig)

	// Find the listener
	listener := findListenerByName(envoyConfig, listenerName)
	assert.NotNil(t, listener, fmt.Sprintf("%s should exist", listenerName))

	// Marshal listener to JSON for debugging
	if listenerJSON, err := json.Marshal(listener); err == nil {
		t.Logf("Listener JSON: %s\n", string(listenerJSON))
	} else {
		t.Logf("Failed to marshal listener to JSON: %v\n", err)
	}

	return listener
}

// TestListenerHTTP2MaxConcurrentStreamsMissing verifies that when http2MaxConcurrentStreams
// is not set on a Listener, the http2_protocol_options should not appear in the generated
// Envoy configuration.
func TestListenerHTTP2MaxConcurrentStreamsMissing(t *testing.T) {
	f := entrypoint.RunFake(t, entrypoint.FakeConfig{EnvoyConfig: true}, nil)

	// Create a basic Listener without http2MaxConcurrentStreams
	assert.NoError(t, f.UpsertYAML(`
---
apiVersion: getambassador.io/v3alpha1
kind: Listener
metadata:
  name: listener-8080
  namespace: default
spec:
  port: 8080
  protocol: HTTP
  securityModel: XFP
  hostBinding:
    namespace:
      from: ALL
---
apiVersion: getambassador.io/v3alpha1
kind: Mapping
metadata:
  name: backend
  namespace: default
spec:
  hostname: "*"
  prefix: /backend/
  service: backend
`))

	f.Flush()

	// Get the envoy config
	listener := GetEnvoyConfigWithListener(t, f, "listener-8080")

	// Extract the HCM from the listener
	hcm := getHCMFromListener(t, listener)
	assert.NotNil(t, hcm)

	// Verify that http2_protocol_options is NOT set
	assert.Nil(t, hcm.Http2ProtocolOptions, "http2_protocol_options should not be set when http2MaxConcurrentStreams is not specified")

	t.Logf("TestListenerHTTP2MaxConcurrentStreamsMissing completed successfully\n")
}

// TestListenerHTTP2MaxConcurrentStreamsSet verifies that when http2MaxConcurrentStreams
// is set on a Listener, it appears correctly in the generated Envoy configuration.
func TestListenerHTTP2MaxConcurrentStreamsSet(t *testing.T) {
	f := entrypoint.RunFake(t, entrypoint.FakeConfig{EnvoyConfig: true}, nil)

	// Create a Listener with http2MaxConcurrentStreams set to 100
	require.NoError(t, f.UpsertYAML(`
---
apiVersion: getambassador.io/v3alpha1
kind: Listener
metadata:
  name: listener-8080
  namespace: default
spec:
  port: 8080
  protocol: HTTP
  securityModel: XFP
  http2MaxConcurrentStreams: 100
  hostBinding:
    namespace:
      from: ALL
---
apiVersion: getambassador.io/v3alpha1
kind: Mapping
metadata:
  name: backend
  namespace: default
spec:
  hostname: "*"
  prefix: /backend/
  service: backend
`))

	f.Flush()

	// Get the envoy config
	listener := GetEnvoyConfigWithListener(t, f, "listener-8080")

	// Extract the HCM from the listener
	hcm := getHCMFromListener(t, listener)
	assert.NotNil(t, hcm)

	// Verify that http2_protocol_options is set correctly
	assert.NotNil(t, hcm.Http2ProtocolOptions, "http2_protocol_options should be set")
	assert.Equal(t, uint32(100), hcm.Http2ProtocolOptions.MaxConcurrentStreams.GetValue(), "max_concurrent_streams should be 100")

	t.Logf("TestListenerHTTP2MaxConcurrentStreamsSet completed successfully\n")
}

// TestListenerHTTP2MaxConcurrentStreams1024 verifies that http2MaxConcurrentStreams
// can be set to 1024 (the new Envoy 1.36 default).
func TestListenerHTTP2MaxConcurrentStreams1024(t *testing.T) {
	f := entrypoint.RunFake(t, entrypoint.FakeConfig{EnvoyConfig: true}, nil)

	// Create an HTTPS Listener with http2MaxConcurrentStreams set to 1024
	require.NoError(t, f.UpsertYAML(`
---
apiVersion: getambassador.io/v3alpha1
kind: Listener
metadata:
  name: listener-8443
  namespace: default
spec:
  port: 8443
  protocol: HTTPS
  securityModel: SECURE
  http2MaxConcurrentStreams: 1024
  hostBinding:
    namespace:
      from: ALL
---
apiVersion: getambassador.io/v3alpha1
kind: Mapping
metadata:
  name: backend
  namespace: default
spec:
  hostname: "*"
  prefix: /backend/
  service: backend
`))

	f.Flush()

	// Find the listener
	listener := GetEnvoyConfigWithListener(t, f, "listener-8443")

	// Extract the HCM from the listener
	hcm := getHCMFromListener(t, listener)
	require.NotNil(t, hcm)

	// Verify that http2_protocol_options is set correctly
	require.NotNil(t, hcm.Http2ProtocolOptions, "http2_protocol_options should be set")
	assert.Equal(t, uint32(1024), hcm.Http2ProtocolOptions.MaxConcurrentStreams.GetValue(), "max_concurrent_streams should be 1024")

	fmt.Printf("TestListenerHTTP2MaxConcurrentStreams1024 completed successfully\n")
}

// getHCMFromListener extracts the HttpConnectionManager from a listener's filter chains
func getHCMFromListener(t *testing.T, listener *v3listener.Listener) *v3hcm.HttpConnectionManager {
	t.Helper()

	if len(listener.FilterChains) == 0 {
		t.Fatal("listener has no filter chains")
	}

	filterChain := listener.FilterChains[0]
	if len(filterChain.Filters) == 0 {
		t.Fatal("filter chain has no filters")
	}

	for _, filter := range filterChain.Filters {
		if filter.Name == "envoy.filters.network.http_connection_manager" {
			typedConfig := filter.GetTypedConfig()
			if typedConfig == nil {
				t.Fatal("http_connection_manager has no typed config")
			}

			hcm := &v3hcm.HttpConnectionManager{}
			err := typedConfig.UnmarshalTo(hcm)
			require.NoError(t, err)

			// Marshal HCM to JSON for debugging
			if hcmJSON, err := json.Marshal(hcm); err == nil {
				fmt.Printf("HCM JSON: %s\n", string(hcmJSON))
			} else {
				fmt.Printf("Failed to marshal HCM to JSON: %v\n", err)
			}
			return hcm
		}
	}

	t.Fatal("http_connection_manager filter not found")
	return nil
}
