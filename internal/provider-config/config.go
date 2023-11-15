package config

import (
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	lxd "github.com/canonical/lxd/client"
	lxd_config "github.com/canonical/lxd/lxc/config"
	lxd_shared "github.com/canonical/lxd/shared"
	lxd_api "github.com/canonical/lxd/shared/api"
	"github.com/terraform-lxd/terraform-provider-lxd/internal/utils"
)

// supportedLXDVersions defines LXD versions that are supported by the provider.
const supportedLXDVersions = ">= 4.0.0"

// A global mutex.
var mutex sync.RWMutex

// LxdProviderRemoteConfig represents LXD remote/server data as defined
// in a user's Terraform schema/configuration.
type LxdProviderRemoteConfig struct {
	Name         string
	Address      string
	Port         string
	Password     string
	Scheme       string
	Bootstrapped bool
}

// LxdProviderConfig contains the Provider configuration and initialized
// remote servers.
type LxdProviderConfig struct {
	// AcceptServerCertificates toggles if an LXD remote SSL certificate
	// should be accepted.
	acceptServerCertificate bool

	// refreshInterval is a custom interval for communicating with remote
	// LXD servers.
	refreshInterval time.Duration

	// LXDConfig is the converted form of terraformLXDConfig
	// in LXD's native data structure. This is lazy-loaded / created
	// only when a connection to an LXD remote/server happens.
	// https://github.com/canonical/lxd/blob/main/lxc/config/config.go
	lxdConfig lxd_config.Config

	// remotes is a map of LXD remotes which the user has defined in
	// the Terraform schema/configuration.
	remotes map[string]LxdProviderRemoteConfig

	// servers is a map of client connections to LXD remote servers.
	// These are lazy-loaded / created only when a connection to an LXD
	// remote/server is established.
	//
	// While a client can also be retrieved from LXDConfig, this map serves
	// an additional purpose of ensuring Terraform has successfully
	// connected and authenticated to each defined LXD server/remote.
	servers map[string]lxd.Server

	// This is a mutex used to handle concurrent reads/writes.
	mux sync.RWMutex
}

// NewLxdProvider returns initialized LXD provider structure. This struct is
// used to store information about this Terraform provider's configuration for
// reference throughout the lifecycle.
func NewLxdProvider(lxdConfig lxd_config.Config, refreshInterval time.Duration, acceptServerCert bool) *LxdProviderConfig {
	return &LxdProviderConfig{
		acceptServerCertificate: acceptServerCert,
		refreshInterval:         refreshInterval,
		lxdConfig:               lxdConfig,
		remotes:                 make(map[string]LxdProviderRemoteConfig),
		servers:                 make(map[string]lxd.Server),
	}

}

// Remote returns LXD remote with the given name or default otherwise.
func (p *LxdProviderConfig) Remote(name string) *LxdProviderRemoteConfig {
	p.mux.RLock()
	defer p.mux.RUnlock()

	remote, ok := p.remotes[name]
	if !ok || name == "" {
		remote, ok = p.remotes[p.lxdConfig.DefaultRemote]
		if !ok {
			panic(fmt.Errorf("Remote %q not found (default: %q)", name, p.lxdConfig.DefaultRemote))
		}
	}

	return &remote
}

// SetRemote set LXD remote for the given name.
func (p *LxdProviderConfig) SetRemote(remote LxdProviderRemoteConfig, isDefault bool) {
	p.mux.Lock()
	defer p.mux.Unlock()

	if isDefault {
		p.lxdConfig.DefaultRemote = remote.Name
	}

	p.remotes[remote.Name] = remote
}

// setLxdServer set LXD server for the given name.
func (p *LxdProviderConfig) setLxdConfigRemote(name string, remote lxd_config.Remote) {
	p.mux.Lock()
	defer p.mux.Unlock()

	p.lxdConfig.Remotes[name] = remote
}

// setLxdServer set LXD server for the given name.
func (p *LxdProviderConfig) getLxdConfigRemote(name string) lxd_config.Remote {
	p.mux.RLock()
	defer p.mux.RUnlock()

	return p.lxdConfig.Remotes[name]
}

// getLxdConfigInstanceServer will retrieve an LXD InstanceServer client
// in a conncurrent-safe way.
func (p *LxdProviderConfig) getLxdConfigInstanceServer(remoteName string) (lxd.InstanceServer, error) {
	p.mux.RLock()
	defer p.mux.RUnlock()

	instServer, err := p.lxdConfig.GetInstanceServer(remoteName)
	return instServer, err
}

// getLxdConfigImageServer will retrieve an LXD ImageServer client
// in a conncurrent-safe way.
func (p *LxdProviderConfig) getLxdConfigImageServer(remoteName string) (lxd.ImageServer, error) {
	p.mux.RLock()
	defer p.mux.RUnlock()

	imgServer, err := p.lxdConfig.GetImageServer(remoteName)
	return imgServer, err
}

