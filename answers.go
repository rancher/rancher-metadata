package main

import (
	log "github.com/Sirupsen/logrus"
	"reflect"
	"strconv"
	"strings"
)

func (answers *Answers) Matching(path string, answerKey string) (interface{}, bool) {
	var out interface{}

	// Try the client's IP
	all, ok := (*answers)[answerKey]
	if ok == false {
		// Try the default key because no entry for the client existed
		if answerKey == DEFAULT_KEY {
			return nil, false
		} else {
			log.Debugf("No answers for %s, trying %s", answerKey, DEFAULT_KEY)
			return answers.Matching(path, DEFAULT_KEY)
		}
	}

	if len(path) == 0 {
		return all, true
	}

	out, ok = valueForPath(&all, path)
	if ok {
		return out, true
	}

	return nil, false
}

func valueForPath(in *interface{}, path string) (interface{}, bool) {
	out := *in
	parts := strings.Split(path, "/")

	for _, key := range parts {
		valid := false

		switch v := out.(type) {
		case []interface{}:
			idx, err := strconv.ParseInt(key, 10, 64)
			if err == nil {
				// Is the part is a number, treat it like an array index
				if idx >= 0 && idx < int64(len(v)) {
					out = v[idx]
					valid = true
				}
			} else {
				// Otherwise maybe it's the name of a child map
				vAry, _ := out.([]interface{})
				for childK, childV := range vAry {
					childMap, ok := childV.(map[string]interface{})
					if ok && childMap[MAGIC_ARRAY_KEY] == key {
						out = vAry[childK]
						valid = true
						break
					}
				}
			}

		case map[string]interface{}:
			out, valid = v[key]

		default:
			t := reflect.TypeOf(out)
			log.Debug("Unknown type %s at /%s", t.String(), path)
		}

		if valid == false {
			return nil, false
		}
	}

	return out, true
}
