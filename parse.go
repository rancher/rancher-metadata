package main

import (
	"github.com/ghodss/yaml"

	"io/ioutil"
	"os"

	log "github.com/Sirupsen/logrus"
)

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

	return tmp, err
}