// InstanceServer returns a LXD InstanceServer client for the given remote.
// An error is returned if the remote is not a InstanceServer.
func (p *LxdProviderConfig) InstanceServer(remoteName string) (lxd.InstanceServer, error) {
	server, err := p.server(remoteName)
	if err != nil {
		return nil, err
	}

	connInfo, err := server.GetConnectionInfo()
	if err != nil {
		return nil, err
	}

	protocol := connInfo.Protocol
	if protocol == "lxd" {
		return server.(lxd.InstanceServer), nil
	}

	err = fmt.Errorf("Remote (%s / %s) is not an InstanceServer", remoteName, protocol)
	return nil, err
}

// ImageServer returns a LXD ImageServer client for the given remote.
// An error is returned if the remote is not an ImageServer.
func (p *LxdProviderConfig) ImageServer(remoteName string) (lxd.ImageServer, error) {
	server, err := p.server(remoteName)
	if err != nil {
		return nil, err
	}

	connInfo, err := server.GetConnectionInfo()
	if err != nil {
		return nil, err
	}

	if connInfo.Protocol == "simplestreams" || connInfo.Protocol == "lxd" {
		return server.(lxd.ImageServer), nil
	}

	err = fmt.Errorf("Remote (%s / %s / %s) is not an ImageServer", remoteName, connInfo.Addresses[0], connInfo.Protocol)
	return nil, err
}

// getServer returns a server for the named remote. The returned server
// can be either of type ImageServer or InstanceServer.
func (p *LxdProviderConfig) server(remoteName string) (lxd.Server, error) {
	remote := p.Remote(remoteName)
	if remote == nil {
		return nil, fmt.Errorf("LXD remote %q is not defined", remoteName)
	}

	// Check if there is an already initialized LXD server.
	p.mux.Lock()
	server, ok := p.servers[remote.Name]
	p.mux.Unlock()
	if ok {
		return server, nil
	}

	// If a client was not already created, create a new one.
	if remote != nil && !remote.Bootstrapped {
		err := p.createServer(*remote)
		if err != nil {
			return nil, fmt.Errorf("Unable to create client for remote [%s]: %s", remoteName, err)
		}
	}

	lxdRemoteConfig := p.getLxdConfigRemote(remote.Name)

	// If remote address is not provided or is only set to the prefix for
	// Unix sockets (`unix://`) then determine which LXD directory
	// contains a writable unix socket.
	if lxdRemoteConfig.Addr == "" || lxdRemoteConfig.Addr == "unix://" {
		lxdDir, err := determineLxdDir()
		if err != nil {
			return nil, err
		}

		_ = os.Setenv("LXD_DIR", lxdDir)
	}

	var err error

	switch lxdRemoteConfig.Protocol {
	case "simplestreams":
		server, err = p.getLxdConfigImageServer(remote.Name)
		if err != nil {
			return nil, err
		}
	default:
		server, err = p.getLxdConfigInstanceServer(remote.Name)
		if err != nil {
			return nil, err
		}

		// Ensure that LXD version meets the provider's version constraint.
		err := verifyLxdServerVersion(server.(lxd.InstanceServer))
		if err != nil {
			return nil, fmt.Errorf("Remote %q: %v", remote.Name, err)
		}
	}

	// Add the server to the lxdServer map (cache).
	p.mux.Lock()
	defer p.mux.Unlock()

	p.servers[remote.Name] = server

	return server, nil
}

// createClient will create an LXD client for a given remote.
// The client is then stored in the lxdProvider.Config collection of clients.
func (p *LxdProviderConfig) createServer(remote LxdProviderRemoteConfig) error {
	if remote.Address == "" {
		return nil
	}

	daemonAddr, err := determineLxdDaemonAddr(remote)
	if err != nil {
		return fmt.Errorf("Unable to determine daemon address for remote %q: %s", remote.Name, err)
	}

	lxdRemote := lxd_config.Remote{Addr: daemonAddr}
	p.setLxdConfigRemote(remote.Name, lxdRemote)

	if remote.Scheme == "https" {
		p.mux.RLock()
		// If the LXD remote's certificate does not exist on the client...
		certPath := p.lxdConfig.ServerCertPath(remote.Name)
		p.mux.RUnlock()

		if !lxd_shared.PathExists(certPath) {
			// Try to obtain an early connection to the remote server.
			// If it succeeds, then either the certificates between
			// the remote and the client have already been exchanged
			// or PKI is being used.
			instServer, _ := p.getLxdConfigInstanceServer(remote.Name)

			err := connectToLxdServer(instServer)
			if err != nil {
				// Either PKI isn't being used or certificates haven't been
				// exchanged. Try to add the remote server certificate.
				if p.acceptServerCertificate {
					err := fetchLxdServerCertificate(lxdRemote, certPath)
					if err != nil {
						return fmt.Errorf("Failed to get remote server certificate: %s", err)
					}
				} else {
					return fmt.Errorf("Unable to communicate with remote server. Either set " +
						"accept_remote_certificate to true or add the remote out of band " +
						"of Terraform and try again.")
				}
			}
		}

		// Set bootstrapped to true to prevent an infinite loop.
		// This is required for situations when a remote might be
		// defined in a config.yml file but the client has not yet
		// exchanged certificates with the remote.
		remote.Bootstrapped = true
		p.SetRemote(remote, false)

		// Finally, make sure the client is authenticated.
		instServer, err := p.InstanceServer(remote.Name)
		if err != nil {
			return err
		}

		err = authenticateToLxdServer(instServer, remote.Password)
		if err != nil {
			return err
		}
	}

	return nil
}

