package server

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/rancher/log"
	"github.com/rancher/rancher-metadata/config"
	uuid "github.com/satori/go.uuid"
)

type MetadataController struct {
	metadataServers map[string]*MetadataServer
	versions        config.Versions
	version         string
	sync.Mutex
	versionCond           *sync.Cond
	subscribe             bool
	answersFileNamePrefix string
	reloadInterval        int64
}

func NewMetadataController(subscribe bool, answersFileNamePrefix string, reloadInterval int64) *MetadataController {
	return &MetadataController{
		versions:              (config.Versions)(nil),
		version:               "0",
		subscribe:             subscribe,
		answersFileNamePrefix: answersFileNamePrefix,
		reloadInterval:        reloadInterval,
	}
}

func (mc *MetadataController) Start() error {
	//register default metadata server
	mc.RegisterMetaDataServer(os.Getenv("CATTLE_URL"),
		os.Getenv("CATTLE_ACCESS_KEY"),
		os.Getenv("CATTLE_SECRET_KEY"), true, false)

	mc.versionCond = sync.NewCond(mc)
	if err := mc.LoadVersionsFromFile(); err != nil {
		return err
	}

	go func() {
		for {
			time.Sleep(5 * time.Second)
			mc.versionCond.Broadcast()
		}
	}()

	if mc.subscribe {
		for _, m := range mc.metadataServers {
			if err := m.Start(); err != nil {
				return err
			}
		}
	}

	return nil
}

func (mc *MetadataController) LoadVersionsFromFile() error {
	for _, m := range mc.metadataServers {
		err := m.loadVersionsFromFile()
		if err != nil {
			return fmt.Errorf("Failed to load answers from file: %v", err)
		}
	}
	mc.reloadVersions()

	return nil
}

func (mc *MetadataController) resetVersion() {
	mc.version = uuid.NewV4().String()
}

func (mc *MetadataController) mergeVersions() config.Versions {
	var external []config.Versions
	var local config.Versions
	for _, m := range mc.metadataServers {
		if m.local {
			local = m.GetVersions()
		} else {
			external = append(external, m.GetVersions())
		}
	}

	return config.MergeVersions(local, external, mc.version)
}

func (mc *MetadataController) GetVersions() config.Versions {
	mc.Lock()
	defer mc.Unlock()
	return mc.versions
}

func (mc *MetadataController) RegisterMetaDataServer(url string, accessKey string, secretKey string, local bool, subscribe bool) error {
	create := false
	if mc.metadataServers == nil {
		mc.metadataServers = make(map[string]*MetadataServer)
		create = true
	} else {
		existing, ok := mc.metadataServers[accessKey]
		if !ok {
			create = true
		} else if existing.accessKey != accessKey {
			create = true
		}

	}

	if !create {
		return nil
	}
	log.Infof("Registering metadata server [%s] with url [%s]", accessKey, url)

	m := NewMetaDataServer(url,
		accessKey, secretKey, local, mc.answersFileNamePrefix, mc.reloadInterval, mc.reloadVersions)

	if subscribe && mc.subscribe {
		if err := m.Start(); err != nil {
			return fmt.Errorf("Failed to register metadata server [%s] with url [%s]: [%v]", accessKey, url, err)
		}
	}
	mc.metadataServers[accessKey] = m
	log.Infof("Registered metadata server for [%s] with url [%s]", accessKey, url)
	return nil
}

func (mc *MetadataController) UnregisterMetaDataServer(UUID string) {
	if _, ok := mc.metadataServers[UUID]; !ok {
		return
	}
	log.Infof("Deregestring metadata server [%s]", UUID)

	if mc.subscribe {
		mc.metadataServers[UUID].Stop()
	}
	delete(mc.metadataServers, UUID)
	log.Infof("Deregistered metadata server [%s]", UUID)
}

func (mc *MetadataController) getExternalCredentials() []config.Credential {
	for _, s := range mc.metadataServers {
		if s.local {
			return s.GetExternalCredentials()
		}
	}
	return []config.Credential{}
}

func (mc *MetadataController) reloadVersions() {
	mc.Lock()
	defer mc.Unlock()
	creds := mc.getExternalCredentials()
	// sync subscribers here
	toAdd := make(map[string]config.Credential)

	for _, cred := range creds {
		toAdd[cred.PublicValue] = cred
	}

	toRemove := []string{}
	for key, server := range mc.metadataServers {
		if server.local {
			continue
		}
		if val, ok := toAdd[key]; !ok {
			toRemove = append(toRemove, server.accessKey)
		} else if server.URL != val.URL {
			toRemove = append(toRemove, server.accessKey)
		}
	}

	// 1. Deregister obsolete subscribers
	for _, UUID := range toRemove {
		mc.UnregisterMetaDataServer(UUID)
	}

	// 2. Merge versions
	mc.versions = mc.mergeVersions()
	mc.resetVersion()
	// 3. Register new subscribers
	for _, cred := range toAdd {
		err := mc.RegisterMetaDataServer(cred.URL, cred.PublicValue, cred.SecretValue, false, true)
		if err != nil {
			log.Error(err)
		}
	}

	mc.versionCond.Broadcast()
}

func (mc *MetadataController) LookupAnswer(wait bool, oldValue, version string, ip string, path []string, maxWait time.Duration) (interface{}, bool) {
	if !wait {
		v := mc.GetVersions()
		return v.Matching(version, ip, path)
	}

	if maxWait == time.Duration(0) {
		maxWait = time.Minute
	}

	if maxWait > 2*time.Minute {
		maxWait = 2 * time.Minute
	}

	start := time.Now()

	for {
		v := mc.GetVersions()
		val, ok := v.Matching(version, ip, path)
		if time.Now().Sub(start) > maxWait {
			return val, ok
		}
		if ok && fmt.Sprint(val) != oldValue {
			return val, ok
		}

		mc.versionCond.L.Lock()
		mc.versionCond.Wait()
		mc.versionCond.L.Unlock()
	}
}
