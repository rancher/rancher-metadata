package config

import (
	"bytes"
	"compress/flate"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/ugorji/go/codec"
)

type Generator struct {
	supportedVersions []string
	local             bool
	delta             *MetadataDelta
	savedVersion      string
	jsonHandle        *codec.JsonHandle
	decoder           *MetadataDecoder
	answersFilePath   string
}

type MetadataDelta struct {
	Version string
	Data    []byte
	sync.Mutex
}

type MetadataDecoder struct {
	decoder *codec.Decoder
	sync.Mutex
}

func NewGenerator(local bool, answersFilePath string) *Generator {
	var generator *Generator
	if local {
		generator = &Generator{
			supportedVersions: []string{METADATA_VERSION1, METADATA_VERSION2, METADATA_VERSION3},
			local:             true,
		}
	} else {
		generator = &Generator{
			supportedVersions: []string{METADATA_VERSION3},
			local:             false,
		}
	}

	generator.jsonHandle = &codec.JsonHandle{
		BasicHandle: codec.BasicHandle{
			DecodeOptions: codec.DecodeOptions{
				InternString: true,
				MapType:      reflect.TypeOf(map[string]interface{}{}),
			},
		},
	}

	generator.decoder = &MetadataDecoder{}
	generator.delta = &MetadataDelta{
		Version: "0",
	}
	generator.answersFilePath = answersFilePath

	return generator
}

func (g *Generator) GenerateAnswers(data []map[string]interface{}) (Versions, []Credential, error) {
	versions := make(map[string]Answers)

	var creds []Credential
	for _, v := range g.supportedVersions {
		// 1. generate interim data
		interim := &Interim{
			UUIDToService:                   make(map[string]map[string]interface{}),
			UUIDToContainer:                 make(map[string]map[string]interface{}),
			UUIDToStack:                     make(map[string]map[string]interface{}),
			UUIDToHost:                      make(map[string]map[string]interface{}),
			StackUUIDToServicesUUID:         make(map[string][]string),
			ServiceUUIDNameToContainersUUID: make(map[string][]string),
			ContainerUUIDToContainerLink:    make(map[string]map[string]interface{}),
			ServiceUUIDToServiceLink:        make(map[string]map[string]interface{}),
			Networks:                        []interface{}{},
			Default:                         make(map[string]interface{}),
			Credentials:                     []Credential{},
			Environment:                     make(map[string]interface{}),
		}

		for _, o := range data {
			no := make(map[string]interface{}, len(o))
			for k, v := range o {
				no[k] = v
			}
			processMetadataObject(no, interim)
		}
		creds = interim.Credentials
		// 2. Generate versions from temp data
		if err := g.generateVersions(interim, v, versions); err != nil {
			return nil, nil, err
		}
	}

	//tag the latest
	versions[LATEST_KEY] = versions[METADATA_VERSION3]
	return versions, creds, nil
}

func (g *Generator) generateVersions(interim *Interim, version string, versions Versions) error {
	versionedData, err := applyVersionToData(*interim, version)
	if err != nil {
		return err
	}
	g.addToVersions(versions, version, versionedData)
	return nil
}

func (g *Generator) addToVersions(versions Versions, version string, versionedData *Interim) {
	answers := make(map[string]interface{})
	defaultAnswers := g.addDefaultToAnswers(answers, versionedData)
	if g.local {
		addClientToAnswers(answers, defaultAnswers, versionedData)
	}
	versions[version] = answers
}

func addClientToAnswers(answers Answers, defaultAnswers map[string]interface{}, versionedData *Interim) {
	for _, c := range versionedData.UUIDToContainer {
		if c["primary_ip"] == nil {
			continue
		}
		clientAnswers := make(map[string]interface{})
		self := make(map[string]interface{})
		self["container"] = c
		if c["stack_uuid"] != nil {
			self["stack"] = versionedData.UUIDToStack[c["stack_uuid"].(string)]
			service := versionedData.UUIDToService[getServiceUUID(c["service_uuid"].(string), c["service_name"].(string))]
			selfService := make(map[string]interface{})
			// to exclude token from service
			for k, v := range service {
				selfService[k] = v
			}
			service["token"] = nil
			self["service"] = selfService
		}
		if c["host_uuid"] != nil {
			self["host"] = versionedData.UUIDToHost[c["host_uuid"].(string)]
		}
		clientAnswers["self"] = self
		mergeDefaults(clientAnswers, defaultAnswers)
		answers[c["primary_ip"].(string)] = clientAnswers
	}
}

