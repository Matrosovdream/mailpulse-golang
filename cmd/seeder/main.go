package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"mailpulse/internal/config"
	"mailpulse/internal/seeder"
)

func main() {
	dir := flag.String("dir", "db/seeds", "directory holding the seed files")
	only := flag.String("only", "", "run a single seeder by name")
	list := flag.Bool("list", false, "list the registered seeders and exit")
	flag.Parse()

	viperConfig := config.NewViper()
	log := config.NewLogger(viperConfig)

	registry := seeder.Default(log)

	if *list {
		fmt.Println("registered seeders, in run order:")
		for _, name := range registry.Names() {
			fmt.Printf("  %-16s <- %s/%s.json\n", name, *dir, name)
		}
		return
	}

	db := config.NewDatabase(viperConfig, log)

	target := "all seeders"
	if *only != "" {
		target = *only
	}
	log.Infof("Seeding %s from %s", target, *dir)

	if err := registry.Run(context.Background(), db, *dir, *only); err != nil {
		// a seed failure is not something to bury in a log line: exit non-zero
		// so make and CI both notice
		log.Errorf("Seeding failed: %v", err)
		if strings.Contains(err.Error(), "does not exist") {
			log.Error("Have the migrations been applied? Try: make migrate-up")
		}
		os.Exit(1)
	}

	log.Info("Seeding complete")
}
