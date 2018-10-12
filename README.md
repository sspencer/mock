# mock

Mock creates a HTTP server with *mocked* routes specified from a local file.  It allows for
rapid development and testing of (REST) API clients.  The routes are dynamically configured from
a watched file.

This project is inspired by [localroast](https://github.com/caalberts/localroast).  Thought
it would be fun to recreate something similar with a less verbose API syntax.

## Build/Install

    make

## Run / Develop

    mock [-p PORT] [-r] examples/user.api
        -p INT   port to run server on (default 8080)
        -r       enable request logging

If you're interested in developing, simply run it with:

this    go run cmd/main.go examples/user.api

## API File

Routes are mocked in a text file.  The HTTP Method, Status Code and Path are specified
on the first line.  All remaining text until the next empty line will be treated as a
JSON response. Parameters in the path may be specified by preceding the parameter with
a COLON.  

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
        "id": 5
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





