package integration

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"time"

	"github.com/camunda-cloud/zeebe/clients/go/pkg/zbc"
	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/retry"
	"google.golang.org/grpc"
)

func (s *integrationTest) createPortForwardedClient(serviceName string) (zbc.Client, func(), error) {
	// NOTE: this only waits until the service is created, not until the underlying pods are ready to receive traffic
	k8s.WaitUntilServiceAvailable(s.T(), s.kubeOptions, serviceName, 90, 1*time.Second)

	// port forward the gateway service to avoid having to set up a public endpoint that the test can access externally
	localGatewayPort := k8s.GetAvailablePort(s.T())
	tunnel := k8s.NewTunnel(s.kubeOptions, k8s.ResourceTypeService, serviceName, localGatewayPort, 26500)

	// the gateway is not ready/receiving traffic until at least one leader is present
	s.waitUntilPortForwarded(tunnel, 30, 2*time.Second)

	endpoint := fmt.Sprintf("localhost:%d", localGatewayPort)
	client, err := zbc.NewClient(&zbc.ClientConfig{
		GatewayAddress:         endpoint,
		DialOpts:               []grpc.DialOption{},
		UsePlaintextConnection: true,
	})
	if err != nil {
		return nil, tunnel.Close, err
	}

	return client, func() { client.Close(); tunnel.Close() }, nil
}

func (s *integrationTest) createPortForwardedHttpClientWithPort(serviceName string, port int) (string, func()) {
	return s.createPortForwardedHttpClientWithPortAndContainerPort(serviceName, port, 8080)
}

func (s *integrationTest) createPortForwardedHttpClientWithPortAndContainerPort(serviceName string, port int, containerPort int) (string, func()) {
	// NOTE: this only waits until the service is created, not until the underlying pods are ready to receive traffic
	k8s.WaitUntilServiceAvailable(s.T(), s.kubeOptions, serviceName, 90, 1*time.Second)

	// remote port needs to be container port - not service port!
	tunnel := k8s.NewTunnel(s.kubeOptions, k8s.ResourceTypeService, serviceName, port, containerPort)

	// the gateway is not ready/receiving traffic until at least one leader is present
	s.waitUntilPortForwarded(tunnel, 30, 2*time.Second)

	endpoint := fmt.Sprintf("localhost:%d", port)
	return endpoint, func() {
		tunnel.Close()
	}
}

func (s *integrationTest) createPortForwardedHttpClient(serviceName string) (string, func()) {
	return s.createPortForwardedHttpClientWithPort(serviceName, k8s.GetAvailablePort(s.T()))
}

func (s *integrationTest) waitUntilPortForwarded(tunnel *k8s.Tunnel, retries int, sleepBetweenRetries time.Duration) {
	statusMsg := fmt.Sprintf("Waiting to port forward for endpoint %s", tunnel.Endpoint())
	message := retry.DoWithRetry(
		s.T(),
		statusMsg,
		retries,
		sleepBetweenRetries,
		func() (string, error) {
			err := tunnel.ForwardPortE(s.T())
			if err != nil {
				return "", err
			}

			return fmt.Sprintf("Endpoint %s is now forwarded", tunnel.Endpoint()), nil
		},
	)
	logger.Logf(s.T(), message)
}

func (s *integrationTest) createHttpClientWithJar() (http.Client, *cookiejar.Jar, error) {
	// setup http client with cookie jar - necessary to store tokens
	jar, err := cookiejar.New(nil)
	if err != nil {
		return http.Client{}, nil, err
	}
	httpClient := http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
	}
	return httpClient, jar, nil
}
