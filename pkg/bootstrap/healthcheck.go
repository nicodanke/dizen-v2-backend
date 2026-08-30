package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
)

// ErrUnhealthy is returned when the probe did not get a 200.
var ErrUnhealthy = errors.New("the service is not answering /livez")

// HealthcheckTimeout bounds the self-probe. It is short because Docker gives the command a
// timeout of its own and a probe that hangs is a probe that reports nothing.
const HealthcheckTimeout = 3 * time.Second

// Healthcheck probes the service's own /livez and reports whether it answered.
//
// It exists because the production image is distroless: there is no curl and no shell, so
// the HEALTHCHECK has to be the binary probing itself.
func Healthcheck(port int) error {
	ctx, cancel := context.WithTimeout(context.Background(), HealthcheckTimeout)
	defer cancel()

	url := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) + "/livez"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building the healthcheck request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("probing %s: %w", url, err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s answered %d", ErrUnhealthy, url, resp.StatusCode)
	}

	return nil
}