func mergeDefaults(clientAnswers map[string]interface{}, defaultAnswers map[string]interface{}) {
	for k, v := range defaultAnswers {
		_, exists := clientAnswers[k]
		if !exists {
			clientAnswers[k] = v
		}
	}
}

func (g *Generator) addDefaultToAnswers(answers Answers, versionedData *Interim) map[string]interface{} {
	defaultAnswers := make(map[string]interface{})
	var containers []interface{}
	for _, c := range versionedData.UUIDToContainer {
		if _, ok := defaultAnswers["containers"]; ok {
			containers = defaultAnswers["containers"].([]interface{})
		}
		containers = append(containers, c)
	}
	defaultAnswers["containers"] = containers

	var stacks []interface{}
	for _, s := range versionedData.UUIDToStack {
		if _, ok := defaultAnswers["stacks"]; ok {
			stacks = defaultAnswers["stacks"].([]interface{})
		}
		stacks = append(stacks, s)
	}
	defaultAnswers["stacks"] = stacks

	var services []interface{}
	for _, s := range versionedData.UUIDToService {
		if _, ok := defaultAnswers["services"]; ok {
			services = defaultAnswers["services"].([]interface{})
		}
		services = append(services, s)
	}
	defaultAnswers["services"] = services

	var hosts []interface{}
	for _, h := range versionedData.UUIDToHost {
		if _, ok := defaultAnswers["hosts"]; ok {
			hosts = defaultAnswers["hosts"].([]interface{})
		}
		hosts = append(hosts, h)
	}
	defaultAnswers["hosts"] = hosts
	defaultAnswers["networks"] = versionedData.Networks

	if val, ok := versionedData.Default["version"]; ok {
		defaultAnswers["version"] = val
	}

	if g.local {
		if selfVal, ok := versionedData.Default["self"]; ok {
			self := selfVal.(map[string]interface{})
			if hostVal, ok := self["host"]; ok {
				host := hostVal.(map[string]interface{})
				self["host"] = versionedData.UUIDToHost[host["uuid"].(string)]
			}
			defaultAnswers["self"] = self
		}
	}
	for key, value := range versionedData.Environment {
		if key == "metadata_kind" {
			continue
		}
		defaultAnswers[key] = value
	}
	answers[DEFAULT_KEY] = defaultAnswers
	return defaultAnswers
}

