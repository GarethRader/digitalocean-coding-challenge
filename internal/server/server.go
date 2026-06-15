package server

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"

	"coding-blitz-02/garethrader/internal/middleware"
)

var flagStore *FlagStore

func Run() {
	if err := Init(); err != nil {
		log.Fatalf("failed to initialize server configuration: %v", err)
	}

	var err error
	flagStore, err = NewFlagStore(databaseURL)
	if err != nil {
		log.Fatalf("failed to initialize feature flag store: %v", err)
	}

	stack := middleware.CreateStack(
		middleware.Validate,
		middleware.Logging,
	)

	probeRouter := http.NewServeMux()
	RegisterProbeHandlers(probeRouter)

	apiRouter := http.NewServeMux()
	RegisterAPIHandlers(apiRouter)

	// Only run middleware on configured HTTP routes
	probeRouter.Handle("/", stack(apiRouter))

	server := &http.Server{
		Addr:    fmt.Sprintf(":%s", serverPort),
		Handler: probeRouter,
	}

	if certFilePath != "" && keyFilePath != "" {
		cert, err := tls.LoadX509KeyPair(certFilePath, keyFilePath)
		if err != nil {
			log.Fatalf("failed to load x509 key pair: %v", err)
		}
		server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
		log.Printf("Starting HTTPS server on port: %s", serverPort)
		err = server.ListenAndServeTLS("", "")
	} else {
		log.Printf("Starting HTTP server on port: %s", serverPort)
		err = server.ListenAndServe()
	}

	if err != nil {
		log.Fatal("server error: ", err)
	}
}
