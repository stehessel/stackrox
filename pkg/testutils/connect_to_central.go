package testutils

import (
	"context"
	"crypto/tls"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stackrox/rox/pkg/clientconn"
	"github.com/stackrox/rox/pkg/httputil"
	"github.com/stackrox/rox/pkg/mtls"
	"github.com/stackrox/rox/pkg/netutil"
	"github.com/stackrox/rox/pkg/stringutils"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

const (
	usernameEnvVar = "ROX_USERNAME"
	passwordEnvVar = "ROX_PASSWORD"

	apiEndpointEnvVar = "API_ENDPOINT"

	defaultUsername     = "admin"
	defaultPasswordPath = "deploy/k8s/central-deploy/password"
	defaultAPIEndpoint  = "localhost:8000"
)

var (
	_, inCI = os.LookupEnv("CI")
)

func mustGetEnvVarInCI(t T, envVar string) string {
	value := os.Getenv(envVar)
	if inCI {
		require.NotEmpty(t, value, "Please set %s", envVar)
	}
	return value
}

// RoxUsername returns the rox basic auth username (or fails the test if not found).
func RoxUsername(t T) string {
	return stringutils.FirstNonEmpty(mustGetEnvVarInCI(t, usernameEnvVar), defaultUsername)
}

// RoxPassword returns the rox basic auth password (or fails the test if not found).
func RoxPassword(t T) string {
	if pw := mustGetEnvVarInCI(t, passwordEnvVar); pw != "" {
		return pw
	}

	pwFromFileBytes, err := ioutil.ReadFile(filepath.Join(GetTestWorkspaceDir(t), defaultPasswordPath))
	require.NoErrorf(t, err, "no password set via %s, and could not read password file")

	return strings.TrimSpace(string(pwFromFileBytes))
}

// RoxAPIEndpoint returns the central API endpoint (or fails the test if not found).
func RoxAPIEndpoint(t T) string {
	return stringutils.FirstNonEmpty(mustGetEnvVarInCI(t, apiEndpointEnvVar), defaultAPIEndpoint)
}

// GRPCConnectionToCentral returns a GRPC connection to Central, which can be used in E2E tests.
// It fatals the test if there's an error.
func GRPCConnectionToCentral(t *testing.T) *grpc.ClientConn {
	endpoint := RoxAPIEndpoint(t)
	host, _, _, err := netutil.ParseEndpoint(endpoint)
	require.NoError(t, err)

	opts := clientconn.Options{
		TLS: clientconn.TLSConfigOptions{
			InsecureSkipVerify: true,
			ServerName:         host,
		},
	}
	opts.ConfigureBasicAuth(RoxUsername(t), RoxPassword(t))
	conn, err := clientconn.GRPCConnection(context.Background(), mtls.CentralSubject, endpoint, opts)
	require.NoError(t, err)
	return conn
}

// HTTPClientForCentral returns an *http.Client for talking to central in tests. Basic auth credentials and
// the hostname and scheme part of the URL may be omitted.
func HTTPClientForCentral(t T) *http.Client {
	baseTransport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	endpoint := RoxAPIEndpoint(t)
	user := RoxUsername(t)
	pw := RoxPassword(t)

	transport := httputil.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		modReq := req.Clone(req.Context())
		modReq.SetBasicAuth(user, pw)
		if modReq.URL.Host == "" {
			modReq.URL.Host = endpoint
		}
		if modReq.URL.Scheme == "" {
			modReq.URL.Scheme = "https"
		}
		resp, err := baseTransport.RoundTrip(modReq)
		if err != nil {
			return nil, err
		}
		resp.Request = req
		return resp, nil
	})

	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}

	return client
}