func applyVersionToData(modified Interim, version string) (*Interim, error) {
	// 1. Process containers
	for _, c := range modified.UUIDToContainer {
		switch version {
		case METADATA_VERSION3:
			if c["name"] != nil {
				c["name"] = strings.ToLower(c["name"].(string))
			}
			if c["service_name"] != nil {
				c["service_name"] = strings.ToLower(c["service_name"].(string))
			}
			if c["stack_name"] != nil {
				c["stack_name"] = strings.ToLower(c["stack_name"].(string))
			}
			if _, ok := c["ports"]; ok {
				// set port ip to 0.0.0.0 if not specified
				originalPorts := c["ports"].([]interface{})
				var newPorts []interface{}
				for _, p := range originalPorts {
					port := p.(string)
					splitted := strings.Split(port, ":")
					if len(splitted) == 3 {
						newPorts = append(newPorts, port)
					} else {
						port = fmt.Sprintf("0.0.0.0:%s", port)
						newPorts = append(newPorts, port)
					}
				}
				c["ports"] = newPorts
			}

		default:
			if _, ok := c["ports"]; ok {
				originalPorts := c["ports"].([]interface{})
				// set port ip to host's ip if not specified
				if len(originalPorts) > 0 {
					var newPorts []interface{}
					for _, p := range originalPorts {
						port := p.(string)
						splitted := strings.Split(port, ":")
						if len(splitted) == 3 && splitted[0] != "0.0.0.0" {
							newPorts = append(newPorts, port)
						} else {
							if len(splitted) == 3 {
								port = fmt.Sprintf("%s%s", c["host_ip"], strings.TrimPrefix(port, "0.0.0.0"))
							} else {
								port = fmt.Sprintf("%s:%s", c["host_ip"], port)
							}

							newPorts = append(newPorts, port)
						}
					}
					c["ports"] = newPorts
				}
			}
		}
		c["links"] = modified.ContainerUUIDToContainerLink[c["uuid"].(string)]
		//delete helper field (needed for the port)
		delete(c, "host_ip")
	}

	// 2. Process services
	for _, s := range modified.UUIDToService {
		sUUID := getServiceUUID(s["uuid"].(string), s["name"].(string))

		stackUUID := s["stack_uuid"].(string)
		// add itself to the stack list
		var svcUUIDs []string
		if _, ok := modified.StackUUIDToServicesUUID[stackUUID]; ok {
			svcUUIDs = modified.StackUUIDToServicesUUID[stackUUID]
		}
		svcUUIDs = append(svcUUIDs, getServiceUUID(s["uuid"].(string), s["name"].(string)))
		modified.StackUUIDToServicesUUID[stackUUID] = svcUUIDs
		var cs []interface{}
		var cNames []interface{}
		cUUIDs := modified.ServiceUUIDNameToContainersUUID[sUUID]
		if cUUIDs != nil {
			for _, cUUID := range cUUIDs {
				if c, ok := modified.UUIDToContainer[cUUID]; ok {
					cs = append(cs, c)
					cNames = append(cNames, c["name"].(string))
				}
			}
		}
		// add service links
		s["links"] = modified.ServiceUUIDToServiceLink[s["uuid"].(string)]
		switch version {
		case METADATA_VERSION1:
			s["containers"] = cNames
		case METADATA_VERSION2:
			s["containers"] = cs
		case METADATA_VERSION3:
			s["containers"] = cs
			s["name"] = strings.ToLower(s["name"].(string))
			s["stack_name"] = strings.ToLower(s["stack_name"].(string))
			s["primary_service_name"] = strings.ToLower(s["primary_service_name"].(string))
			if s["sidekicks"] != nil {
				sidekicks := s["sidekicks"].([]interface{})
				var lowercased []interface{}
				for _, sidekick := range sidekicks {
					lowercased = append(lowercased, strings.ToLower(sidekick.(string)))
				}
				s["sidekicks"] = lowercased
			}
			if s["links"] != nil {
				links := s["links"].(map[string]interface{})
				lowercased := make(map[string]interface{})
				for key, value := range links {
					lowercased[strings.ToLower(key)] = value
				}
				s["links"] = lowercased
			}
		}

		// populate stack/service info on container
		if cUUIDs != nil {
			for _, cUUID := range cUUIDs {
				if _, ok := modified.UUIDToContainer[cUUID]; ok {
					modified.UUIDToContainer[cUUID]["service_name"] = s["name"]
					modified.UUIDToContainer[cUUID]["service_uuid"] = s["uuid"]
					modified.UUIDToContainer[cUUID]["stack_name"] = s["stack_name"]
					modified.UUIDToContainer[cUUID]["stack_uuid"] = stackUUID
				}
			}
		}
	}

	// 3. Process stacks
	for _, s := range modified.UUIDToStack {
		var svcs []interface{}
		var svcsNames []interface{}
		svcsUUIDs := modified.StackUUIDToServicesUUID[s["uuid"].(string)]
		if svcsUUIDs != nil {
			for _, svcUUID := range svcsUUIDs {
				if svc, ok := modified.UUIDToService[svcUUID]; ok {
					svcs = append(svcs, svc)
					svcsNames = append(svcsNames, svc["name"].(string))
				}
			}
		}
		switch version {
		case METADATA_VERSION1:
			s["services"] = svcsNames
		case METADATA_VERSION2:
			s["services"] = svcs
		case METADATA_VERSION3:
			s["services"] = svcs
			s["name"] = strings.ToLower(s["name"].(string))
		}
	}

	// 4. process hosts
	for _, h := range modified.UUIDToHost {
		switch version {
		case METADATA_VERSION3:
			delete(h, "hostId")
		}
	}

	return &modified, nil
}

