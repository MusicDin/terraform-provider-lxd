package common

import (
	"sync"

	lxd "github.com/canonical/lxd/client"
	"github.com/canonical/lxd/shared/api"
)

var metadataCache map[string]*api.MetadataConfiguration
var metadataCacheLock sync.Mutex

func ServerMetadataConfiguration(name string, server lxd.InstanceServer) (*api.MetadataConfiguration, error) {
	metadataCacheLock.Lock()
	defer metadataCacheLock.Unlock()

	if metadataCache == nil {
		metadataCache = make(map[string]*api.MetadataConfiguration)
	}

	meta, ok := metadataCache[name]
	if ok {
		return meta, nil
	}

	meta, err := server.GetMetadataConfiguration()
	if err != nil {
		return nil, err
	}

	metadataCache[name] = meta
	return meta, nil
}
