package main

import (
	yaml "gopkg.in/yaml.v2"

	"io/ioutil"
	"os"

	log "github.com/Sirupsen/logrus"
)

func convertVersionKeysToStrings(versions Versions) Versions {
	newVersions := make(Versions)

	for k, v := range versions {
		newVersions[k] = make(Answers)

		for k2, v2 := range v {
			newVersions[k][k2] = convertKeysToStrings(v2)
		}
	}

	return newVersions
}

func convertKeysToStrings(item interface{}) interface{} {
	switch typedDatas := item.(type) {

	case map[interface{}]interface{}:
		newMap := make(map[string]interface{})

		for key, value := range typedDatas {
			stringKey := key.(string)
			newMap[stringKey] = convertKeysToStrings(value)
		}
		return newMap

	case []interface{}:
		var newArray []interface{}
		for _, value := range typedDatas {
			newArray = append(newArray, convertKeysToStrings(value))
		}
		return newArray

	default:
		return item
	}
}

func ParseAnswers(path string) (out Versions, err error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.Warn("Failed to find: ", path)
		}
		return nil, err
	}

	var tmp Versions
	err = yaml.Unmarshal(data, &tmp)

	tmp = convertVersionKeysToStrings(tmp)

	return tmp, err
}
