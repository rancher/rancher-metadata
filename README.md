rancher-metadata
===========

[![Build Status](http://drone.rancher.io/api/badge/github.com/rancher/rancher-metadata/status.svg?branch=master)](http://drone.rancher.io/github.com/rancherio/rancher-metadata)


A simple HTTP server that returns EC2-style introspective information that varies depending on the IP address making the request.

# Usage
```bash
  rancher-metadata [--debug] [--listen host:port] [--log path] [--pid-file path]--answers /path/to/answers.json
```

# Compile
```
  godep go build
```

## CLI Options

Option      | Default        | Description
------------|----------------|------------
`--debug`   | *off*          | If present, more debug info is logged
`--listen`  | 0.0.0.0:80     | IP address and port to listen on
`--answers` | ./answers.json | Path to a JSON file with client-specific answers
`--log`     | *none*         | Output log info to a file path instead of stdout
`--pid-file`| *none*         | Write the server PID to a file path on startup

## JSON Answers File
```javascript
{
  "10.1.2.2": {
    "key1": "value1"
    "arbitrarily": {
      "nested": [
        "JSON", "of", "any", "type", 42, null, false
      ]
    }
  },

  "192.168.0.2": {
    "key1": "value2"
  },

  // "default" is a special key that will be checked if no answer is found in a client IP-specific entry
  "default": {
    "key1": "value3"
  }
}
```

## Answering queries
A query is answered by following the pieces of the path to walk the answers for the requested IP one step at a time.  If the key in the first section of the path is not found or there is no answers entry for the request IP, the `"default"` section is checked.  Defaults are *not* checked if there are client-specific answers they match one (or more) levels of the path.

If the request contains an `Accept` header requesting `application/json`, the response will be the matching subtree from the JSON answer file.

## Contact
For bugs, questions, comments, corrections, suggestions, etc., open an issue in
 [rancher/rancher](//github.com/rancher/rancher/issues) with a title starting with `[rancher-metadata] `.

Or just [click here](//github.com/rancher/rancher/issues/new?title=%5Brancher-metadata%5D%20) to create a new issue.

License
=======
Copyright (c) 2015 [Rancher Labs, Inc.](http://rancher.com)

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

[http://www.apache.org/licenses/LICENSE-2.0](http://www.apache.org/licenses/LICENSE-2.0)

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