func processMetadataObject(o map[string]interface{}, interim *Interim) {
	if val, ok := o["metadata_kind"]; ok {
		switch val.(string) {
		case "container":
			addContainer(o, interim)
		case "stack":
			addStack(o, interim)
		case "network":
			addNetwork(o, interim)
		case "host":
			addHost(o, interim)
		case "defaultData":
			addDefault(o, interim)
		case "serviceContainerLink":
			addServiceContainerLink(o, interim)
		case "containerLink":
			addContainerLink(o, interim)
		case "serviceLink":
			addServiceLink(o, interim)
		case "service":
			addService(o, interim)
		case "environment":
			addEnvironment(o, interim)
		case "credential":
			addCredential(o, interim)
		}
	}
}

func addCredential(credData map[string]interface{}, interim *Interim) {
	cred := Credential{
		URL:         credData["url"].(string),
		PublicValue: credData["public_value"].(string),
		SecretValue: credData["secret_value"].(string),
	}
	interim.Credentials = append(interim.Credentials, cred)
}

func addEnvironment(env map[string]interface{}, interim *Interim) {
	interim.Environment = env
}

func addContainer(container map[string]interface{}, interim *Interim) {
	interim.UUIDToContainer[container["uuid"].(string)] = container
}

func addService(service map[string]interface{}, interim *Interim) {
	interim.UUIDToService[getServiceUUID(service["uuid"].(string), service["name"].(string))] = service
}

func addServiceContainerLink(link map[string]interface{}, interim *Interim) {
	UUID := getServiceUUID(link["service_uuid"].(string), link["service_name"].(string))
	var containerUUIDList []string
	if _, ok := interim.ServiceUUIDNameToContainersUUID[UUID]; ok {
		containerUUIDList = interim.ServiceUUIDNameToContainersUUID[UUID]
	}
	containerUUIDList = append(containerUUIDList, link["container_uuid"].(string))
	interim.ServiceUUIDNameToContainersUUID[UUID] = containerUUIDList
}

func addServiceLink(link map[string]interface{}, interim *Interim) {
	serviceUUID := link["service_uuid"].(string)
	links := make(map[string]interface{})
	if _, ok := interim.ServiceUUIDToServiceLink[serviceUUID]; ok {
		links = interim.ServiceUUIDToServiceLink[serviceUUID]
	}
	linkKey := link["key"].(string)
	links[linkKey] = link["value"].(string)

	interim.ServiceUUIDToServiceLink[serviceUUID] = links
}

func addContainerLink(link map[string]interface{}, interim *Interim) {
	containerUUID := link["container_uuid"].(string)
	links := make(map[string]interface{})
	if _, ok := interim.ContainerUUIDToContainerLink[containerUUID]; ok {
		links = interim.ContainerUUIDToContainerLink[containerUUID]
	}
	linkKey := link["key"].(string)
	links[linkKey] = link["value"].(string)

	interim.ContainerUUIDToContainerLink[containerUUID] = links
}

func getServiceUUID(UUID string, name string) string {
	return strings.ToLower(fmt.Sprintf("%s_%s", UUID, name))
}

func addStack(stack map[string]interface{}, interim *Interim) {
	interim.UUIDToStack[stack["uuid"].(string)] = stack
}

func addNetwork(network map[string]interface{}, interim *Interim) {
	interim.Networks = append(interim.Networks, network)
}

func addHost(host map[string]interface{}, interim *Interim) {
	interim.UUIDToHost[host["uuid"].(string)] = host
}

func addDefault(def map[string]interface{}, interim *Interim) {
	interim.Default = def
}

