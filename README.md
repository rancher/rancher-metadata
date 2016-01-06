rancher-metadata
===========

[![Build Status](http://ci.rancher.io/api/badge/github.com/rancher/rancher-metadata/status.svg?branch=master)](http://ci.rancher.io/github.com/rancher/rancher-metadata)


A simple HTTP server that returns EC2-style metadata information that varies depending on the source IP address making the request.  This package contains no Rancher-specific code, but is used in Rancher with an answer file that provide the requesting container information about itself, the service and stack it is a member of, the host it is running on, etc.

# Usage
```bash
  rancher-metadata --answers /path/to/answers.{yaml|json} [--debug] [--listen [host]:port] [--log path] [--pid-file path] [--xff]
```

# Compile
```
  godep go build
```

## CLI Options

Option      | Default        | Description
------------|----------------|------------
`--answers` | ./answers.yaml | Path to a JSON or YAML file with client-specific answers
`--debug`   | *off*          | Log more debugging info
`--listen`  | 0.0.0.0:80     | IP address and port to listen on
`--log`     | *none*         | Output log info to a file path instead of stdout
`--pid-file`| *none*         | Write the server PID to a file path on startup
`--xff`     | *off*          | Enable using the `X-Forwarded-For` header to determine source IP

## Answers File

The answers file provides all the structure that the metadata server responds with.
  - The top-level must be a map of version numbers, where each version should be an ISO-8601 date (yyyy-mm-dd) for compatibility with Rancher/Amazon EC2-style.
    - There may be an additional version called `latest` which should be the same as one of the dated version.  If one is not provided, the highest version ASCII-betically will be used as latest.
  - The 2nd level (top level of each version) must be a map of client IP addresses.  The request IP will be used to look up the appropriate set of answers.
    - A special key `default` will be checked if no answer is found in a client IP-specific entry.

### YAML
```yaml
'2015-12-19': &latest
  '10.1.2.2':
    key1: value1
    arbitrarily:
      nested:
        - YAML
        - of
        - any
        - type
        - 42
        - null
        - false
  '192.168.0.2':
    key1: value2

  # "default" is a special key that will be checked if no answer is found in a client IP-specific entry
  default:
    key1: value3

'2015-07-25':
  # Data for older revision

latest: *latest
```


## JSON
```javascript
{
  "2015-12-19": {
    "10.1.2.2": {
      "key1": "value1",
      "arbitrarily": {
        "nested": [
          "JSON", "of", "any", "type", 42, null, false
        ]
      }
    },

    "192.168.0.2": {
      "key1": "value2"
    },

    # "default" is a special key that will be checked if no answer is found in a client IP-specific entry
    "default": {
      "key1": "value3"
    }
  },

  "2015-07-25": {
    # Data for older revision
  },
}
```


## Answering queries
A query is answered by following the pieces of the path to walk the answers for the requested IP one step at a time.  If the key in the first section of the path is not found or there is no answers entry for the request IP, the `"default"` section is checked.  Defaults are *not* checked if there are client-specific answers they match one (or more) levels of the path.

If the request contains an `Accept` header requesting `application/json`, the response will be the matching subtree as a JSON document.

If the request contains an `Accept` header requesting `{application|text}/{yaml|x-yaml}`, the response will be the matching subtree as a YAML document.

## Contact
For bugs, questions, comments, corrections, suggestions, etc., open an issue in
 [rancher/rancher](//github.com/rancher/rancher/issues) with a title starting with `[rancher-metadata] `.

Or just [click here](//github.com/rancher/rancher/issues/new?title=%5Brancher-metadata%5D%20) to create a new issue.

License
=======
Copyright (c) 2015-2016 [Rancher Labs, Inc.](http://rancher.com)

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

[http://www.apache.org/licenses/LICENSE-2.0](http://www.apache.org/licenses/LICENSE-2.0)

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
