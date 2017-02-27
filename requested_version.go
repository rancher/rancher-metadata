package main

import "sync"

var (
	requestedVersion     string
	requestedVersionLock sync.Mutex
)

func SetRequestedVersion(version string) {
	requestedVersionLock.Lock()
	requestedVersion = version
	requestedVersionLock.Unlock()
}

func GetRequestedVersion() string {
	requestedVersionLock.Lock()
	defer requestedVersionLock.Unlock()
	if requestedVersion == "0" || len(requestedVersion) == 0 {
		return ""
	}
	return requestedVersion
}
