package main

import "sync"

type Versions map[string]Answers
type Answers map[string]interface{}

type Interim struct {
	UUIDToService                   map[string]map[string]interface{}
	UUIDToContainer                 map[string]map[string]interface{}
	UUIDToStack                     map[string]map[string]interface{}
	UUIDToHost                      map[string]map[string]interface{}
	ServiceUUIDNameToContainersUUID map[string][]string
	StackUUIDToServicesUUID         map[string][]string
	ContainerUUIDToContainerLink    map[string]map[string]interface{}
	ServiceUUIDToServiceLink        map[string]map[string]interface{}

	Networks []interface{}
	Default  map[string]interface{}
}

type MetadataDelta struct {
	Version string
	Data    []byte
	sync.Mutex
}
