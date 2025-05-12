package nats

import (
	"os"

	"github.com/nats-io/nats.go"
)

type Nats struct {
	Url   string
	Token string
	Conn  *nats.Conn
}

func Connect() (*Nats, error) {
	n := &Nats{
		Url:   os.Getenv("NATS_URL"),
		Token: os.Getenv("NATS_TOKEN"),
	}

	if n.Url == "" {
		n.Url = "nats://localhost:4224"
	}

	opts := []nats.Option{
		nats.Name("NATS Connection"),
	}

	// if token provided
	if n.Token != "" {
		opts = append(opts, nats.Token(n.Token))
	}

	conn, err := nats.Connect(n.Url, opts...)
	if err != nil {
		return nil, err
	}

	n.Conn = conn

	return n, nil
}
