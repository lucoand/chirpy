# Chirpy

boot.dev webserver project written in Go.

## What is Chirpy?

Chirpy is a backend server API developed and tested on Linux that mimics some of the basic functionality of Twitter/X.  It uses postgreSQL for its database.

This is effecitvely a toy application and is not intended for a serious use.  It does not currently support any real security.

It supports a variety of http requests with a variety of endpoints.  Users can sign up, log in and recieve access and refresh tokens, make posts (Chrips!), delete their own Chirps, and view all of the Chirps posted by all users, or just a single user.

## Requirements

Chirpy was build with Go version 1.24.4 and postgreSQL 17.5.  It also uses goose to handle migrations and sqlc was used to generate go code for database queries.

## Installation

- Clone the repository to your local machine

```console
git clone https://github.com/lucoand/chirpy.git
cd chirpy
```

From here, you can do either build or install:

```console
go build
```

```console
go install
```

Installing will put the `chirpy` executable in your path, which will make it easier to run.

You will also need Goose to handle the database migrations for postgreSQL.

```console
go install github.com/pressly/goose/v3/cmd/goose@latest
```

## Configuration

### Step 1

In your postgres console, create a database called `chirpy`:

```console
CREATE DATABASE chirpy;
\c chirpy
```

You should now see this prompt:

```console
chirpy=#
```

From here I would recommend setting up a password for the database.

```console
ALTER USER postgres PASSWORD 'yourpasshere';
```

We will need this password later.

### Step 2

Next, set up your database connection string.  It should be of this form:

```console
"postgres://username:password@host:port/database"
```

Default port for postgres is 5432.

For example:

#### macOS
```console
"postgres://username:@localhost:5432/chirpy"
```
#### Linux
```console
"postgres://postgres:yourpasshere@localhost:5432/chirpy"
```

Next, from the root directory of the repo:

```console
cd sql/schema
goose postgres <connectionString> up
```

Example:
```console
goose postgres "postgres://postgres:yourpasshere@localhost:5432/chirpy" up
```

This will set up the database tables to work with chirpy.

### Step 3

Finally we need to create a file named `.env` in the root chirpy directgory.

The contents of the file should be set up like so:

```code
DB_URL="<connectionString>?sslmode=disable"
SECRET="<secretKey>"
POLKA_KEY="<apiKey>"
```

There is also an optional value you can add for testing purposes:

```code
PLATFORM="dev"
```

I would not recommend setting this option if you were ever using this in a non-testing/toy environment as it allows the use of a destructive reset endpoint.

`DB_URL=` is the address of your database.  We used the connection string earlier to migrate the database.

Example:

`DB_URL="postgres://postgres:postgres@localhost:5432/chirpy?sslmode=disable"`

`SECRET=` is the secret string used to generate authorization tokens.  As its name suggests, you should not share this value!

`POLKA_KEY=` is an API Key.  In the server it is used for an endpoint that toggles a value in user that mimics a subscription service.  Hypothetically it could be used with a payment service to authorize advanced functionality.

That's it!  You're ready to use Chirpy

### Optional

In the main.go code, there is a variable named `profanities` that contains a list (slice) of forbidden words.  It is currently set to some silly words.  Feel free to change this value to suit your needs, if you're into censorship.

## Usage

To start the server, run `./chirpy` in the project root, or just `chirpy` if you installed.  To close the server, press CTRL+C.

### Endpoints

Chirpy supports a variety of http endpoints.  By default, the port is set to `:8080`.  This can be changed by altering the `port` variable in `main.go`

Most of the endpoints will require JSON data in the http request.

#### /app/

Fileserver endpoint.  Shows a simple Welcome page in the browser (index.html).  Currently this serves the entire root directory of the project, which is probably not a good idea.  This does, however, give access to the assets directory as well, so you can serve files from there.  I may change the directory this serves in the future so data is safer.

Example:
`localhost:8080/app/assets/logo.png` will display the logo provided by boot.dev for the project.


#### "GET /api/healthz"

Shows a status of OK when the server is running

#### "GET /admin/metrics"

Shows the number of hits/accesses of the fileserver endpoint from above. Currently it only stores this data while the server is running.

#### "POST /admin/reset"

This dangerous endpoint is only available if you include `PLATFORM="dev"` in your `.env` file.

This effectively resets the database.  As you can imagine, you should use this with care outside of a testing/development environment.

It also resets the hit counter from the previous endpoint.

#### "POST /api/users"

Creates a user in the database.

JSON data expected:
```json
{
    "email": "email@example.com",
    "password": "<yourpassword>"
}
```

As stated earlier, this is a toy application and does not send data securely (yet!).  Transmit passwords with caution.

The response will contain JSON data with user data:

```json
{
    "id": "user_id_in_UUID_format",
    "created_at": "time_user_was_created_at",
    "updated_at": "time_user_was_updated_at",
    "email": "email@example.com",
    "is_chirpy_red": false
}
```

`is_chirpy_red` will be false for new users created this way.

#### "PUT /api/users"

Updates a user's email and password.  Requires a user's access token (more on this later) transmitted in the http requests' Authorization header:
`Authorization: Bearer <token>`

The request needs to have the same JSON format as the previous endpoint.  The response, likewise, will be of the same structure as above.

#### "POST /api/login"

Generates two tokens for the user to be used for interacting with certain endpoints.

The http request must again contain the email and password in JSON like previous endpoints.

The response JSON is slightly different:
```json
    "id": "user_id_in_UUID_format",
    "created_at": "time_user_was_created_at",
    "updated_at": "time_user_was_updated_at",
    "email": "email@example.com",
    "is_chirpy_red": false,
    "token": "<accessToken>",
    "refresh_token": "<refreshToken>"
```

`token` is the users' access token.  This token expires 1 hour after generation, and is used several endpoints.

`refresh_token` expires 60 days after issue.  This token is used to generate a new access token.  This token can also be revoked.

#### "POST /api/refresh"

Generates a new access token for the user.

The http request must contain an Authorization header starting with a Bearer field with the refresh token:

`Authorization: Bearer <refreshToken>`

If the token is valid and has not expired or been revoked, a new authorization token will be generated and returned in the response as JSON data:

```json
{
    "token": "<authorizationToken>"
}
```

#### "POST /api/revoke"

Revokes a refresh token.  Like the previous endpoint, the request requires an authorization header in the same format:

`Authoriztion: Bearer <refreshToken>`

Response will have a status code of 204 if the revokation was successful.  Any other status code indicates something went wrong.

#### "POST /api/chirps"

Creates a new post (Chirp) in the database.

Request requires an Authorization header:
`Authorization: Bearer <authorizationToken>`

As well as JSON data:
```json
{
    "body": "message_body_with_less_than_140_characters"
}
```

If the message is longer than 140 characters, the response will have a status code of `400` and JSON data:
```json
{
    "error": "Chirp is too long"
}
```

If there is an issue with the authorization token, such as a missing or invalid token, the response will have status code `401`.

Otherwise, the response will contain JSON data in the following format:
```json
{
    "body": "message_body",
    "id": "chirp_id_in_UUID_format",
    "created_at": "chirp_creation_time"
    "updated_at": "chirp_update_time"
    "user_id": "user_id_in_uuid_format"
}
```

Response status code will be `201` on success.