func (g *Generator) GenerateDelta(body io.Reader) ([]map[string]interface{}, string, error) {
	content, err := ioutil.ReadAll(body)
	if err != nil {
		return nil, "", err
	}

	r := flate.NewReader(bytes.NewBuffer(content))

	defer r.Close()

	g.decoder.Lock()
	defer g.decoder.Unlock()

	if g.decoder.decoder == nil {
		g.decoder.decoder = codec.NewDecoder(r, g.jsonHandle)
	} else {
		g.decoder.decoder.Reset(r)
	}

	var data []map[string]interface{}
	var version string
	for {
		var o map[string]interface{}
		err := g.decoder.decoder.Decode(&o)
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, "", err
		} else {
			data = append(data, o)
			kind := o["metadata_kind"]
			if kind == "defaultData" {
				version = o["version"].(string)
			}
		}
	}
	g.reloadDelta(version, content)
	return data, version, nil
}

func (g *Generator) reloadDelta(version string, data []byte) {
	g.delta.Lock()
	defer g.delta.Unlock()
	g.delta.Version = version
	g.delta.Data = data
}

func (g *Generator) SaveToFile(t time.Time) {
	g.delta.Lock()
	defer g.delta.Unlock()
	currentVersion := g.delta.Version
	if g.savedVersion != g.delta.Version && len(g.delta.Data) > 0 {
		err := g.saveDeltaToFile()
		if err != nil {
			logrus.Errorf("Failed to save delta to file: [%v]", err)
		} else {
			logrus.Debugf("Saved delta to file at [%v]", t)
			g.savedVersion = currentVersion
		}
	}
}

func (g *Generator) saveDeltaToFile() error {
	tempFile := g.answersFilePath + ".temp"
	out, err := os.Create(tempFile)
	if err != nil {
		return err
	}
	defer func() {
		out.Close()
		os.Remove(tempFile)
	}()

	err = json.NewEncoder(out).Encode(g.delta)
	if err != nil {
		return err
	}

	os.Rename(tempFile, g.answersFilePath)
	return nil
}

func (g *Generator) readVersionsFromFile() (Versions, []Credential, error) {
	var v Versions
	var md MetadataDelta
	f, err := os.Open(g.answersFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			logrus.Warn("Failed to find: ", g.answersFilePath)
			return v, nil, nil
		}
		return nil, nil, err
	}
	defer f.Close()

	err = json.NewDecoder(f).Decode(&md)
	if err != nil {
		return nil, nil, err
	}

	delta, _, err := g.GenerateDelta(bytes.NewBuffer(md.Data))
	if err != nil {
		return v, nil, err
	}

	v, creds, err := g.GenerateAnswers(delta)

	return v, creds, err
}

func (g *Generator) LoadVersionsFromFile(ignoreIfMissing bool) (Versions, []Credential, error) {
	logrus.Infof("Loading answers from file %s", g.answersFilePath)
	if _, err := os.Stat(g.answersFilePath); err == nil {
		versions, creds, err := g.readVersionsFromFile()
		if err != nil {
			return nil, nil, fmt.Errorf("Failed to load answers from file %s: %v", g.answersFilePath, err)
		}
		return versions, creds, nil
	} else {
		if ignoreIfMissing {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("Failed to load answers from file %s: %v", g.answersFilePath, err)
	}
}

func MergeVersions(local Versions, external []Versions, version string) Versions {
	if len(local.Versions()) == 0 {
		return local
	}
	var environments []interface{}
	for _, v := range external {
		if _, ok := v[METADATA_VERSION3][DEFAULT_KEY]; ok {
			externalData := v[METADATA_VERSION3][DEFAULT_KEY].(map[string]interface{})
			environments = append(environments, externalData)
		}
	}
	for _, v := range SUPPORTED_VERSIONS {
		for key, value := range local[v] {
			localData := value.(map[string]interface{})
			if v == METADATA_VERSION3 {
				localData[ENVIRONMENT_KEY] = environments
			}
			localData[VERSION_KEY] = version
			local[v][key] = localData
		}
	}

	return local
}
