package main

import (
	"context"

	"accur8.io/godev/a8"
	"accur8.io/godev/nefario"
)

func main() {

	config := &nefario.Config{
		DatabaseUrl: "postgresql://qubes_nefario_owner:gJsK5evKyhZ2fnaS8bfe@tulip.accur8.net:5432/qubes_nefario",
		NatsUrl:     "nats://localhost:4222",
	}

	a8.RunApp(func(ctx context.Context) error {
		return nefario.Run(ctx, config)
	})

}