// connectToLxdServer makes a simple GET request to the servers API to ensure
// connection can be successfully established.
func connectToLxdServer(instServer lxd.InstanceServer) error {
	if instServer == nil {
		return fmt.Errorf("Instance server is nil")
	}

	_, _, err := instServer.GetServer()
	if err != nil {
		return err
	}

	return nil
}

// authenticateToLXDServer authenticates to a given remote LXD server.
// If successful, the LXD server becomes trusted to the LXD client,
// and vice-versa.
func authenticateToLxdServer(instServer lxd.InstanceServer, password string) error {
	mutex.Lock()
	defer mutex.Unlock()

	server, _, err := instServer.GetServer()
	if err != nil {
		return err
	}

	if server.Auth == "trusted" {
		return nil
	}

	req := lxd_api.CertificatesPost{}
	req.Password = password
	req.Type = "client"

	err = instServer.CreateCertificate(req)
	if err != nil {
		return fmt.Errorf("Unable to authenticate with remote server: %s", err)
	}

	_, _, err = instServer.GetServer()
	if err != nil {
		return err
	}

	return nil
}

// fetchServerCertificate will attempt to retrieve a remote LXD server's
// certificate and save it to the servercerts path.
func fetchLxdServerCertificate(lxdRemote lxd_config.Remote, certPath string) error {
	certificate, err := lxd_shared.GetRemoteCertificate(lxdRemote.Addr, "terraform-provider-lxd/2.0")
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(certPath), 0750)
	if err != nil {
		return fmt.Errorf("Failed to create server cert dir: %v", err)
	}

	certFile, err := os.Create(certPath)
	if err != nil {
		return err
	}

	defer certFile.Close()

	return pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})
}

// verifyLXDVersion verifies whether the version of target LXD server matches the
// provider's required version contraint.
func verifyLxdServerVersion(instServer lxd.InstanceServer) error {
	server, _, err := instServer.GetServer()
	if err != nil {
		return err
	}

	serverVersion := server.Environment.ServerVersion
	ok, err := utils.CheckVersion(serverVersion, supportedLXDVersions)
	if err != nil {
		return err
	}

	if !ok {
		return fmt.Errorf("LXD server with version %q does not meet the required version constraint: %q", serverVersion, supportedLXDVersions)
	}

	return nil
}

// determineLxdDaemonAddr determines address of the LXD server daemon.
func determineLxdDaemonAddr(remote LxdProviderRemoteConfig) (string, error) {
	var daemonAddr string

	if remote.Address == "" {
		switch remote.Scheme {
		case "unix", "":
			daemonAddr = fmt.Sprintf("unix:%s", remote.Address)
		case "https":
			daemonAddr = fmt.Sprintf("https://%s:%s", remote.Address, remote.Port)
		}
	}

	return daemonAddr, nil
}

// determineLxdDir determines which standard LXD directory contains a writable UNIX socket.
// If environment variable LXD_DIR or LXD_SOCKET is set, the function will return LXD directory
// based on the value from any of those variables.
func determineLxdDir() (string, error) {
	lxdSocket, ok := os.LookupEnv("LXD_SOCKET")
	if ok {
		if utils.IsSocketWritable(lxdSocket) {
			return filepath.Dir(lxdSocket), nil
		}

		return "", fmt.Errorf("Environment variable LXD_SOCKET points to either a non-existing or non-writable unix socket")
	}

	lxdDir, ok := os.LookupEnv("LXD_DIR")
	if ok {
		socketPath := filepath.Join(lxdDir, "unix.socket")
		if utils.IsSocketWritable(socketPath) {
			return lxdDir, nil
		}

		return "", fmt.Errorf("Environment variable LXD_DIR points to a LXD directory that does not contain a writable unix socket")
	}

	lxdDirs := []string{
		"/var/lib/lxd",
		"/var/snap/lxd/common/lxd",
	}

	// Iterate over LXD directories and find a writable unix socket.
	for _, lxdDir := range lxdDirs {
		socketPath := filepath.Join(lxdDir, "unix.socket")
		if utils.IsSocketWritable(socketPath) {
			return lxdDir, nil
		}
	}

	return "", fmt.Errorf("LXD socket with write permissions not found. Searched LXD directories: %v", lxdDirs)
}