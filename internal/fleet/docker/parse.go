// Package docker provides a fleet.Probe implementation that discovers running
// containers via the local Docker Engine unix socket using raw HTTP/1.1.
// No github.com/docker/docker SDK is used — stdlib net, net/http, bufio,
// encoding/json only.
package docker
