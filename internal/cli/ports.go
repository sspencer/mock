package cli

import (
	"errors"
	"fmt"
	"net"
	"slices"

	"github.com/sspencer/mock/restclient"
)

// portPlan records the mock listeners: the default port that serves routes
// without a port of their own, and every bound mock port.
type portPlan struct {
	// defaultPort is the bound default port, or 0 when no route needed it.
	defaultPort int
	ports       map[int]bool
}

// listenMock binds the default port (-p) when a route has no port, and every
// port a route declares, in ascending order. If any bind fails, the listeners
// already bound are closed. A declared port may not be the admin UI port.
func listenMock(cfg config, methods []restclient.Method) ([]net.Listener, *portPlan, error) {
	declared := make(map[int]restclient.Method)
	var ports []int
	needDefault := false
	for _, method := range methods {
		if method.Port == 0 {
			needDefault = true
			continue
		}
		if _, ok := declared[method.Port]; !ok {
			declared[method.Port] = method
			ports = append(ports, method.Port)
		}
	}
	slices.Sort(ports)
	if !cfg.NoAdmin && cfg.AdminPort != 0 {
		if method, ok := declared[cfg.AdminPort]; ok {
			return nil, nil, fmt.Errorf("%s:%d: section %q uses port %d, which is the admin UI port; change the route's port, set -a (or MOCK_UI_PORT), or pass -x",
				method.Source, method.Line, method.Name, cfg.AdminPort)
		}
	}

	plan := &portPlan{ports: make(map[int]bool)}
	var listeners []net.Listener
	fail := func(err error) ([]net.Listener, *portPlan, error) {
		for _, ln := range listeners {
			_ = ln.Close()
		}
		return nil, nil, err
	}
	if needDefault {
		ln, err := net.Listen("tcp", listenAddress(cfg.Bind, cfg.Port))
		if err != nil {
			return fail(fmt.Errorf("server failed: %v", err))
		}
		listeners = append(listeners, ln)
		plan.defaultPort = ln.Addr().(*net.TCPAddr).Port
		plan.ports[plan.defaultPort] = true
	}
	for _, port := range ports {
		if plan.ports[port] {
			continue
		}
		ln, err := net.Listen("tcp", listenAddress(cfg.Bind, port))
		if err != nil {
			method := declared[port]
			return fail(fmt.Errorf("%s:%d: cannot serve section %q on port %d: %v", method.Source, method.Line, method.Name, port, err))
		}
		listeners = append(listeners, ln)
		plan.ports[port] = true
	}
	return listeners, plan, nil
}

// resolve returns a copy of methods with every route assigned to a bound
// port: routes without a port get the default port. It fails when a route
// needs a port that is not being served, as after a reload that adds a port;
// mock binds its listeners only at startup. A nil plan resolves nothing.
func (p *portPlan) resolve(methods []restclient.Method) ([]restclient.Method, error) {
	if p == nil {
		return methods, nil
	}
	out := slices.Clone(methods)
	var errs []error
	for i := range out {
		method := &out[i]
		if method.Port == 0 {
			if p.defaultPort == 0 {
				errs = append(errs, fmt.Errorf("%s:%d: section %q has no port, but mock is not serving the default port; give it a port or restart mock",
					method.Source, method.Line, method.Name))
				continue
			}
			method.Port = p.defaultPort
			continue
		}
		if !p.ports[method.Port] {
			errs = append(errs, fmt.Errorf("%s:%d: section %q uses port %d, which mock is not listening on; restart mock to serve new ports",
				method.Source, method.Line, method.Name, method.Port))
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// uiPort is the port the admin UI falls back to for sample requests: the
// default port, else the lowest bound port.
func (p *portPlan) uiPort() int {
	if p.defaultPort != 0 {
		return p.defaultPort
	}
	ports := make([]int, 0, len(p.ports))
	for port := range p.ports {
		ports = append(ports, port)
	}
	if len(ports) == 0 {
		return 0
	}
	return slices.Min(ports)
}
