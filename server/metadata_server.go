package server

import (
	"fmt"

	"github.com/rancher/rancher-metadata/config"
)

type GlobalReloadFunc func()

type MetadataServer struct {
	URL                 string
	accessKey           string
	secretKey           string
	subscriber          *Subscriber
	versions            config.Versions
	globalReload        GlobalReloadFunc
	generator           *config.Generator
	local               bool
	version             string
	externalCredentials []config.Credential
	reloadInterval      int64
}

func NewMetaDataServer(URL string, accessKey string, secretKey string,
	local bool, answersFilePathPrefix string, reloadInterval int64, globalReload GlobalReloadFunc) *MetadataServer {

	return &MetadataServer{
		URL:            URL,
		accessKey:      accessKey,
		secretKey:      secretKey,
		local:          local,
		versions:       (config.Versions)(nil),
		globalReload:   globalReload,
		reloadInterval: reloadInterval,
		generator: config.NewGenerator(local, getAnswersFileName(answersFilePathPrefix,
			accessKey, local)),
	}
}

func getAnswersFileName(answersFilePathPrefix string, accessKey string, isLocal bool) string {
	if isLocal {
		return answersFilePathPrefix
	}
	return fmt.Sprintf("%s_%s", answersFilePathPrefix, accessKey)
}

func (ms *MetadataServer) Start() error {

	ms.subscriber = NewSubscriber(
		ms.URL,
		ms.accessKey,
		ms.secretKey,
		ms.generator,
		ms.reloadInterval,
		ms.setVersions,
	)
	if err := ms.subscriber.Subscribe(); err != nil {
		return fmt.Errorf("Failed to subscribe to url [%s]: %v", ms.URL, err)
	}
	return nil
}

func (ms *MetadataServer) Stop() {
	ms.subscriber.Unsubscribe()
}

func (ms *MetadataServer) loadVersionsFromFile() (err error) {
	neu, creds, err := ms.generator.LoadVersionsFromFile(true)
	if err != nil {
		return err
	}
	ms.setVersions(neu, creds, "")
	return nil
}

func (ms *MetadataServer) GetVersions() config.Versions {
	return ms.versions
}

func (ms *MetadataServer) setVersions(versions config.Versions, creds []config.Credential, version string) {
	ms.versions = versions
	ms.version = version
	ms.externalCredentials = creds
	ms.globalReload()
}

func (ms *MetadataServer) GetExternalCredentials() []config.Credential {
	return ms.externalCredentials
}
