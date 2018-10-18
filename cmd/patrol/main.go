package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/tsenart/patrol"
)

func main() {
	fs := flag.NewFlagSet("patrol", flag.ExitOnError)
	addr := fs.String("addr", "0.0.0.0:8080", "Address to bind HTTP API to")
	if err := run(*addr); err != nil {
		log.Fatal(err)
	}
}

func run(addr string) error {
	lg := log.New(os.Stderr, "", log.LstdFlags)
	repo := patrol.NewInMemoryRepo()
	api := patrol.NewAPI(lg, repo)
	srv := http.Server{
		Addr:     addr,
		Handler:  api.Handler(),
		ErrorLog: lg,
	}
	return srv.ListenAndServe()
}
