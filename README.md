# mock

Mock creates an HTTP server with *mocked* routes specified from a local file.  It allows for
rapid development and testing of (REST) API clients.  The routes are dynamically configured from
a watched file.

For added flexibility, Mock optionally just serves the specified directory.

This project was originally inspired by [localroast](https://github.com/caalberts/localroast).  
Thought it would be fun to recreate something similar with a less verbose API syntax.

## Build/Install

    make

## Run / Develop

    mock [flags] [input_file1] [input_file2]
      -d string
            delay server responses (default "0ms")
      -p int
            port to run server on (default 8080)
      -r    
            log the request

1. Mock API: `mock examples/user.api` or `cat my.api | mock` or even `mock < my.api`
2. Serve Directory: `mock .`  NOTE: can't combine serving a directory with serving api files.
3. Each flag can be specified through ENV Vars:
   * `MOCK_PORT`:  ex/ "8080"
   * `MOCK_LOG`:   ex/ "1" or "true" -- bool type value
   * `MOCK_DELAY`: ex/ "1500ms"

If you're interested in developing `mock` *itself*, simply start `mock` with:

    go run cmd/main.go examples/user.api

## API File

Routes are mocked in a text file.  The HTTP Method, Status Code and Path are specified
on the first line.  All remaining text until the next empty line will be treated as a
JSON response. Parameters in the path may be specified by preceding the parameter with
a COLON.  To substitute this parameter in the response, surround the name with double
curly brackets.  See example below (or [examples/user.api](examples/user.api)).

There is an optional fourth parameter that specifies either the
non-default (`application/json`) content-type, or a file to be served as the response body.

    METHOD STATUS PATH [optional parameter]
    body line 1
    body line 2
    ...
    body line n

For example:

    GET 200 /users/:id
    {
        "id": "{{id}}"
        "name": "John Dough",
        "email": "john@dough.com"
    }

### Optional Parameters

You may serve a non-json content-type like by marking it with double quotes:

    GET 200 /hello "text/plain"
    Hello World

Or include a large response with a local file by prepending it with an @ symbol:

    GET 200 /users @users.json

1. The content-type of included files will be guessed based on the file's extension.
2. Files are assumed to be relative to the API file (see [examples/](examples/)).

### New for 2021

#### Multiple Responses

The same Method and Path may now be specified.  Each duplicate Method / Path adds
a new response to the entry.  As you request the same API, different responses
are returned in a round-robin fashion.

For example:

    POST 201 /users
    { "id": 5 }

    POST 201 /users
    { "id": 6 }

    POST 400 /users
    { "id": 0 }

Will return the status codes `201`, `201`, `400` and responses `{ "id": 5 }`, 
`{ "id": 6 }`, `{ "id": 0 }` in order as you issue
`curl -XPOST http://localhost:8080/users` commands.

#### Multiline Responses

Include longer inline responses that can include empty lines by using triple quotes:

    GET 200 /long "text/plain"
    """
    First line

    Last line, one skipped.
    """

## Features

- [x] easy api specification
- [x] specify multiple api files from command line
- [x] include external files
- [x] path variables
- [x] autoload file changes
- [x] multiple responses per Method / Path
- [x] dockerized

## Ideas

- [ ] basic auth per method (basic auth: user=hello pass=world)
- [ ] specify *random* or *sequential* for endpoints with more than 1 response
- [ ] :auto_id and :auto_uuid path variables in response to auto increment (or randomly generate) ids
- [ ] specify enumerated values for path variables and randomly choose value in response :rand_state
 