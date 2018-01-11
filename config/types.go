package config

// The top-level key in the JSON for the default (not client-specific answers)
const DEFAULT_KEY = "default"
const VERSION_KEY = "version"
const LATEST_KEY = "latest"
const ENVIRONMENT_KEY = "environments"
const METADATA_VERSION1 = "2015-07-25"
const METADATA_VERSION2 = "2015-12-19"
const METADATA_VERSION3 = "2016-07-29"

var SUPPORTED_VERSIONS = []string{METADATA_VERSION1, METADATA_VERSION2, METADATA_VERSION3}
var MAGIC_ARRAY_KEYS = []string{"name", "uuid"}

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
	Networks                        []interface{}
	Default                         map[string]interface{}
	Environment                     map[string]interface{}
	Credentials                     []Credential
}

type Credential struct {
	URL         string
	PublicValue string
	SecretValue string
}
