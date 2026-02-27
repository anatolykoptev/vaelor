package sample

import (
	"fmt"
	"net/http"
)

const MaxRetries = 3

type Config struct {
	Host string
	Port int
}

type Handler interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

var defaultConfig = Config{Host: "localhost", Port: 8080}

func NewConfig() *Config {
	return &defaultConfig
}

func (c *Config) Run() error {
	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)
	return http.ListenAndServe(addr, nil)
}
