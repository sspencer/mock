# mock

Mock creates an HTTP server with *mocked* routes specified from a local file.  It allows for
rapid development and testing of (REST) API clients.  The routes are dynamically configured from
a watched file.

For added flexibility, Mock optionally just serves the specified directory.

This project was originally inspired by [localroast](https://github.com/caalberts/localroast).  
Thought it would be fun to recreate something similar with a less verbose API syntax.
The syntax is very similar to VSCode's [REST Client](https://marketplace.visualstudio.com/items?itemName=humao.rest-client) /
IntelliJ's [HTTP Client](https://www.jetbrains.com/help/idea/http-client-in-product-code-editor.html#creating-http-request-files). 

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

1. Mock API: `mock examples/user.http` or `cat my.http | mock` or even `mock < my.http`
2. Serve Directory: `mock .`  NOTE: can't combine serving a directory with serving `.http` files.
3. Each flag can be specified through ENV Vars:
   * `MOCK_PORT`:  ex/ "8080"
   * `MOCK_LOG`:   ex/ "1" or "true" -- bool type value
   * `MOCK_DELAY`: ex/ "1500ms"

If you're interested in developing `mock` *itself*, simply start `mock` with:

    go run cmd/main.go examples/user.api

## Response File

Responses are mocked in a text file.  Responses start with `###`, specify optional 
parameters, then the HTTP Method and PATH, followed by optional headers, an 
empty line and an optional body. Parameters in the path may be specified by preceding 
the parameter with a COLON.  To substitute this parameter in the response, surround 
the name with double curly brackets.

Examples:

    ### Return user
    GET /users/:id
    content-type: application/json

    {
        "id": "{{id}}"
        "name": "John Dough",
        "email": "john@dough.com"
    }

    ### Delete user
    # @status 204
    DELETE /users/:id

In general, syntax is:

    ### Response Name
    # @varname value (optional, zero or more)
    method path
    header      (optional zero or more)
                (empty line, required if body specified)
    body line 1 (optional)
    body line 2 (optional)
    
    ### Response 2
    ...

### Response Variables

Variables are specified after `###`, are optional, and are defined one per line.

1. `# @delay 500ms` delays response (defaults to 0, golang duration syntax)
2. `# @status 201` defines http status code (defaults to 200)
3. `# @file index.html` specifies body from external file (defaults to unspecified)

### Path Variables

Variables may be defined in the path, preceded by a colon.  For any path variable
defined in the path, `{{varname}}` in the body will be replaced with the value
of the variable.

    ### say hello
    GET /hello/:name
    content-type: text/plain

    Hello {{name}}!

Read more about the path variables syntax in Julien Schimdt's 
[httprouter](https://github.com/julienschmidt/httprouter),
the router used by `mock`.

### Headers

Headers are optional.  By default, every response will respond
with `content-type: "text/html; charset=utf-8"`.  

**NOTE** While it'd be nicer to default content type to `application/json`, 
HTTP Client plugins only highlight JSON bodies if the content-type
is specified.

### Body Variables

Besides replacing path variables in the body e.g. `{{id}}`, the following
variables will be replaced in the body:

* `{{$uuid}}` 
* `{{$timestamp}}`
* `{{$randomInt}}`

More may be added.

## Multiple Responses

The same Method and Path can be specified.  Each duplicate Method / Path adds
a new response to the entry.  As you request the same API, different responses
are returned in a round-robin fashion.

For example (not actual format):

    # @status 201
    POST /users
    { "id": 5 }

    # @status 201
    POST 201 /users
    { "id": 6 }

    # @status 201
    POST 400 /users
    { "id": 0 }

Will return the status codes `201`, `201`, `400` and responses `{ "id": 5 }`, 
`{ "id": 6 }`, `{ "id": 0 }` in order as you issue
`curl -XPOST http://localhost:8080/users` requests.


## Features

- [x] easy api specification similar to [HTTP Client](https://www.jetbrains.com/help/idea/http-client-in-product-code-editor.html)
- [x] specify multiple api files from command line
- [x] include external files
- [x] path variables
- [x] autoload file changes
- [x] multiple responses per Method / Path
- [x] dockerized

## Ideas

- [ ] support `@basicauth name pass` in response variable
- [ ] add {{body}} variables similar to what http client supports
    - $exampleServer
    - $isoTimestamp
    - $random.alphabetic
    - $random.alphanumeric
    - $random.email
    - $random.float
    - $random.hexadecimal
    - $random.integer
    - $random.uuid
    - $randomInt
    - $timestamp
    - $uuid
- [ ] Use [faker](https://github.com/jaswdr/faker) for body variables 
- [ ] Embed web server on different port that displays log of all requests/responses.  Use [ngrok](https://ngrok.com) as inspiration.
- [ ] Use Go lang text templates instead of ReplaceAll()
